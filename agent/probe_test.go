package agent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func withProbeModelTransport(t *testing.T, transport http.RoundTripper) {
	t.Helper()
	original := probeModelClient.Transport
	probeModelClient.Transport = transport
	t.Cleanup(func() { probeModelClient.Transport = original })
}

func scriptedModelJSON(content string) http.RoundTripper {
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"message":{"content":` + jsonQuote(content) + `}}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
}

// jsonQuote escapes content as a JSON string literal (reusing encoding/json
// would need an import cycle-free helper; this test only ever quotes plain
// ASCII test fixtures, so a minimal escaper is enough).
func jsonQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return `"` + s + `"`
}

func TestTemplatizeURLPlainMatch(t *testing.T) {
	tmpl, err := templatizeURL("https://example.com/search?q=golang", "golang")
	if err != nil || tmpl != "https://example.com/search?q={query}" {
		t.Fatalf("got %q, %v", tmpl, err)
	}
}

func TestTemplatizeURLPercentEscapedMatch(t *testing.T) {
	tmpl, err := templatizeURL("https://example.com/search?q=native%20studio", "native studio")
	if err != nil || tmpl != "https://example.com/search?q={query}" {
		t.Fatalf("got %q, %v", tmpl, err)
	}
}

func TestTemplatizeURLPlusEscapedMatch(t *testing.T) {
	tmpl, err := templatizeURL("https://example.com/search?q=native+studio", "native studio")
	if err != nil || tmpl != "https://example.com/search?q={query}" {
		t.Fatalf("got %q, %v", tmpl, err)
	}
}

func TestTemplatizeURLNotFound(t *testing.T) {
	_, err := templatizeURL("https://example.com/search?q=golang", "rustlang")
	if err == nil {
		t.Fatal("expected an error when the query text isn't in the URL")
	}
}

func TestSniffKind(t *testing.T) {
	if sniffKind([]byte("  <rss></rss>")) != "rss" {
		t.Fatal("expected rss")
	}
	if sniffKind([]byte(" {\"a\":1}")) != "json" {
		t.Fatal("expected json")
	}
	if sniffKind([]byte(" [1,2]")) != "json" {
		t.Fatal("expected json for a bare array")
	}
	if sniffKind([]byte("<html><body>hi</body></html>")) != "rss" {
		// Deliberately documents the current sniff limitation: any leading
		// "<" is treated as RSS-candidate, including plain HTML — the
		// downstream XML decode is what actually rejects non-RSS markup.
		t.Fatal("expected rss (sniff only looks at the leading byte)")
	}
	if sniffKind([]byte("not markup or json")) != "" {
		t.Fatal("expected unsupported for plain text")
	}
}

func TestSlugifyEngineName(t *testing.T) {
	cases := map[string]string{
		"Marginalia Search": "marginalia-search",
		"  Bing!! ":          "bing",
		"a_b/c":              "a-b-c",
	}
	for in, want := range cases {
		if got := slugifyEngineName(in); got != want {
			t.Errorf("slugifyEngineName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProbeSearchEngineRSSSuccess(t *testing.T) {
	rss := `<?xml version="1.0"?><rss><channel><item><title>Golang tips</title><link>https://x.test/1</link><description>about golang</description></item></channel></rss>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, rss)
	}))
	defer server.Close()

	result := ProbeSearchEngine(context.Background(), "http://ollama.test", "test-model", ProbeRequest{
		Name: "Test RSS Engine", SampleURL: server.URL + "/search?q=golang", SampleQuery: "golang",
	})
	if !result.OK {
		t.Fatalf("expected success, got %+v", result)
	}
	if result.Engine == nil || result.Engine.Kind != "rss" || result.Engine.URLTemplate != server.URL+"/search?q={query}" {
		t.Fatalf("unexpected engine: %+v", result.Engine)
	}
	if len(result.Sample) == 0 {
		t.Fatal("expected at least one sample result")
	}
}

func TestProbeSearchEngineJSONSuccess(t *testing.T) {
	sampleJSON := `{"results":[{"heading":"Golang Tips","link":"https://x.test/1","snippet":"about golang"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, sampleJSON)
	}))
	defer server.Close()
	withProbeModelTransport(t, scriptedModelJSON(`{"results_path":"results","title_field":"heading","url_field":"link","desc_field":"snippet"}`))

	result := ProbeSearchEngine(context.Background(), "http://ollama.test", "test-model", ProbeRequest{
		Name: "Test JSON Engine", SampleURL: server.URL + "/api?q=golang", SampleQuery: "golang",
	})
	if !result.OK {
		t.Fatalf("expected success, got %+v", result)
	}
	if result.Engine.ResultsPath != "results" || result.Engine.TitleField != "heading" {
		t.Fatalf("unexpected field mapping: %+v", result.Engine)
	}
	if len(result.Sample) != 1 || result.Sample[0].Title != "Golang Tips" {
		t.Fatalf("expected the real sample extracted via the inferred mapping, got %+v", result.Sample)
	}
}

// TestProbeSearchEngineRejectsUnverifiedModelGuess is the key safety test:
// a model-inferred field mapping that doesn't actually extract anything from
// the real response must be reported as a failed probe, not accepted on the
// strength of the guess alone.
func TestProbeSearchEngineRejectsUnverifiedModelGuess(t *testing.T) {
	sampleJSON := `{"results":[{"heading":"Golang Tips","link":"https://x.test/1"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, sampleJSON)
	}))
	defer server.Close()
	// Model names fields that don't exist in the real response.
	withProbeModelTransport(t, scriptedModelJSON(`{"results_path":"items","title_field":"name"}`))

	result := ProbeSearchEngine(context.Background(), "http://ollama.test", "test-model", ProbeRequest{
		Name: "Bad Mapping Engine", SampleURL: server.URL + "/api?q=golang", SampleQuery: "golang",
	})
	if result.OK {
		t.Fatalf("expected failure when the inferred mapping extracts nothing real, got %+v", result)
	}
}

func TestProbeSearchEngineRejectsHTMLResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "not markup or json, just plain text")
	}))
	defer server.Close()

	result := ProbeSearchEngine(context.Background(), "http://ollama.test", "test-model", ProbeRequest{
		Name: "Plaintext Engine", SampleURL: server.URL + "/search?q=golang", SampleQuery: "golang",
	})
	if result.OK {
		t.Fatalf("expected failure for an unsupported response format, got %+v", result)
	}
}

func TestProbeSearchEngineRequiresAllFields(t *testing.T) {
	result := ProbeSearchEngine(context.Background(), "http://ollama.test", "test-model", ProbeRequest{Name: "X"})
	if result.OK {
		t.Fatal("expected failure when sample_url/sample_query are missing")
	}
}

func TestProbeSearchEngineQueryNotInURL(t *testing.T) {
	result := ProbeSearchEngine(context.Background(), "http://ollama.test", "test-model", ProbeRequest{
		Name: "X", SampleURL: "https://example.com/search?q=golang", SampleQuery: "rustlang",
	})
	if result.OK {
		t.Fatal("expected failure when the sample query text isn't found in the URL")
	}
}
