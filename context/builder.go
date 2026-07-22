package context

import (
	"fmt"
	"sort"
	"strings"

	"github.com/entreya/nativestudio/editor"
	"github.com/entreya/nativestudio/knowledge"
)

type ContextItem struct {
	Kind, Path, Symbol         string
	StartLine, EndLine, Tokens int
	Score                      float64
	Reasons                    []string
}
type ModelContext struct {
	EditorAndKnowledge string
	History            []Message
	Items              []ContextItem
	Tokens             int
	Budget             int
}
type BuildInput struct {
	Model      string
	State      editor.EditorState
	History    []Message
	Candidates []knowledge.Candidate
}

func BuildModelContext(input BuildInput) ModelContext {
	total := GetModelContextWindow(input.Model)
	available := total * 75 / 100
	editorBudget := total * 15 / 100
	conversationBudget := total * 15 / 100
	codeBudget := total * 35 / 100
	memoryBudget := total * 10 / 100
	var out ModelContext
	out.Budget = available
	var notes strings.Builder
	appendText := func(kind, path, symbol, text string, start, end int, score float64, reasons []string, budget *int) {
		tokens := EstimateTokens(text)
		if tokens <= 0 || *budget <= 0 {
			return
		}
		if tokens > *budget {
			text = truncateToTokens(text, *budget)
			tokens = EstimateTokens(text)
		}
		if strings.TrimSpace(text) == "" {
			return
		}
		notes.WriteString(fmt.Sprintf("\n[%s] %s", kind, path))
		if symbol != "" {
			notes.WriteString(" › " + symbol)
		}
		if start > 0 {
			notes.WriteString(fmt.Sprintf(" lines %d-%d", start, end))
		}
		notes.WriteString("\n" + text + "\n")
		out.Items = append(out.Items, ContextItem{Kind: kind, Path: path, Symbol: symbol, StartLine: start, EndLine: end, Tokens: tokens, Score: score, Reasons: reasons})
		out.Tokens += tokens
		*budget -= tokens
	}
	if input.State.Selection != nil && input.State.Selection.Text != "" {
		appendText("selected_text", input.State.ActiveFile, "", input.State.Selection.Text, input.State.Selection.StartLine, input.State.Selection.EndLine, 100, []string{"selected_code"}, &editorBudget)
	}
	if input.State.SelectedSymbol != nil {
		symbol := input.State.SelectedSymbol
		appendText("selected_symbol", symbol.FilePath, symbol.Name, symbol.Name, symbol.StartLine, symbol.EndLine, 95, []string{"symbol_under_cursor"}, &editorBudget)
	}
	if input.State.ActiveFileContent != "" {
		appendText("active_file", input.State.ActiveFile, "", input.State.ActiveFileContent, 1, 0, 90, []string{"active_file_buffer"}, &codeBudget)
	}
	activePath := strings.TrimPrefix(strings.ReplaceAll(input.State.ActiveFile, "\\", "/"), "/")
	sort.SliceStable(input.Candidates, func(i, j int) bool { return input.Candidates[i].Score > input.Candidates[j].Score })
	for _, candidate := range input.Candidates {
		if input.State.ActiveFileContent != "" && candidate.Kind == "code" && strings.TrimPrefix(strings.ReplaceAll(candidate.Path, "\\", "/"), "/") == activePath {
			continue
		}
		budget := &codeBudget
		if candidate.Kind == "file_summary" || candidate.Kind == "module_summary" || candidate.Kind == "fact" || candidate.Kind == "decision" || candidate.Kind == "accepted_change" || candidate.Kind == "project_summary" {
			budget = &memoryBudget
		}
		appendText(candidate.Kind, candidate.Path, candidate.Symbol, candidate.Content, candidate.StartLine, candidate.EndLine, candidate.Score, candidate.Reasons, budget)
	}
	for i := len(input.History) - 1; i >= 0; i-- {
		tokens := EstimateTokens(input.History[i].Content)
		if tokens > conversationBudget {
			continue
		}
		out.History = append([]Message{input.History[i]}, out.History...)
		conversationBudget -= tokens
		out.Tokens += tokens
	}
	out.EditorAndKnowledge = notes.String()
	return out
}
func truncateToTokens(value string, tokens int) string {
	limit := tokens * 4
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "\n…[truncated to context budget]"
}
