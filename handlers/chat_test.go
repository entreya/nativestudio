package handlers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entreya/nativestudio/agent"
	ctxpkg "github.com/entreya/nativestudio/context"
	"github.com/entreya/nativestudio/db"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// scriptedOllamaResponse serves a single canned /api/chat reply to every
// request, mirroring the NDJSON envelope agent/loop.go's streaming reader
// expects.
func scriptedOllamaResponse(t *testing.T, content string) http.RoundTripper {
	t.Helper()
	body := `{"message":{"content":"` + content + `"},"done":true}` + "\n"
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})
}

func withDefaultTransport(t *testing.T, transport http.RoundTripper) {
	t.Helper()
	original := http.DefaultClient.Transport
	http.DefaultClient.Transport = transport
	t.Cleanup(func() { http.DefaultClient.Transport = original })
}

func newChatTestHandler(t *testing.T) (*ChatHandler, *db.DB) {
	t.Helper()
	root := t.TempDir()
	database, err := db.Open(filepath.Join(t.TempDir(), "chat-test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := database.CreateProject("project", "Project", root, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateSession("session", "project", "test", "Test"); err != nil {
		t.Fatal(err)
	}
	ctxpkg.Store = ctxpkg.NewDBStore(database)

	registry := agent.NewRegistry(root)
	agentInstance := &agent.Agent{OllamaURL: "http://ollama.test", Registry: registry, DB: database, MaxToolSteps: 5}
	handler := NewChatHandler("http://ollama.test", ctxpkg.Config{YellowThreshold: 0.7, RedThreshold: 0.9}, agentInstance, database)
	return handler, database
}

// TestHandleChatSurvivesClientDisconnect is the regression test for a live
// bug: switching to another page (Database Explorer, Knowledge, Conversations)
// unmounts ChatPanel, whose cleanup effect aborts its in-flight fetch to
// /api/chat. That closes the underlying HTTP connection, which cancels
// r.Context() on the Go side. HandleChat used to pass r.Context() straight
// into Agent.Run, so the cancellation reached the Ollama call the run was
// making mid-generation and killed it outright — and since Agent.Run only
// saves the assistant reply after the loop finishes normally, a response
// that was most of the way through generating was discarded completely, not
// just left undelivered. The fix detaches the run onto its own context, so
// it keeps going and saves its answer even once the client is gone.
//
// This test cancels the request's context BEFORE calling the handler at all
// — the sharpest form of "the client already left" — and confirms the
// assistant's reply still lands in the database.
func TestHandleChatSurvivesClientDisconnect(t *testing.T) {
	handler, database := newChatTestHandler(t)
	withDefaultTransport(t, scriptedOllamaResponse(t, "Hello! How can I help?"))

	body := `{"prompt":"hello","model":"test","session_id":"session"}`
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(body))

	cancelledCtx, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(cancelledCtx)

	handler.HandleChat(httptest.NewRecorder(), req)

	messages, err := database.GetMessages("session")
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}

	var gotAssistantReply bool
	for _, m := range messages {
		if m.Role == "assistant" && strings.Contains(m.Content, "Hello! How can I help?") {
			gotAssistantReply = true
		}
	}
	if !gotAssistantReply {
		t.Fatalf("expected the assistant's reply to be saved despite the request context being cancelled, got messages: %+v", messages)
	}
}
