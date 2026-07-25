package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entreya/nativestudio/db"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestParseTextToolCalls(t *testing.T) {
	registry := NewRegistry(t.TempDir())

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "single textual call",
			content: `{"name":"read_file","arguments":{"path":"main.go"}}`,
			want:    "read_file",
		},
		{
			name:    "ollama envelope",
			content: `{"tool_calls":[{"function":{"name":"ask_follow_up","arguments":{"question":"Which file?","options":["Current file","All files"]}}}]}`,
			want:    "ask_follow_up",
		},
		{
			name:    "ordinary json answer",
			content: `{"answer":"This must remain visible"}`,
		},
		{
			name:    "unknown tool",
			content: `{"name":"delete_everything","arguments":{}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := parseTextToolCalls(tt.content, registry)
			if tt.want == "" {
				if len(calls) != 0 {
					t.Fatalf("expected no calls, got %#v", calls)
				}
				return
			}
			if len(calls) != 1 || calls[0].Function.Name != tt.want {
				t.Fatalf("expected %q call, got %#v", tt.want, calls)
			}
		})
	}
}

func TestToolArgumentsAcceptObjectAndString(t *testing.T) {
	for _, payload := range []string{
		`{"function":{"name":"read_file","arguments":{"path":"main.go"}}}`,
		`{"function":{"name":"read_file","arguments":"{\"path\":\"main.go\"}"}}`,
	} {
		var call OllamaToolCall
		if err := json.Unmarshal([]byte(payload), &call); err != nil {
			t.Fatal(err)
		}
		var args ToolInput
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil || args["path"] != "main.go" {
			t.Fatalf("unexpected arguments %q: %#v, %v", call.Function.Arguments, args, err)
		}
	}
}

func TestStringSlice(t *testing.T) {
	got := stringSlice([]any{"One", "", 3, "Two"})
	if len(got) != 2 || got[0] != "One" || got[1] != "Two" {
		t.Fatalf("unexpected options: %#v", got)
	}
}

func TestNormalizeThinkLevel(t *testing.T) {
	tests := map[string]string{
		"light": "low", "medium": "medium", "high": "high",
		"extra": "high", "supreme": "high", "unknown": "medium",
	}
	for input, want := range tests {
		if got := normalizeThinkLevel(input); got != want {
			t.Fatalf("normalizeThinkLevel(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSupportsNativeThinking(t *testing.T) {
	for _, model := range []string{"qwen3:4b", "deepseek-r1:latest", "deepseek-v3.1", "gpt-oss:20b"} {
		if !supportsNativeThinking(model) {
			t.Fatalf("expected %q to support native thinking", model)
		}
	}
	for _, model := range []string{"qwen2.5-coder:7b", "phi4-mini:latest", "llama3.2"} {
		if supportsNativeThinking(model) {
			t.Fatalf("did not expect %q to support native thinking", model)
		}
	}
}

func TestLightweightConversationDetection(t *testing.T) {
	for _, prompt := range []string{"hi", "Hello!", "good morning", "How are you?", "thanks"} {
		if !isLightweightConversation(prompt) {
			t.Fatalf("expected %q to use direct conversation", prompt)
		}
	}
	for _, prompt := range []string{"hi, explain this file", "remove hello action", "how does login work?"} {
		if isLightweightConversation(prompt) {
			t.Fatalf("expected %q to use the coding agent", prompt)
		}
	}
}

func TestDirectStepOmitsToolsAndPlanning(t *testing.T) {
	originalTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if _, exists := payload["tools"]; exists {
			t.Fatal("direct conversation unexpectedly sent tool definitions")
		}
		body := `{"message":{"content":"Hello!"},"done":true}` + "\n"
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	t.Cleanup(func() { http.DefaultClient.Transport = originalTransport })

	run := &AgentRun{Model: "test", MaxSteps: 1, Direct: true}
	planned := false
	done, _, err := run.Step(context.Background(), nil, NewRegistry(t.TempDir()), "http://ollama.test", func(event string, _ any) {
		if event == "agent_step" {
			planned = true
		}
	})
	if err != nil || !done || planned {
		t.Fatalf("unexpected direct result: done=%v planned=%v err=%v", done, planned, err)
	}
}

func TestGenerateConversationMetadata(t *testing.T) {
	originalTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["stream"] != false || payload["format"] == nil {
			t.Fatalf("expected a non-streaming structured metadata request, got %#v", payload)
		}
		body := `{"message":{"content":"{\"title\":\"Remove Hello Action\",\"summary\":\"The user asked to remove the hello action from SiteController.php.\"}"}}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})
	t.Cleanup(func() { http.DefaultClient.Transport = originalTransport })

	agent := &Agent{OllamaURL: "http://ollama.test"}
	title, summary, err := agent.generateConversationMetadata(
		context.Background(),
		"qwen2.5-coder:1.5b",
		"New Conversation",
		"",
		"Remove the hello action",
		"I prepared the updated controller.",
	)
	if err != nil {
		t.Fatal(err)
	}
	if title != "Remove Hello Action" || !strings.Contains(summary, "SiteController.php") {
		t.Fatalf("unexpected metadata: %q, %q", title, summary)
	}
}

func TestUnsupportedModelDoesNotSendThinkField(t *testing.T) {
	originalTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if _, exists := payload["think"]; exists {
			t.Fatalf("unsupported model received think field: %#v", payload["think"])
		}
		body := `{"message":{"content":"Hi"},"done":true,"prompt_eval_count":4,"eval_count":1}` + "\n"
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})
	t.Cleanup(func() { http.DefaultClient.Transport = originalTransport })

	run := &AgentRun{Model: "qwen2.5-coder:7b", Think: true, ThinkLevel: "high", MaxSteps: 2}
	done, _, err := run.Step(context.Background(), nil, NewRegistry(t.TempDir()), "http://ollama.test", func(string, any) {})
	if err != nil || !done {
		t.Fatalf("unexpected result: done=%v err=%v", done, err)
	}
}

func TestParseNarratedFollowUp(t *testing.T) {
	got, ok := parseNarratedFollowUp("Ask the user whether they are okay with removing the action.")
	if !ok {
		t.Fatal("expected narrated follow-up to be recognized")
	}
	if got["input_type"] != "select" {
		t.Fatalf("expected select input, got %#v", got)
	}
	if options, ok := got["options"].([]string); !ok || len(options) != 2 {
		t.Fatalf("expected yes/no options, got %#v", got["options"])
	}
}

func TestStepPausesForFollowUp(t *testing.T) {
	originalTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"message":{"tool_calls":[{"function":{"name":"ask_follow_up","arguments":"{\"question\":\"Which scope?\",\"options\":[\"Current file\",\"Whole project\"],\"input_type\":\"multiselect\"}"}}]},"done":true}` + "\n"
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})
	t.Cleanup(func() { http.DefaultClient.Transport = originalTransport })

	registry := NewRegistry(t.TempDir())
	run := &AgentRun{Model: "test", MaxSteps: 2}
	var followUp map[string]any
	emit := func(event string, data any) {
		if event == "follow_up" {
			followUp, _ = data.(map[string]any)
		}
	}

	done, messages, err := run.Step(context.Background(), nil, registry, "http://ollama.test", emit)
	if err != nil {
		t.Fatal(err)
	}
	if !done {
		t.Fatal("expected the run to pause")
	}
	if followUp == nil || followUp["question"] != "Which scope?" {
		t.Fatalf("unexpected follow-up event: %#v", followUp)
	}
	if followUp["input_type"] != "multiselect" {
		t.Fatalf("expected multiselect follow-up, got %#v", followUp)
	}
	if len(messages) == 0 || messages[len(messages)-1].Content == "" {
		t.Fatalf("clarification was not retained in history: %#v", messages)
	}
}

func TestFallbackConversationMetadataReplacesPlaceholder(t *testing.T) {
	title, summary := fallbackConversationMetadata("remove hello action from SiteController", "Prepared the requested edit.")
	if title == "" || title == "New Conversation" {
		t.Fatalf("unexpected fallback title %q", title)
	}
	if !strings.Contains(summary, "remove hello action") {
		t.Fatalf("unexpected fallback summary %q", summary)
	}
}

func TestCurrentQuestionsTriggerInternetSearch(t *testing.T) {
	if !needsInternetSearch("Do you know about the recent protest on 20th July at Jantar Mantar?") {
		t.Fatal("expected a recent protest question to trigger internet search")
	}
	query := currentSearchQuery("recent protest on 20th July", time.Date(2026, time.July, 22, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(query, "2026") {
		t.Fatalf("expected current year in query, got %q", query)
	}
	if needsInternetSearch("Explain this Go function") {
		t.Fatal("did not expect a stable code question to trigger internet search")
	}
	focused := currentSearchQuery("do you know about recent protestt on 20th july at jantar mmantar", time.Date(2026, time.July, 22, 0, 0, 0, 0, time.UTC))
	if focused != "protestt 20 july jantar mmantar 2026" {
		t.Fatalf("unexpected focused query %q", focused)
	}
	generic := currentSearchQuery("Can you tell me about the latest Aurora Bridge closure?", time.Date(2026, time.July, 22, 0, 0, 0, 0, time.UTC))
	if generic != "aurora bridge closure 2026" {
		t.Fatalf("unexpected generic query %q", generic)
	}
}

func TestSearchResultRelevanceFiltering(t *testing.T) {
	candidates := []internetSearchResult{
		{Title: "DO English meaning", Description: "Definition of the verb do"},
		{Title: "Jantar Mantar protest", Description: "Protesters gathered in Delhi on July 20"},
	}
	results := filterRelevantResults("protestt 20 july jantar mmantar 2026", candidates)
	if len(results) != 1 || results[0].Title != "Jantar Mantar protest" {
		t.Fatalf("unexpected filtered results: %#v", results)
	}
}

func TestSuccessfulSearchRefusalGetsGroundedFallback(t *testing.T) {
	refusal := "I don't have specific knowledge because my training data is old. Would you like help finding articles?"
	if !responseIgnoredSuccessfulSearch(refusal) {
		t.Fatal("expected cutoff response to be rejected after successful search")
	}
	results := []internetSearchResult{{Title: "Current event report", URL: "https://example.com/report"}}
	answer := groundedSearchFallback("current event 2026", results)
	if !strings.Contains(answer, "Current event report") || !strings.Contains(answer, "https://example.com/report") {
		t.Fatalf("fallback was not grounded in results: %q", answer)
	}
	if !responseGroundedInSuccessfulSearch("Current reports confirm the event; source: https://example.com/report", results) {
		t.Fatal("expected a response containing a retrieved URL to be accepted")
	}
	if responseGroundedInSuccessfulSearch("Current reports confirm the event without a citation.", results) {
		t.Fatal("expected an uncited response to be replaced")
	}
}

// TestGroundingFallbackDoesNotOverrideATimeoutMessage reproduces a live bug:
// asking "what is today's date" triggered a preflight search (webResults
// non-empty), then the model's generation — and its one corrective-nudge
// retry — both blew the thinking-token budget. That correctly produced the
// graceful "I'm having trouble reaching a clear answer..." message, but the
// grounding-fallback check right after it didn't know about
// ThinkingBudgetExceeded and, seeing an "ungrounded" message with no cited
// URL, replaced it anyway with a fabricated "Yes, I found current reporting
// that matches your question" answer built from irrelevant search results.
func TestGroundingFallbackDoesNotOverrideATimeoutMessage(t *testing.T) {
	results := []internetSearchResult{{Title: "Unrelated result", URL: "https://example.com/unrelated"}}
	timeoutMessage := "I'm having trouble reaching a clear answer for this without taking too long. Could you rephrase the question or break it into a smaller one?"

	if shouldApplyGroundingFallback(false, true, false, results, false, timeoutMessage) {
		t.Fatal("expected the timeout message to be left alone when ThinkingBudgetExceeded is true")
	}
	if shouldApplyGroundingFallback(false, true, false, results, false, "") {
		t.Fatal("expected an empty budget-exceeded result to also be left alone (no fabricated fallback)")
	}
	// Sanity check the flag actually matters: with it false, the same
	// ungrounded content should still get replaced as before.
	if !shouldApplyGroundingFallback(false, false, false, results, false, timeoutMessage) {
		t.Fatal("expected an ungrounded, non-timeout message to still be replaced")
	}
}

func TestStepStagesReviewablePatch(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "controllers"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "controllers", "SiteController.php"), []byte("<?php old"), 0644); err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(filepath.Join(t.TempDir(), "agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	project, err := database.CreateProject("project", "Project", root, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateSession("session", project.ID, "test", "Patch test"); err != nil {
		t.Fatal(err)
	}
	originalTransport := http.DefaultClient.Transport
	http.DefaultClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"message":{"tool_calls":[{"function":{"name":"replace_in_file","arguments":{"path":"controllers/SiteController.php","old_text":"<?php old","new_text":"<?php updated"}}}]},"done":true}` + "\n"
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})
	t.Cleanup(func() { http.DefaultClient.Transport = originalTransport })

	run := &AgentRun{RunID: "run", SessionID: "session", Model: "test", MaxSteps: 2, DB: database}
	var staged map[string]any
	done, messages, err := run.Step(context.Background(), nil, NewRegistry(root), "http://ollama.test", func(event string, data any) {
		if event == "patch_staged" {
			staged, _ = data.(map[string]any)
		}
	})
	if err != nil || done {
		t.Fatalf("unexpected result: done=%v err=%v", done, err)
	}
	if staged["file_path"] != "controllers/SiteController.php" || staged["operation"] != "modify" {
		t.Fatalf("unexpected staged patch: %#v", staged)
	}
	if len(messages) == 0 || messages[len(messages)-1].Role != "tool" {
		t.Fatalf("expected a tool result for the next agent step, got %#v", messages)
	}
	patches, err := database.ListPatches(context.Background(), "session", "pending")
	if err != nil || len(patches) != 1 || patches[0].NewContent != "<?php updated" {
		t.Fatalf("patch was not persisted: %#v err=%v", patches, err)
	}
}

// TestThinkingBudgetStopsRunawayReasoning reproduces (deterministically, via a
// mocked transport rather than waiting on a real slow model) the failure mode
// found live: a model that streams thinking chunks indefinitely without ever
// reaching content or a tool call. Without maxThinkingTokens, Step would just
// keep scanning every line the mock hands it and never return.
func TestThinkingBudgetStopsRunawayReasoning(t *testing.T) {
	originalTransport := http.DefaultClient.Transport
	// Far more thinking-only chunks than maxThinkingTokens, and no "done":true
	// line at all — if Step read the whole body, it would hang waiting for a
	// done signal that never comes. A well-behaved cutoff must stop reading
	// well before line maxThinkingTokens+1.
	var body strings.Builder
	for i := 0; i < defaultMaxThinkingTokens*2; i++ {
		body.WriteString(`{"message":{"thinking":"still thinking "}}` + "\n")
	}
	http.DefaultClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body.String())),
			Request:    r,
		}, nil
	})
	t.Cleanup(func() { http.DefaultClient.Transport = originalTransport })

	run := &AgentRun{Model: "test", MaxSteps: 1}
	done, messages, err := run.Step(context.Background(), nil, NewRegistry(t.TempDir()), "http://ollama.test", func(string, any) {})
	if err != nil {
		t.Fatalf("expected a clean stop, not an error: %v", err)
	}
	if !done {
		t.Fatal("expected Step to report done=true once the thinking budget is exceeded")
	}
	if !run.ThinkingBudgetExceeded {
		t.Fatal("expected ThinkingBudgetExceeded to be set")
	}
	if len(messages) != 0 {
		t.Fatalf("expected no assistant message appended for an aborted generation, got %#v", messages)
	}
}

// TestStepNumbersDoNotRepeatAfterAnAbortedStep reproduces a live bug: a step
// that gets cut short (thinking budget exceeded, or the corrective-nudge
// retry in agent.go) used to leave run.Steps unincremented, so the very
// next Step call emitted "agent_step" with the same number as the aborted
// one. The frontend timeline keys each step's row by that number, so a
// repeat collided (a duplicate React key — one row silently never resolves
// out of "running", exactly what surfaced as a permanently stuck "Planning
// the next action" spinner during a live session).
func TestStepNumbersDoNotRepeatAfterAnAbortedStep(t *testing.T) {
	originalTransport := http.DefaultClient.Transport
	t.Cleanup(func() { http.DefaultClient.Transport = originalTransport })

	var runaway strings.Builder
	for i := 0; i < defaultMaxThinkingTokens*2; i++ {
		runaway.WriteString(`{"message":{"thinking":"still thinking "}}` + "\n")
	}
	http.DefaultClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(runaway.String())), Request: r}, nil
	})

	run := &AgentRun{Model: "test", MaxSteps: 5}
	var stepNumbers []int
	emit := func(event string, data any) {
		if event != "agent_step" {
			return
		}
		if m, ok := data.(map[string]any); ok {
			stepNumbers = append(stepNumbers, m["step"].(int))
		}
	}

	done, _, err := run.Step(context.Background(), nil, NewRegistry(t.TempDir()), "http://ollama.test", emit)
	if err != nil || !done || !run.ThinkingBudgetExceeded {
		t.Fatalf("expected the first step to abort on the thinking budget: done=%v err=%v exceeded=%v", done, err, run.ThinkingBudgetExceeded)
	}

	http.DefaultClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"message":{"content":"done"},"done":true}` + "\n"
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	if _, _, err := run.Step(context.Background(), nil, NewRegistry(t.TempDir()), "http://ollama.test", emit); err != nil {
		t.Fatalf("second step returned an error: %v", err)
	}

	if len(stepNumbers) != 2 {
		t.Fatalf("expected exactly 2 agent_step emissions, got %v", stepNumbers)
	}
	if stepNumbers[0] == stepNumbers[1] {
		t.Fatalf("expected the second step's number to differ from the aborted first one, got %v twice", stepNumbers[0])
	}
}
