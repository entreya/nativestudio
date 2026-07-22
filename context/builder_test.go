package context

import (
	"strings"
	"testing"

	"github.com/entreya/nativestudio/editor"
	"github.com/entreya/nativestudio/knowledge"
)

func TestBuildModelContextPrioritizesEditorAndFitsBudget(t *testing.T) {
	large := strings.Repeat("code ", 20000)
	result := BuildModelContext(BuildInput{Model: "unknown-small-model", State: editor.EditorState{ActiveFile: "main.go", ActiveFileContent: large, Selection: &editor.Selection{StartLine: 1, EndLine: 1, Text: "selected"}}, History: []Message{{Role: "user", Content: "recent"}}, Candidates: []knowledge.Candidate{{Kind: "file_summary", Path: "service.go", Content: "service summary", Score: 30, Reasons: []string{"summary_keyword_relevance"}}}})
	if len(result.Items) == 0 || result.Items[0].Kind != "selected_text" {
		t.Fatalf("selection was not first: %#v", result.Items)
	}
	if result.Tokens > result.Budget {
		t.Fatalf("context exceeded budget: %d > %d", result.Tokens, result.Budget)
	}
	if !strings.Contains(result.EditorAndKnowledge, "truncated to context budget") {
		t.Fatal("expected oversized active buffer to be truncated")
	}
}
