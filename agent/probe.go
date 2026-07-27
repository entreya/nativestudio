package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ProbeRequest is what Settings sends to test a candidate search engine
// before it's saved. sampleURL is a real results-page URL the user already
// got by searching sampleQuery on that engine in their own browser — the
// probe finds sampleQuery inside sampleURL and replaces it with "{query}" to
// build the template, rather than asking anyone (human or model) to guess
// the engine's query parameter name.
type ProbeRequest struct {
	Name        string `json:"name"`
	SampleURL   string `json:"sample_url"`
	SampleQuery string `json:"sample_query"`
}

// ProbeResult is shown to the user before anything is saved — either a
// working engine configuration plus a sample of what it actually returned
// for their query, or a clear reason it didn't work. Probing never persists
// anything; the caller (Settings) decides whether to keep Engine.
type ProbeResult struct {
	OK      bool          `json:"ok"`
	Message string        `json:"message"`
	Engine  *SearchEngine `json:"engine,omitempty"`
	Sample  []ProbeSample `json:"sample,omitempty"`
}

type ProbeSample struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

const probeHTTPTimeout = 20 * time.Second
const probeModelTimeout = 25 * time.Second

// probeHTTPClient and probeModelClient are dedicated clients (not
// http.DefaultClient) for the same reason every other agent HTTP call in
// this codebase gets its own: a test scripting http.DefaultClient.Transport
// must not silently intercept probe traffic.
var probeHTTPClient = &http.Client{Timeout: probeHTTPTimeout}
var probeModelClient = &http.Client{Timeout: probeModelTimeout}

const jsonFieldInferenceSystemPrompt = `You are given a sample JSON response from a search API, and the query text that produced it. Identify where the list of results lives and which fields within each result hold the title, URL, and description.

Output ONLY a JSON object of this shape, nothing else:
{"results_path": "dotted.path.to.array", "title_field": "field_name", "url_field": "field_name", "desc_field": "field_name"}

"results_path" is a dot-separated path from the root object to the array of results (e.g. "query.search" if the array is at root.query.search). "title_field", "url_field", "desc_field" are the key names within ONE element of that array — not full paths, just the key name. If there's no separate description field, reuse the title field's name for desc_field. If there's no direct URL field but titles could be turned into a URL by prefixing them, set url_field to "" — omit url_prefix entirely; the caller decides whether to add one.`

// ProbeSearchEngine tests a candidate search engine end to end: builds a URL
// template from the sample, fetches it for real, detects whether the
// response is RSS or JSON, and — for JSON, whose shape varies per API unlike
// RSS's fixed schema — asks the model to identify the field mapping, then
// verifies that mapping actually extracts non-empty results from the real
// response before ever reporting success. A plausible-looking model guess
// that doesn't actually work on the real data is treated as a failed probe,
// not a success.
func ProbeSearchEngine(ctx context.Context, ollamaURL, model string, req ProbeRequest) ProbeResult {
	name := strings.TrimSpace(req.Name)
	sampleURL := strings.TrimSpace(req.SampleURL)
	sampleQuery := strings.TrimSpace(req.SampleQuery)
	if name == "" || sampleURL == "" || sampleQuery == "" {
		return ProbeResult{OK: false, Message: "name, a sample results URL, and the query you searched for are all required"}
	}

	template, err := templatizeURL(sampleURL, sampleQuery)
	if err != nil {
		return ProbeResult{OK: false, Message: err.Error()}
	}

	probeCtx, cancel := context.WithTimeout(ctx, probeHTTPTimeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(probeCtx, http.MethodGet, sampleURL, nil)
	if err != nil {
		return ProbeResult{OK: false, Message: err.Error()}
	}
	httpReq.Header.Set("User-Agent", "NativeStudio/1.0")

	resp, err := probeHTTPClient.Do(httpReq)
	if err != nil {
		return ProbeResult{OK: false, Message: fmt.Sprintf("could not reach that URL: %v", err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ProbeResult{OK: false, Message: fmt.Sprintf("that URL returned status %d", resp.StatusCode)}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return ProbeResult{OK: false, Message: fmt.Sprintf("could not read the response: %v", err)}
	}

	switch sniffKind(body) {
	case "rss":
		return probeRSS(name, template, sampleQuery, body)
	case "json":
		return probeJSON(ctx, ollamaURL, model, name, template, sampleQuery, body)
	default:
		return ProbeResult{OK: false, Message: "that URL returned something that isn't RSS or JSON (looks like an HTML page) — NativeStudio can only use a search engine that has an RSS or JSON results feed"}
	}
}

// templatizeURL finds query (or its URL-escaped form) inside sampleURL and
// replaces it with the literal "{query}" placeholder. Tried in both forms
// because a URL copied from a browser's address bar is usually already
// percent-encoded, but a single-word query often isn't changed by encoding
// at all.
func templatizeURL(sampleURL, query string) (string, error) {
	// url.QueryEscape encodes a space as "+" (form-encoding style);
	// url.PathEscape encodes it as "%20" instead — real query strings use
	// either depending on the engine, so both are tried explicitly rather
	// than assuming one.
	candidates := []string{query, url.QueryEscape(query), url.PathEscape(query)}
	for _, candidate := range candidates {
		if candidate != "" && strings.Contains(sampleURL, candidate) {
			return strings.Replace(sampleURL, candidate, "{query}", 1), nil
		}
	}
	return "", fmt.Errorf("couldn't find %q anywhere in that URL — paste the exact URL you got after searching for exactly that text", query)
}

// sniffKind reports "rss", "json", or "" (unsupported) based on the first
// non-whitespace byte of the response body — cheap and reliable enough for
// deciding which parser to try, without needing the (often absent or wrong)
// Content-Type header.
func sniffKind(body []byte) string {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return ""
	}
	switch trimmed[0] {
	case '<':
		return "rss"
	case '{', '[':
		return "json"
	default:
		return ""
	}
}

func probeRSS(name, template, sampleQuery string, body []byte) ProbeResult {
	engine := SearchEngine{ID: slugifyEngineName(name), Name: name, Enabled: true, Kind: "rss", Category: "web", URLTemplate: template}
	results, err := parseRSSEngine(bytes.NewReader(body), engine, sampleQuery, 5)
	if err != nil {
		return ProbeResult{OK: false, Message: fmt.Sprintf("found an RSS feed, but couldn't parse it: %v", err)}
	}
	if len(results) == 0 {
		return ProbeResult{OK: false, Message: "found a valid RSS feed, but it returned no results relevant to your sample query — double check the URL actually searched for that text"}
	}
	return ProbeResult{OK: true, Message: fmt.Sprintf("Works — found %d result(s) as an RSS feed.", len(results)), Engine: &engine, Sample: toProbeSamples(results)}
}

func probeJSON(ctx context.Context, ollamaURL, model, name, template, sampleQuery string, body []byte) ProbeResult {
	if model == "" {
		return ProbeResult{OK: false, Message: "no model configured to identify this JSON API's field layout"}
	}
	fields, err := inferJSONFields(ctx, ollamaURL, model, sampleQuery, body)
	if err != nil {
		return ProbeResult{OK: false, Message: fmt.Sprintf("found a JSON API, but couldn't work out its field layout: %v", err)}
	}

	engine := SearchEngine{
		ID: slugifyEngineName(name), Name: name, Enabled: true, Kind: "json", Category: "web", URLTemplate: template,
		ResultsPath: fields.ResultsPath, TitleField: fields.TitleField, URLField: fields.URLField, DescField: fields.DescField,
	}

	// The model's guess is only trusted once it's proven to actually pull
	// real, non-empty results out of the real response — never on the
	// strength of the guess alone.
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ProbeResult{OK: false, Message: fmt.Sprintf("that response isn't valid JSON: %v", err)}
	}
	results, err := parseJSONEngine(bytes.NewReader(body), engine, sampleQuery, 5)
	if err != nil || len(results) == 0 {
		return ProbeResult{OK: false, Message: fmt.Sprintf(
			"identified a possible layout (results at %q, title field %q) but it didn't extract any real results from the response — this engine's format may not be supported",
			fields.ResultsPath, fields.TitleField)}
	}

	return ProbeResult{OK: true, Message: fmt.Sprintf("Works — found %d result(s) as a JSON API.", len(results)), Engine: &engine, Sample: toProbeSamples(results)}
}

// slugifyEngineName turns a display name into a stable, URL/JSON-key-safe ID
// (matching the style of the builtin IDs — "bing", "google-news") so the
// caller doesn't need to invent one, and re-probing the same name again
// consistently produces the same ID rather than a fresh one each time.
func slugifyEngineName(name string) string {
	var b strings.Builder
	lastWasDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastWasDash = false
		case !lastWasDash:
			b.WriteByte('-')
			lastWasDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func toProbeSamples(results []sourcedResult) []ProbeSample {
	samples := make([]ProbeSample, 0, len(results))
	for _, r := range results {
		samples = append(samples, ProbeSample{Title: r.Title, URL: r.URL, Description: r.Description})
	}
	return samples
}

type jsonFieldMapping struct {
	ResultsPath string `json:"results_path"`
	TitleField  string `json:"title_field"`
	URLField    string `json:"url_field"`
	DescField   string `json:"desc_field"`
}

// inferJSONFields asks the model to locate the results array and its title/
// URL/description fields within a sample JSON response. Truncates the
// sample to keep the prompt small — enough structure to identify the shape
// is present near the start of almost any real API response.
func inferJSONFields(ctx context.Context, ollamaURL, model, sampleQuery string, body []byte) (jsonFieldMapping, error) {
	sample := body
	const maxSampleBytes = 4000
	if len(sample) > maxSampleBytes {
		sample = sample[:maxSampleBytes]
	}

	reqCtx, cancel := context.WithTimeout(ctx, probeModelTimeout)
	defer cancel()

	payload := map[string]any{
		"model":      model,
		"stream":     false,
		"think":      false,
		"format":     "json",
		"keep_alive": "5m",
		"messages": []map[string]string{
			{"role": "system", "content": jsonFieldInferenceSystemPrompt},
			{"role": "user", "content": fmt.Sprintf("Query used: %s\n\nSample JSON response:\n%s", sampleQuery, string(sample))},
		},
		"options": map[string]any{"temperature": 0.1},
	}
	body2, err := json.Marshal(payload)
	if err != nil {
		return jsonFieldMapping{}, err
	}

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, ollamaURL+"/api/chat", bytes.NewReader(body2))
	if err != nil {
		return jsonFieldMapping{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := probeModelClient.Do(httpReq)
	if err != nil {
		return jsonFieldMapping{}, fmt.Errorf("model unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return jsonFieldMapping{}, fmt.Errorf("model returned status %d", resp.StatusCode)
	}

	var parsed struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return jsonFieldMapping{}, fmt.Errorf("decode model response: %w", err)
	}

	var mapping jsonFieldMapping
	if err := json.Unmarshal([]byte(strings.TrimSpace(parsed.Message.Content)), &mapping); err != nil {
		return jsonFieldMapping{}, fmt.Errorf("model did not return valid JSON: %w", err)
	}
	if mapping.ResultsPath == "" || mapping.TitleField == "" {
		return jsonFieldMapping{}, fmt.Errorf("model could not identify a results array in this response")
	}
	return mapping, nil
}
