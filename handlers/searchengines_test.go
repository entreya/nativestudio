package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListEnginesReturnsBuiltins(t *testing.T) {
	handler := NewSearchEnginesHandler("http://ollama.test", "test-model")
	req := httptest.NewRequest(http.MethodGet, "/api/search-engines", nil)
	rec := httptest.NewRecorder()
	handler.ListEngines(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var engines []map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&engines); err != nil {
		t.Fatal(err)
	}
	if len(engines) < 4 {
		t.Fatalf("expected at least the 4 builtin engines, got %d: %+v", len(engines), engines)
	}
	found := false
	for _, e := range engines {
		if e["id"] == "bing" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the builtin bing engine in the list, got %+v", engines)
	}
}

func TestProbeEngineEndToEnd(t *testing.T) {
	rssServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<?xml version="1.0"?><rss><channel><item><title>Golang tips</title><link>https://x.test/1</link><description>about golang</description></item></channel></rss>`)
	}))
	defer rssServer.Close()

	handler := NewSearchEnginesHandler("http://ollama.test", "test-model")
	body := `{"name":"Test Engine","sample_url":"` + rssServer.URL + `/search?q=golang","sample_query":"golang"}`
	req := httptest.NewRequest(http.MethodPost, "/api/search-engines/probe", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ProbeEngine(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var result map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result["ok"] != true {
		t.Fatalf("expected a successful probe, got %+v", result)
	}
	engine, ok := result["engine"].(map[string]interface{})
	if !ok || engine["id"] != "test-engine" {
		t.Fatalf("expected a slugified engine id, got %+v", result)
	}
}

func TestProbeEngineRejectsInvalidJSON(t *testing.T) {
	handler := NewSearchEnginesHandler("http://ollama.test", "test-model")
	req := httptest.NewRequest(http.MethodPost, "/api/search-engines/probe", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	handler.ProbeEngine(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON, got %d", rec.Code)
	}
}
