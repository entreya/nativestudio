package ollama

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
func testResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"application/json"}}}
}

func TestStructuredSummaryRepairsInvalidJSONOnce(t *testing.T) {
	var calls atomic.Int32
	client := NewClient("http://ollama.test", "summary", "embed", 1)
	client.HTTP = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return testResponse(200, `{"message":{"content":"not json"}}`), nil
		}
		return testResponse(200, `{"message":{"content":"{\"purpose\":\"Starts the server\",\"important_symbols\":[],\"dependencies\":[],\"important_behaviour\":[],\"side_effects\":[]}"}}`), nil
	})}
	summary, err := client.SummarizeFile(context.Background(), "main.go", "go", nil, []string{"func main() {}"})
	if err != nil {
		t.Fatal(err)
	}
	if summary == "" || calls.Load() != 2 {
		t.Fatalf("expected repaired summary after two calls, got %q and %d calls", summary, calls.Load())
	}
}

func TestEmbedFallsBackToLegacyEndpoint(t *testing.T) {
	client := NewClient("http://ollama.test", "summary", "embed", 1)
	client.HTTP = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/api/embed" {
			return testResponse(http.StatusNotFound, "unsupported"), nil
		}
		return testResponse(http.StatusOK, `{"embedding":[0.25,0.75]}`), nil
	})}
	vectors, err := client.Embed(context.Background(), []string{"one", "two"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 2 || len(vectors[0]) != 2 {
		t.Fatalf("unexpected vectors: %#v", vectors)
	}
}

func TestEmbedDoesNotFanOutAfterServerError(t *testing.T) {
	var calls atomic.Int32
	client := NewClient("http://ollama.test", "summary", "embed", 1)
	client.HTTP = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		calls.Add(1)
		return testResponse(http.StatusInternalServerError, "model failed"), nil
	})}
	if _, err := client.Embed(context.Background(), []string{"one", "two", "three"}); err == nil {
		t.Fatal("expected batch embedding error")
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one batch request, got %d", calls.Load())
	}
}

func TestEmbedRejectsMismatchedBatchWithoutLegacyFanout(t *testing.T) {
	var calls atomic.Int32
	client := NewClient("http://ollama.test", "summary", "embed", 1)
	client.HTTP = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		calls.Add(1)
		return testResponse(http.StatusOK, `{"embeddings":[[0.25,0.75]]}`), nil
	})}
	if _, err := client.Embed(context.Background(), []string{"one", "two"}); err == nil {
		t.Fatal("expected embedding count mismatch")
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one batch request, got %d", calls.Load())
	}
}
