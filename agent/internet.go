package agent

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type internetSearchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

// registerInternetSearchTool gives the local model a read-only research path
// when the answer is not present in its knowledge or the workspace.
func registerInternetSearchTool(r *Registry) {
	r.Register(&Tool{
		Name: "search_internet",
		Description: "Search the public internet across several engines at once and cross-check what they say. Returns results tagged with their publisher, plus a confidence rating based on how many INDEPENDENT sources agree. " +
			"Use this before guessing when a request depends on facts not in the workspace or your knowledge. Check the returned confidence: 'high' means several independent sources agree and you can answer from it; " +
			"'medium' means only two sources agree; 'low' means the evidence is too thin to state as fact — refine the query and search again, or ask the user to verify.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":       map[string]any{"type": "string", "description": "A focused search query."},
				"max_results": map[string]any{"type": "integer", "description": "Results per engine, from 1 to 8. Defaults to 5."},
			},
			"required": []string{"query"},
		},
		Safety: Safe,
		Execute: func(ctx context.Context, input ToolInput, meta ToolMeta) (ToolResult, error) {
			query, _ := input["query"].(string)
			query = strings.TrimSpace(query)
			if query == "" {
				return ToolResult{OK: false, Error: "query is required"}, nil
			}
			limit := 5
			if raw, ok := input["max_results"].(float64); ok && raw >= 1 && raw <= 8 {
				limit = int(raw)
			}

			check := searchAllEngines(ctx, query, limit)
			if len(check.Results) == 0 {
				return ToolResult{OK: false, Error: "no results found across any search engine", Content: map[string]any{
					"query": query, "engines_queried": check.EnginesQueried, "engines_failed": check.EnginesFailed,
				}}, nil
			}
			return ToolResult{OK: true, Content: map[string]any{
				"query":               query,
				"results":             check.Results,
				"confidence":          check.Confidence,
				"confidence_reason":   check.Reason,
				"independent_sources": check.IndependentDomains,
				"engines_queried":     check.EnginesQueried,
				"engines_failed":      check.EnginesFailed,
			}}, nil
		},
	})
}

func searchInternet(ctx context.Context, query string, limit int) ([]internetSearchResult, error) {
	endpoint := "https://www.bing.com/search?format=rss&q=" + url.QueryEscape(query)
	return searchRSS(ctx, endpoint, query, limit)
}

func searchNews(ctx context.Context, query string, limit int) ([]internetSearchResult, error) {
	endpoint := "https://news.google.com/rss/search?q=" + url.QueryEscape(query) + "&hl=en&gl=US&ceid=US:en"
	return searchRSS(ctx, endpoint, query, limit)
}

func searchCurrentEvent(ctx context.Context, query string, limit int) ([]internetSearchResult, error) {
	newsResults, newsErr := searchNews(ctx, query, limit)
	if newsErr == nil && len(newsResults) > 0 {
		return newsResults, nil
	}
	webResults, webErr := searchInternet(ctx, query, limit)
	if webErr == nil {
		return webResults, nil
	}
	if newsErr != nil {
		return nil, fmt.Errorf("news search failed: %v; web fallback failed: %w", newsErr, webErr)
	}
	return nil, webErr
}

func searchRSS(ctx context.Context, endpoint, query string, limit int) ([]internetSearchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "NativeStudio/1.0")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("internet search unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("internet search returned status %d", resp.StatusCode)
	}
	var feed struct {
		Channel struct {
			Items []struct {
				Title       string `xml:"title"`
				Link        string `xml:"link"`
				Description string `xml:"description"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("decode internet search: %w", err)
	}
	if limit > len(feed.Channel.Items) {
		limit = len(feed.Channel.Items)
	}
	candidates := make([]internetSearchResult, 0, len(feed.Channel.Items))
	for _, item := range feed.Channel.Items {
		candidates = append(candidates, internetSearchResult{
			Title:       strings.TrimSpace(item.Title),
			URL:         strings.TrimSpace(item.Link),
			Description: cleanSearchDescription(item.Description),
		})
	}
	results := filterRelevantResults(query, candidates)
	if limit > len(results) {
		limit = len(results)
	}
	return results[:limit], nil
}

var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)

func cleanSearchDescription(value string) string {
	return strings.TrimSpace(html.UnescapeString(htmlTagPattern.ReplaceAllString(value, " ")))
}

func filterRelevantResults(query string, candidates []internetSearchResult) []internetSearchResult {
	tokens := significantSearchTokens(query)
	if len(tokens) == 0 {
		return candidates
	}
	core := coreSearchTokens(query)
	minimumMatches := 2
	if len(tokens) == 1 {
		minimumMatches = 1
	}
	results := make([]internetSearchResult, 0, len(candidates))
	for _, candidate := range candidates {
		haystack := strings.ToLower(candidate.Title + " " + candidate.Description)

		// A result must name the actual subject. Without this, a document
		// sharing only generic words with the query counts as a match, and
		// then as an independent corroborating source — manufacturing
		// agreement between documents that have nothing to do with each
		// other. Verified live against the "latest PHP release" case.
		if len(core) > 0 && !matchesAnyToken(core, haystack) {
			continue
		}

		matches := 0
		for _, token := range tokens {
			if fuzzyTokenMatch(token, haystack) {
				matches++
			}
		}
		if matches >= minimumMatches {
			results = append(results, candidate)
		}
	}
	return results
}

func matchesAnyToken(tokens []string, haystack string) bool {
	for _, token := range tokens {
		if fuzzyTokenMatch(token, haystack) {
			return true
		}
	}
	return false
}

func fuzzyTokenMatch(token, text string) bool {
	// Short tokens must match as whole words. Plain substring matching is
	// fine for longer terms, but a 2-3 character token like "go" occurs
	// inside "algorithm", "google" and "going", which would let completely
	// unrelated documents look relevant now that short tokens are no longer
	// discarded outright.
	if len(token) < 4 {
		return containsTokenAtBoundary(text, token)
	}
	if strings.Contains(text, token) {
		return true
	}
	if len(token) < 5 {
		return false
	}
	for _, word := range strings.FieldsFunc(text, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}) {
		if absInt(len(word)-len(token)) <= 1 && editDistanceAtMostOne(token, word) {
			return true
		}
	}
	return false
}

func editDistanceAtMostOne(a, b string) bool {
	if a == b {
		return true
	}
	if absInt(len(a)-len(b)) > 1 {
		return false
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	i, j, edits := 0, 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			i++
			j++
			continue
		}
		edits++
		if edits > 1 {
			return false
		}
		if len(a) == len(b) {
			i++
		}
		j++
	}
	if i < len(a) || j < len(b) {
		edits++
	}
	return edits <= 1
}

// containsTokenAtBoundary reports whether token appears in text as a whole
// word — neighbours must be non-alphanumeric. Used for short tokens, where
// substring matching is too loose to be meaningful.
func containsTokenAtBoundary(text, token string) bool {
	isAlphanumeric := func(b byte) bool {
		return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
	}
	for offset := 0; ; {
		index := strings.Index(text[offset:], token)
		if index < 0 {
			return false
		}
		start := offset + index
		end := start + len(token)
		beforeOK := start == 0 || !isAlphanumeric(text[start-1])
		afterOK := end == len(text) || !isAlphanumeric(text[end])
		if beforeOK && afterOK {
			return true
		}
		offset = start + 1
		if offset >= len(text) {
			return false
		}
	}
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

// searchStopwords are words too generic to identify a topic. This list does
// the filtering that a blunt minimum-length rule used to: the old code
// dropped anything under 4 characters, which silently discarded the single
// most meaningful term in queries like "latest PHP release", "npm install
// fails" or "git rebase" — leaving only generic words to match on. That
// produced live false positives (a GOV.UK story titled "DVLA releases latest
// scam images" matched "latest PHP release" because both "latest" and
// "release" were present and "php" had been thrown away).
var searchStopwords = map[string]bool{
	"a": true, "about": true, "all": true, "an": true, "and": true, "any": true,
	"are": true, "as": true, "at": true, "be": true, "best": true, "but": true,
	"by": true, "can": true, "current": true, "currently": true, "do": true,
	"does": true, "for": true, "from": true, "get": true, "give": true,
	"has": true, "have": true, "how": true, "in": true, "is": true, "it": true,
	"its": true, "latest": true, "me": true, "my": true, "new": true,
	"newest": true, "news": true, "not": true, "now": true, "of": true,
	"on": true, "or": true, "recent": true, "should": true, "so": true,
	"one": true, "ones": true, "please": true, "thing": true, "things": true,
	"some": true, "tell": true, "that": true, "the": true, "their": true,
	"there": true, "these": true, "this": true, "to": true, "today": true,
	"update": true, "updated": true, "use": true, "using": true, "was": true,
	"what": true, "when": true, "where": true, "which": true, "who": true,
	"why": true, "will": true, "with": true, "you": true, "your": true,
}

// genericTopicWords still carry meaning but are far too common across
// unrelated documents to identify a subject on their own. A result matching
// only these is not on-topic — see coreSearchTokens.
var genericTopicWords = map[string]bool{
	"release": true, "releases": true, "released": true, "version": true,
	"versions": true, "download": true, "install": true, "download s": true,
	"price": true, "prices": true, "date": true, "dates": true, "time": true,
	"guide": true, "tutorial": true, "docs": true, "documentation": true,
}

// significantSearchTokens returns every topic-bearing token in a query.
// Short tokens are kept — "php", "git", "npm", "vue", "ios", "css", "sql",
// "aws" and "api" are exactly the words that pin a query to its subject.
func significantSearchTokens(query string) []string {
	fields := strings.Fields(strings.ToLower(strings.NewReplacer(`"`, " ", "'", "").Replace(query)))
	seen := map[string]bool{}
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, " ,.!?:;()[]{}/\\")
		if len(field) < 2 || searchStopwords[field] || seen[field] {
			continue
		}
		seen[field] = true
		result = append(result, field)
	}
	return result
}

// coreSearchTokens are the tokens that actually identify the subject — the
// significant tokens minus the generic ones. At least one of these must
// appear in a result for it to count as on-topic; matching only generic
// words ("latest", "release") is how unrelated documents used to slip
// through and then get counted as corroborating sources.
func coreSearchTokens(query string) []string {
	tokens := significantSearchTokens(query)
	core := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if !genericTopicWords[token] {
			core = append(core, token)
		}
	}
	return core
}
