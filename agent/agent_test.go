package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entreya/nativestudio/db"
	"github.com/entreya/nativestudio/editor"

	ctxpkg "github.com/entreya/nativestudio/context"
)

func TestNeedsInternetSearchIgnoresCodingCurrentPhrases(t *testing.T) {
	notSearch := []string{
		"install a basic yii2 app in the current folder",
		"what does the current file do",
		"list the current directory",
		"show me the current branch",
		"explain the current project structure",
	}
	for _, prompt := range notSearch {
		if needsInternetSearch(prompt) {
			t.Errorf("expected %q to NOT trigger an internet search", prompt)
		}
	}

	shouldSearch := []string{
		"what's the current situation in the news today",
		"what is currently happening with the election",
		"give me the latest PHP release",
		"what's the weather like",
	}
	for _, prompt := range shouldSearch {
		if !needsInternetSearch(prompt) {
			t.Errorf("expected %q to trigger an internet search", prompt)
		}
	}
}

// TestPromptRequestsExplanationGatesRephrasing verifies the guard added
// after a live finding: rephraserModel (agent/rephrase.go) doesn't just
// rewrite an "explain this function" style request — it answers it,
// fabricating a description of code it has never seen. Prompts matching
// this pattern must skip the rephrase call entirely (Agent.Run uses this to
// decide) rather than risk that.
func TestPromptRequestsExplanationGatesRephrasing(t *testing.T) {
	shouldSkip := []string{
		"explain this function",
		"can you describe what SiteController does",
		"please review this code",
		"what does this method do",
		"how does the resolver work",
		"analyze this file for bugs",
		"summarize this file",
	}
	for _, prompt := range shouldSkip {
		if !promptRequestsExplanation(prompt) {
			t.Errorf("expected %q to be detected as an explanation request", prompt)
		}
	}

	shouldRephrase := []string{
		"help method bana jimsme 5000 loop likha ho",
		"SiteController mein hello action hata do",
		"install a basic yii2 app in the current folder",
		"rename this variable to userCount",
	}
	for _, prompt := range shouldRephrase {
		if promptRequestsExplanation(prompt) {
			t.Errorf("expected %q to NOT be detected as an explanation request", prompt)
		}
	}
}

func newAgentTestEnvironment(t *testing.T) (*Agent, string, string) {
	t.Helper()
	root := t.TempDir()
	database, err := db.Open(filepath.Join(t.TempDir(), "agent-test.db"))
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

	registry := NewRegistry(root)
	agentInstance := &Agent{OllamaURL: "http://ollama.test", Registry: registry, DB: database, MaxToolSteps: 5}
	return agentInstance, root, "session"
}

// TestAgentRetriesWhenModelDescribesInsteadOfActing exercises the exact bug
// reported live: asked to create a file, a small local model sometimes
// answers with a code block explaining the change instead of calling
// create_file, even though the system prompt forbids code blocks in chat
// responses. Agent.Run should notice that (a fenced code block with zero
// tool calls made) and give the model one corrective nudge — and, critically,
// must keep stepping after that nudge if the model's corrected response is
// itself a tool call rather than immediately final text.
func TestAgentRetriesWhenModelDescribesInsteadOfActing(t *testing.T) {
	agentInstance, root, sessionID := newAgentTestEnvironment(t)

	withTransport(t, scriptedTransport(t, []string{
		// First attempt: describes the change in prose with a code block, no tool call.
		`{"message":{"content":"Sure, create HelpController.php with:\n` + "```php" + `\n<?php class HelpController {}\n` + "```" + `"},"done":true}`,
		// After the corrective nudge: model does the right thing, calls create_file.
		`{"message":{"tool_calls":[{"id":"call-1","function":{"name":"create_file","arguments":{"path":"HelpController.php","content":"<?php class HelpController {}"}}}]},"done":true}`,
		// Loop must continue past the tool call to reach a real final answer.
		`{"message":{"content":"Created HelpController.php."},"done":true}`,
	}))

	var toolCalls []string
	emit := func(event string, data any) {
		if event != "tool_call" {
			return
		}
		if m, ok := data.(map[string]any); ok {
			toolCalls = append(toolCalls, m["name"].(string))
		}
	}

	finalContent, err := agentInstance.Run(context.Background(), sessionID, "create a file named HelpController which says hi", "test", false, "", editor.EditorState{}, emit)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}

	if len(toolCalls) != 1 || toolCalls[0] != "create_file" {
		t.Fatalf("expected the corrective nudge to result in exactly one create_file call, got %v", toolCalls)
	}
	if strings.Contains(finalContent, "```") {
		t.Fatalf("final content should not still contain a code block after the retry, got %q", finalContent)
	}
	if finalContent != "Created HelpController.php." {
		t.Fatalf("expected the final answer to come from after the tool call completed, got %q", finalContent)
	}
	if root == "" {
		t.Fatal("workspace root should be set") // sanity check the test env itself
	}
}

// TestAgentDoesNotRetryWhenToolAlreadyCalled makes sure the safety net does
// not fire (and cannot loop forever) when the model already used a tool but
// its final summary happens to include a code block for some other reason
// (e.g. quoting existing file content).
func TestAgentDoesNotRetryWhenToolAlreadyCalled(t *testing.T) {
	agentInstance, _, sessionID := newAgentTestEnvironment(t)

	withTransport(t, scriptedTransport(t, []string{
		`{"message":{"tool_calls":[{"id":"call-1","function":{"name":"read_file","arguments":{"path":"HelpController.php"}}}]},"done":true}`,
		`{"message":{"content":"Here is the existing file:\n` + "```php" + `\n<?php class HelpController {}\n` + "```" + `"},"done":true}`,
	}))

	callCount := 0
	emit := func(event string, data any) {
		if event == "tool_call" {
			callCount++
		}
	}

	finalContent, err := agentInstance.Run(context.Background(), sessionID, "show me HelpController.php", "test", false, "", editor.EditorState{}, emit)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("expected exactly one tool call (no corrective retry triggered), got %d", callCount)
	}
	if !strings.Contains(finalContent, "```") {
		t.Fatalf("expected the original response (with its code block) to be left alone, got %q", finalContent)
	}
}

// TestBudgetExceededTwiceDoesNotEchoAnEarlierTurnsAnswer reproduces a live
// bug: in a multi-turn conversation, asking the same question twice — where
// both the initial attempt and its one corrective-nudge retry blow the
// thinking-token budget without producing anything new — echoed the FIRST
// turn's real answer back as if it were a fresh response to the SECOND
// turn, because the old logic scanned all of `messages` backward for any
// assistant-role entry, and a prior turn's genuine reply is still sitting
// right there in history. It should instead recognize that this turn
// produced nothing new and fall back to the graceful timeout message.
func TestBudgetExceededTwiceDoesNotEchoAnEarlierTurnsAnswer(t *testing.T) {
	agentInstance, _, sessionID := newAgentTestEnvironment(t)

	const earlierAnswer = "Yes. I found current reporting that matches your question. Here are the most relevant reports: ..."
	ctxpkg.Store.AppendMessage(sessionID, ctxpkg.Message{Role: "user", Content: "what is todays date"})
	ctxpkg.Store.AppendMessage(sessionID, ctxpkg.Message{Role: "assistant", Content: earlierAnswer})

	var runaway strings.Builder
	for i := 0; i < defaultMaxThinkingTokens*2; i++ {
		runaway.WriteString(`{"message":{"thinking":"still thinking "}}` + "\n")
	}
	// Both the first attempt and the corrective-nudge retry blow the budget —
	// scriptedTransport serves the same runaway body to every request.
	withTransport(t, scriptedTransport(t, []string{runaway.String(), runaway.String()}))

	finalContent, err := agentInstance.Run(context.Background(), sessionID, "what is todays date", "test", false, "", editor.EditorState{}, func(string, any) {})
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if strings.Contains(finalContent, earlierAnswer) || finalContent == earlierAnswer {
		t.Fatalf("expected the stale earlier-turn answer NOT to be echoed back, got %q", finalContent)
	}
	if !strings.Contains(finalContent, "having trouble") {
		t.Fatalf("expected the graceful timeout message, got %q", finalContent)
	}
}

// TestAgentNudgesPastRunawayThinking reproduces (deterministically) the live
// failure where a small model spent thousands of thinking tokens re-deriving
// the same conclusion without ever committing to an action. The first
// scripted response simulates that — far more thinking-only lines than
// maxThinkingTokens, no done — which run.Step must cut off on its own rather
// than reading to completion. Run should then give it one direct nudge
// ("stop analyzing, act now") and use the second scripted response — a real
// tool call — as the actual result, the same "one corrective chance" pattern
// already proven for narratedInsteadOfActing.
func TestAgentNudgesPastRunawayThinking(t *testing.T) {
	agentInstance, _, sessionID := newAgentTestEnvironment(t)

	var runaway strings.Builder
	for i := 0; i < defaultMaxThinkingTokens*2; i++ {
		runaway.WriteString(`{"message":{"thinking":"still thinking "}}` + "\n")
	}

	withTransport(t, scriptedTransport(t, []string{
		runaway.String(),
		`{"message":{"tool_calls":[{"id":"call-1","function":{"name":"read_file","arguments":{"path":"HelpController.php"}}}]},"done":true}`,
		`{"message":{"content":"Here is the file."},"done":true}`,
	}))

	var toolCalls []string
	emit := func(event string, data any) {
		if event != "tool_call" {
			return
		}
		if m, ok := data.(map[string]any); ok {
			toolCalls = append(toolCalls, m["name"].(string))
		}
	}

	finalContent, err := agentInstance.Run(context.Background(), sessionID, "show me HelpController.php", "test", false, "", editor.EditorState{}, emit)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if len(toolCalls) != 1 || toolCalls[0] != "read_file" {
		t.Fatalf("expected the thinking-budget nudge to result in exactly one read_file call, got %v", toolCalls)
	}
	if finalContent != "Here is the file." {
		t.Fatalf("expected the final answer to come from after the nudge's tool call completed, got %q", finalContent)
	}
}
