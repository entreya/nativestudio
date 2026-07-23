package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// scriptedTransport replays one scripted Ollama /api/chat response per call,
// repeating the last entry once the script runs out (used by the max-steps
// test, which needs the model to keep calling tools indefinitely).
func scriptedTransport(t *testing.T, responses []string) http.RoundTripper {
	t.Helper()
	call := 0
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		index := call
		if index >= len(responses) {
			index = len(responses) - 1
		}
		call++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(responses[index] + "\n")),
			Request:    r,
		}, nil
	})
}

func withTransport(t *testing.T, transport http.RoundTripper) {
	t.Helper()
	original := http.DefaultClient.Transport
	http.DefaultClient.Transport = transport
	t.Cleanup(func() { http.DefaultClient.Transport = original })
}

// runAgentSteps drives AgentRun.Step exactly like Agent.Run does — call,
// then feed the returned messages back in — until done or the step budget
// is exhausted, collecting tool_call/tool_result events along the way.
func runAgentSteps(t *testing.T, run *AgentRun, registry *Registry, maxIterations int) (toolCalls, toolResults []map[string]any) {
	t.Helper()
	var messages []OllamaMessage
	emit := func(event string, data any) {
		m, ok := data.(map[string]any)
		if !ok {
			return
		}
		switch event {
		case "tool_call":
			toolCalls = append(toolCalls, m)
		case "tool_result":
			toolResults = append(toolResults, m)
		}
	}
	for i := 0; i < maxIterations; i++ {
		done, newMessages, err := run.Step(context.Background(), messages, registry, "http://ollama.test", emit)
		if err != nil {
			t.Fatalf("step %d returned an error: %v", i, err)
		}
		messages = newMessages
		if done {
			break
		}
	}
	return toolCalls, toolResults
}

func TestAgentCallsFindFilesThenNarrowsSearch(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "src/services/AuthService.php"), "<?php class AuthService {}")
	mustWriteFile(t, filepath.Join(root, "src/services/UserAuthService.php"), "<?php class UserAuthService {}")
	mustWriteFile(t, filepath.Join(root, "src/controllers/AuthController.go"), "package controllers")

	withTransport(t, scriptedTransport(t, []string{
		`{"message":{"tool_calls":[{"id":"call-1","function":{"name":"find_files","arguments":{"query":"auth"}}}]},"done":true}`,
		`{"message":{"tool_calls":[{"id":"call-2","function":{"name":"find_files","arguments":{"query":"AuthService","extensions":["php"]}}}]},"done":true}`,
		`{"message":{"content":"Found it: src/services/AuthService.php"},"done":true}`,
	}))

	registry := NewRegistry(root)
	run := &AgentRun{Model: "test", MaxSteps: 5}
	toolCalls, toolResults := runAgentSteps(t, run, registry, 5)

	if len(toolCalls) != 2 || toolCalls[0]["name"] != "find_files" || toolCalls[1]["name"] != "find_files" {
		t.Fatalf("expected two find_files calls (broad then narrow), got %#v", toolCalls)
	}
	if len(toolResults) != 2 {
		t.Fatalf("expected two tool results, got %d", len(toolResults))
	}
	// Results must map back to the exact call IDs the model used.
	if toolResults[0]["id"] != "call-1" || toolResults[1]["id"] != "call-2" {
		t.Fatalf("tool results did not map to the correct call IDs: id0=%v id1=%v", toolResults[0]["id"], toolResults[1]["id"])
	}

	broad, ok := toolResults[0]["output"].(findFilesOutput)
	if !ok || len(broad.Matches) < 2 {
		t.Fatalf("expected the broad search to return multiple ranked matches, got %#v", toolResults[0]["output"])
	}
	narrow, ok := toolResults[1]["output"].(findFilesOutput)
	if !ok || len(narrow.Matches) == 0 || narrow.Matches[0].Path != "src/services/AuthService.php" {
		t.Fatalf("expected the narrowed search to rank AuthService.php first, got %#v", toolResults[1]["output"])
	}
}

func TestAgentCallsListDirectoryForFolderRequest(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "modules/auth/AuthController.php"), "<?php")
	mustWriteFile(t, filepath.Join(root, "modules/auth/AuthService.php"), "<?php")

	withTransport(t, scriptedTransport(t, []string{
		`{"message":{"tool_calls":[{"id":"call-1","function":{"name":"list_directory","arguments":{"path":"modules/auth"}}}]},"done":true}`,
		`{"message":{"content":"The auth module has AuthController.php and AuthService.php."},"done":true}`,
	}))

	registry := NewRegistry(root)
	run := &AgentRun{Model: "test", MaxSteps: 5}
	toolCalls, toolResults := runAgentSteps(t, run, registry, 5)

	if len(toolCalls) != 1 || toolCalls[0]["name"] != "list_directory" {
		t.Fatalf("expected a single list_directory call, got %#v", toolCalls)
	}
	output, ok := toolResults[0]["output"].(listDirectoryOutput)
	if !ok || len(output.Entries) != 2 {
		t.Fatalf("expected 2 listed entries, got %#v", toolResults[0]["output"])
	}
}

func TestAgentFileSearchNeverEscapesWorkspace(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "src/main.go"), "package main")

	argsJSON, _ := json.Marshal(map[string]any{"query": "passwd", "path": "../../etc"})
	withTransport(t, scriptedTransport(t, []string{
		`{"message":{"tool_calls":[{"id":"call-1","function":{"name":"find_files","arguments":` + string(argsJSON) + `}}]},"done":true}`,
		`{"message":{"content":"No matches."},"done":true}`,
	}))

	registry := NewRegistry(root)
	run := &AgentRun{Model: "test", MaxSteps: 5}
	_, toolResults := runAgentSteps(t, run, registry, 5)

	if len(toolResults) != 1 {
		t.Fatalf("expected one tool result, got %d", len(toolResults))
	}
	if ok, _ := toolResults[0]["ok"].(bool); ok {
		t.Fatalf("expected the escape attempt to fail, got %#v", toolResults[0])
	}
}

func TestAgentEnforcesMaxToolSteps(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "src/main.go"), "package main")

	// The model never produces a final answer — every response is another
	// tool call — so this only terminates if the step limit is enforced.
	withTransport(t, scriptedTransport(t, []string{
		`{"message":{"tool_calls":[{"function":{"name":"find_files","arguments":{"query":"main"}}}]},"done":true}`,
	}))

	registry := NewRegistry(root)
	const maxSteps = 3
	run := &AgentRun{Model: "test", MaxSteps: maxSteps}

	var messages []OllamaMessage
	var maxStepsHit bool
	emit := func(event string, data any) {
		if event == "error" {
			if m, ok := data.(map[string]any); ok && m["message"] == "max_steps_reached" {
				maxStepsHit = true
			}
		}
	}
	// Run well beyond maxSteps to prove it actually stops there rather than
	// merely being an unenforced upper bound on the loop we drive.
	for i := 0; i < maxSteps+5; i++ {
		done, newMessages, err := run.Step(context.Background(), messages, registry, "http://ollama.test", emit)
		if err != nil {
			t.Fatalf("step %d returned an error: %v", i, err)
		}
		messages = newMessages
		if done {
			break
		}
	}
	if !maxStepsHit {
		t.Fatal("expected max_steps_reached to be emitted once the tool-step budget was exhausted")
	}
	if run.Steps > maxSteps {
		t.Fatalf("expected the run to stop incrementing Steps past MaxSteps, got %d", run.Steps)
	}
}

func TestAgentHandlesInvalidToolArgumentsGracefully(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "src/main.go"), "package main")

	withTransport(t, scriptedTransport(t, []string{
		// Malformed (non-JSON) arguments string for find_files.
		`{"message":{"tool_calls":[{"id":"call-1","function":{"name":"find_files","arguments":"not valid json"}}]},"done":true}`,
		`{"message":{"content":"Please tell me what to search for."},"done":true}`,
	}))

	registry := NewRegistry(root)
	run := &AgentRun{Model: "test", MaxSteps: 5}
	_, toolResults := runAgentSteps(t, run, registry, 5)

	if len(toolResults) != 1 {
		t.Fatalf("expected one tool result, got %d", len(toolResults))
	}
	ok, _ := toolResults[0]["ok"].(bool)
	if ok {
		t.Fatal("expected malformed arguments to fall back to an empty input and fail find_files' required-query check")
	}
	if errMsg, _ := toolResults[0]["error"].(string); errMsg != "query is required" {
		t.Fatalf("expected a graceful \"query is required\" error, got %q", errMsg)
	}
}
