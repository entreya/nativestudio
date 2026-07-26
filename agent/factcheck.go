package agent

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// enginePerRequestTimeout bounds a single engine so one slow or hanging
// source can't hold up the whole multi-engine search — every engine is
// queried in parallel and stragglers are simply dropped from the result set.
const enginePerRequestTimeout = 12 * time.Second

// sourcedResult is a search hit tagged with where it came from, which is what
// makes cross-source checking possible at all: the raw result alone can't tell
// you whether two hits are independent reporting or the same wire story
// republished twice.
type sourcedResult struct {
	internetSearchResult
	EngineID   string `json:"engine_id"`
	EngineName string `json:"engine"`
	Category   string `json:"category"`
	// Domain is the *publisher* domain, not the aggregator's. Google News
	// links all point at news.google.com, but its RSS carries the real
	// publisher in a <source url> tag; without unwrapping that, ten stories
	// from ten outlets would look like a single source.
	Domain string `json:"domain"`
	// Independent is false when this engine merely republishes another
	// (DerivesFrom), so corroboration counting can discount it.
	Independent bool `json:"independent"`
}

// factCheck is the outcome of querying every enabled engine and comparing what
// they returned.
type factCheck struct {
	Query              string          `json:"query"`
	Results            []sourcedResult `json:"results"`
	EnginesQueried     []string        `json:"engines_queried"`
	EnginesFailed      []string        `json:"engines_failed,omitempty"`
	IndependentDomains []string        `json:"independent_domains"`
	// Confidence is "high", "medium" or "low" — see scoreFactCheck for the
	// exact rule. This is what the agent loop escalates on.
	Confidence string `json:"confidence"`
	Reason     string `json:"reason"`
}

// searchAllEngines queries every enabled engine in parallel and returns their
// combined, de-duplicated results. An engine failing is recorded, never fatal:
// partial evidence is still useful, and the caller decides via Confidence
// whether it's enough to act on.
func searchAllEngines(ctx context.Context, query string, perEngineLimit int) factCheck {
	engines := enabledSearchEngines()
	check := factCheck{Query: query}

	type engineOutcome struct {
		engine  SearchEngine
		results []sourcedResult
		err     error
	}

	outcomes := make([]engineOutcome, len(engines))
	var wg sync.WaitGroup
	for i, engine := range engines {
		wg.Add(1)
		go func(index int, engine SearchEngine) {
			defer wg.Done()
			engineCtx, cancel := context.WithTimeout(ctx, enginePerRequestTimeout)
			defer cancel()
			results, err := querySearchEngine(engineCtx, engine, query, perEngineLimit)
			outcomes[index] = engineOutcome{engine: engine, results: results, err: err}
		}(i, engine)
	}
	wg.Wait()

	seenURL := map[string]bool{}
	for _, outcome := range outcomes {
		if outcome.err != nil {
			check.EnginesFailed = append(check.EnginesFailed, outcome.engine.Name)
			continue
		}
		check.EnginesQueried = append(check.EnginesQueried, outcome.engine.Name)
		for _, result := range outcome.results {
			key := strings.ToLower(strings.TrimRight(result.URL, "/"))
			if key != "" && seenURL[key] {
				continue
			}
			seenURL[key] = true
			check.Results = append(check.Results, result)
		}
	}

	scoreFactCheck(&check)
	return check
}

// scoreFactCheck decides how much the gathered evidence can be trusted, based
// on how many *independent publisher domains* back it — not how many results
// came back. Ten hits from one outlet is one source; that distinction is the
// whole point of cross-checking.
func scoreFactCheck(check *factCheck) {
	domains := map[string]bool{}
	categories := map[string]bool{}
	for _, result := range check.Results {
		if !result.Independent || result.Domain == "" {
			continue
		}
		domains[result.Domain] = true
		categories[result.Category] = true
	}
	for domain := range domains {
		check.IndependentDomains = append(check.IndependentDomains, domain)
	}
	sort.Strings(check.IndependentDomains)

	count := len(check.IndependentDomains)
	switch {
	case count == 0:
		check.Confidence = "low"
		check.Reason = "No independent sources returned usable results."
	case count == 1:
		check.Confidence = "low"
		check.Reason = fmt.Sprintf("Only one independent source (%s) — nothing to corroborate it against.", check.IndependentDomains[0])
	case count == 2 && len(categories) == 1:
		// Two outlets of the same kind (e.g. two news aggregators) often
		// carry the same wire copy, so this is weaker than it looks.
		check.Confidence = "medium"
		check.Reason = fmt.Sprintf("Two sources agree, but both are %s — they may share one original report.", onlyKey(categories))
	case count == 2:
		check.Confidence = "medium"
		check.Reason = "Two independent sources of different kinds agree."
	default:
		check.Confidence = "high"
		check.Reason = fmt.Sprintf("%d independent sources returned consistent results.", count)
	}
}

func onlyKey(set map[string]bool) string {
	for key := range set {
		return key
	}
	return ""
}

// querySearchEngine fetches and parses one engine according to its configured
// Kind. Returns an error rather than partial junk so searchAllEngines can
// report precisely which sources were unavailable.
func querySearchEngine(ctx context.Context, engine SearchEngine, query string, limit int) ([]sourcedResult, error) {
	endpoint := buildSearchURL(engine.URLTemplate, query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "NativeStudio/1.0")

	resp, err := searchHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s unavailable: %w", engine.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned status %d", engine.Name, resp.StatusCode)
	}

	switch engine.Kind {
	case "json":
		return parseJSONEngine(resp.Body, engine, query, limit)
	default:
		return parseRSSEngine(resp.Body, engine, query, limit)
	}
}

// searchHTTPClient follows redirects (Google News answers 302 before serving
// its feed) and is separate from http.DefaultClient so tests that swap the
// default transport to script the model's own calls don't accidentally
// intercept search traffic.
var searchHTTPClient = &http.Client{Timeout: enginePerRequestTimeout}

type rssFeed struct {
	Channel struct {
		Items []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			Description string `xml:"description"`
			Source      struct {
				URL  string `xml:"url,attr"`
				Name string `xml:",chardata"`
			} `xml:"source"`
		} `xml:"item"`
	} `xml:"channel"`
}

func parseRSSEngine(body interface{ Read([]byte) (int, error) }, engine SearchEngine, query string, limit int) ([]sourcedResult, error) {
	var feed rssFeed
	if err := xml.NewDecoder(body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("decode %s: %w", engine.Name, err)
	}

	candidates := make([]internetSearchResult, 0, len(feed.Channel.Items))
	publishers := make([]string, 0, len(feed.Channel.Items))
	for _, item := range feed.Channel.Items {
		candidates = append(candidates, internetSearchResult{
			Title:       strings.TrimSpace(item.Title),
			URL:         strings.TrimSpace(item.Link),
			Description: cleanSearchDescription(item.Description),
		})
		// Prefer the aggregator's declared publisher over the link host, so
		// Google News results attribute to the real outlet.
		publishers = append(publishers, strings.TrimSpace(item.Source.URL))
	}

	relevant := filterRelevantResults(query, candidates)
	return tagResults(relevant, candidates, publishers, engine, limit), nil
}

func parseJSONEngine(body interface{ Read([]byte) (int, error) }, engine SearchEngine, query string, limit int) ([]sourcedResult, error) {
	var payload map[string]any
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode %s: %w", engine.Name, err)
	}

	items, ok := lookupJSONArray(payload, engine.ResultsPath)
	if !ok {
		return nil, fmt.Errorf("%s: no results at %q", engine.Name, engine.ResultsPath)
	}

	candidates := make([]internetSearchResult, 0, len(items))
	for _, raw := range items {
		element, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		title := cleanSearchDescription(stringField(element, engine.TitleField))
		if title == "" {
			continue
		}
		link := stringField(element, engine.URLField)
		if link == "" && engine.URLPrefix != "" {
			link = engine.URLPrefix + strings.ReplaceAll(title, " ", "_")
		}
		candidates = append(candidates, internetSearchResult{
			Title:       title,
			URL:         strings.TrimSpace(link),
			Description: cleanSearchDescription(stringField(element, engine.DescField)),
		})
	}

	relevant := filterRelevantResults(query, candidates)
	return tagResults(relevant, candidates, nil, engine, limit), nil
}

// tagResults attaches engine/domain/independence metadata and applies the
// per-engine limit. publishers is optional and index-aligned with allResults.
func tagResults(relevant, allResults []internetSearchResult, publishers []string, engine SearchEngine, limit int) []sourcedResult {
	publisherFor := func(result internetSearchResult) string {
		if publishers == nil {
			return ""
		}
		for i, candidate := range allResults {
			if candidate.URL == result.URL && i < len(publishers) {
				return publishers[i]
			}
		}
		return ""
	}

	tagged := make([]sourcedResult, 0, len(relevant))
	for _, result := range relevant {
		if len(tagged) >= limit {
			break
		}
		domainSource := result.URL
		if publisher := publisherFor(result); publisher != "" {
			domainSource = publisher
		}
		tagged = append(tagged, sourcedResult{
			internetSearchResult: result,
			EngineID:             engine.ID,
			EngineName:           engine.Name,
			Category:             engine.Category,
			Domain:               sourceDomain(domainSource, engine.ID),
			Independent:          engine.DerivesFrom == "",
		})
	}
	return tagged
}

// lookupJSONArray walks a dotted path ("query.search") to an array.
func lookupJSONArray(payload map[string]any, path string) ([]any, bool) {
	if path == "" {
		return nil, false
	}
	var current any = payload
	for _, segment := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[segment]
		if !ok {
			return nil, false
		}
	}
	array, ok := current.([]any)
	return array, ok
}

func stringField(element map[string]any, key string) string {
	if key == "" {
		return ""
	}
	value, _ := element[key].(string)
	return value
}
