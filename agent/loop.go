package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/entreya/nativestudio/db"
	"github.com/entreya/nativestudio/editor"
)

// OllamaMessage represents a single message in the Ollama conversation format.
type OllamaMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	Thinking   string           `json:"thinking,omitempty"`
	ToolCalls  []OllamaToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

// OllamaToolCall represents a tool call requested by the model.
type OllamaToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string        `json:"name"`
		Arguments ToolArguments `json:"arguments"`
	} `json:"function"`
}

// ToolArguments accepts both Ollama's current object form and the older
// JSON-encoded string form used by some local model templates.
type ToolArguments string

func (a *ToolArguments) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var encoded string
		if err := json.Unmarshal(data, &encoded); err != nil {
			return err
		}
		*a = ToolArguments(encoded)
		return nil
	}
	*a = ToolArguments(data)
	return nil
}

func (a ToolArguments) MarshalJSON() ([]byte, error) {
	if a == "" {
		return []byte(`{}`), nil
	}
	if !json.Valid([]byte(a)) {
		return json.Marshal(string(a))
	}
	return []byte(a), nil
}

// AgentRun tracks the state of a single agent execution loop.
type AgentRun struct {
	RunID      string
	SessionID  string
	Model      string
	Steps      int
	MaxSteps   int
	Think      bool
	ThinkLevel string
	// Direct disables tools and agent-planning events for lightweight conversation.
	Direct bool
	// State holds the editor context for the get_editor_context tool.
	State editor.EditorState
	DB    *db.DB
	// Paused is set when the run stopped to ask the user a clarifying
	// question (via ask_follow_up or a narrated equivalent) rather than to
	// give a final answer — a legitimate reason to have made zero tool
	// calls, so Run()'s corrective retry must not treat it as the
	// "described the action instead of doing it" failure mode.
	Paused bool
	// ThinkingBudgetExceeded is set when Step aborted a generation that blew
	// past maxThinkingTokens without producing content or a tool call —
	// confirmed via live reproduction that this small model can otherwise
	// spend thousands of tokens re-deriving the same conclusion in slightly
	// different words without ever committing to an answer. Run() checks this
	// to give the model one direct nudge to stop analyzing and act, the same
	// pattern already used for narratedInsteadOfActing.
	ThinkingBudgetExceeded bool
}

// DefaultMaxSteps is the maximum number of tool-call iterations before the agent stops.
const DefaultMaxSteps = 15

// defaultMaxThinkingTokens caps how many thinking chunks a single generation
// may stream before Step aborts it as a runaway reasoning loop, unless the
// user has overridden it from the Settings page (currentMaxThinkingTokens,
// agent/settings.go). Chosen from a live-reproduced pathological case (8,792
// tokens / ~7 minutes, never concluding) versus normal multi-paragraph
// reasoning observed elsewhere in the same testing (a few hundred tokens) —
// generous enough for real reasoning, well short of the failure mode.
const defaultMaxThinkingTokens = 1200

// ollamaKeepAlive bounds how long Ollama keeps a model resident in memory
// after the last request (Ollama's own default is 5 minutes). On a 16GB
// machine, a 7GB+ model held resident for hours of back-to-back local
// development — each request resetting the timer before it ever fires —
// starves everything else of free memory, forcing heavy swap/compression
// that shows up as sustained system-wide CPU load and heat.
const ollamaKeepAlive = "2m"

// Step executes one iteration of the agent loop:
// 1. Sends messages to Ollama with tool definitions
// 2. Streams the response
// 3. If the model calls tools, executes them and returns done=false
// 4. If the model produces a final text response, returns done=true
func (run *AgentRun) Step(
	ctx context.Context,
	messages []OllamaMessage,
	registry *Registry,
	ollamaURL string,
	emit func(string, any),
) (bool, []OllamaMessage, error) {

	if run.Steps >= run.MaxSteps {
		emit("error", map[string]any{"message": "max_steps_reached"})
		return true, messages, nil
	}
	// Incremented here, on entry, rather than on a successful return at the
	// bottom of Step: a step that gets cut short (thinking-budget exceeded,
	// a corrective nudge retry) must still consume a step number. Otherwise
	// two separate Step calls emit "agent_step" with the same number — the
	// frontend timeline keys entries by step number, so a repeat collides
	// (duplicate React key, one entry silently never resolves out of
	// "running") — and, separately, a run that kept hitting an aborted step
	// without ever incrementing could retry forever without ever tripping
	// the MaxSteps limit.
	run.Steps++
	if !run.Direct {
		emit("agent_step", map[string]any{"step": run.Steps, "message": "Planning the next action"})
	}

	// Bound the Ollama round trip so a wedged model/server can't hold the SSE
	// connection open forever. Generous, since local generation on modest
	// hardware with a long context can legitimately take minutes.
	stepCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	// Build Ollama request
	ollamaReq := map[string]any{
		"model":      run.Model,
		"messages":   messages,
		"stream":     true,
		"keep_alive": nextChatKeepAlive(),
		"options":    map[string]any{"temperature": currentResponseTemperature()},
	}
	if !run.Direct {
		ollamaReq["tools"] = registry.OllamaDefinitions()
	}
	if run.Think && supportsNativeThinking(run.Model) {
		level := normalizeThinkLevel(run.ThinkLevel)
		if strings.HasPrefix(strings.ToLower(run.Model), "gpt-oss") {
			ollamaReq["think"] = level
		} else {
			ollamaReq["think"] = true
		}
	}

	// Call Ollama /api/chat. Older Ollama builds and some model templates reject
	// the think field with HTTP 400; retry once without it instead of failing the
	// whole conversation.
	doRequest := func(payload map[string]any) (*http.Response, error) {
		reqBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		httpReq, err := http.NewRequestWithContext(stepCtx, http.MethodPost, ollamaURL+"/api/chat", bytes.NewReader(reqBytes))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		return http.DefaultClient.Do(httpReq)
	}

	resp, err := doRequest(ollamaReq)
	if err != nil {
		return true, messages, fmt.Errorf("ollama unreachable: %w", err)
	}
	if resp.StatusCode == http.StatusBadRequest && ollamaReq["think"] != nil {
		resp.Body.Close()
		delete(ollamaReq, "think")
		emit("thinking_unavailable", map[string]any{"message": "This model does not support native thinking; continuing without a reasoning trace."})
		resp, err = doRequest(ollamaReq)
		if err != nil {
			return true, messages, fmt.Errorf("ollama unreachable: %w", err)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return true, messages, fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	// Stream and accumulate the response
	var thinkingBuf strings.Builder
	var contentBuf strings.Builder
	var toolCalls []OllamaToolCall
	thinkingChunks := 0
	budgetExceeded := false
	thinkingBudget := currentMaxThinkingTokens()

	scanner := bufio.NewScanner(resp.Body)
	// Increase buffer size for large responses
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var chunk struct {
			Message struct {
				Content   string           `json:"content"`
				Thinking  string           `json:"thinking"`
				ToolCalls []OllamaToolCall `json:"tool_calls"`
			} `json:"message"`
			Done            bool `json:"done"`
			PromptEvalCount int  `json:"prompt_eval_count"`
			EvalCount       int  `json:"eval_count"`
		}

		if err := json.Unmarshal(line, &chunk); err != nil {
			continue
		}

		// Accumulate thinking tokens
		if chunk.Message.Thinking != "" {
			thinkingBuf.WriteString(chunk.Message.Thinking)
			thinkingChunks++
			emit("thinking_token", map[string]any{"text": chunk.Message.Thinking})
		}

		// Accumulate content tokens — stream them to the client in real time
		if chunk.Message.Content != "" {
			contentBuf.WriteString(chunk.Message.Content)
			emit("token", map[string]any{"text": chunk.Message.Content})
		}

		// Collect tool calls
		if len(chunk.Message.ToolCalls) > 0 {
			toolCalls = append(toolCalls, chunk.Message.ToolCalls...)
		}

		if chunk.Done {
			emit("usage", map[string]any{
				"prompt_tokens":     chunk.PromptEvalCount,
				"completion_tokens": chunk.EvalCount,
				"total_tokens":      chunk.PromptEvalCount + chunk.EvalCount,
			})
			break
		}

		// Only trip while still purely reasoning — once real content or a
		// tool call has started arriving, the model has already committed to
		// an answer and cutting it off would just produce a truncated result
		// instead of a runaway one.
		if thinkingChunks > thinkingBudget && contentBuf.Len() == 0 && len(toolCalls) == 0 {
			budgetExceeded = true
			cancel()
			break
		}
	}
	if !budgetExceeded {
		if err := scanner.Err(); err != nil {
			return true, messages, fmt.Errorf("read ollama stream: %w", err)
		}
	}
	if budgetExceeded {
		run.ThinkingBudgetExceeded = true
		emit("thinking_token", map[string]any{"text": "\n\n[stopped: reasoning exceeded the time budget]"})
		return true, messages, nil
	}

	// Some local models print a function call as JSON instead of using Ollama's
	// tool_calls field. Recognize only exact calls to registered tools, then
	// clear that protocol JSON from the UI and process it as a real tool call.
	if !run.Direct && len(toolCalls) == 0 {
		if parsedCalls := parseTextToolCalls(contentBuf.String(), registry); len(parsedCalls) > 0 {
			toolCalls = parsedCalls
			contentBuf.Reset()
			emit("replace_content", map[string]any{"text": ""})
		}
	}

	// Tool-challenged local models sometimes narrate the instruction "Ask the
	// user ..." instead of calling ask_follow_up. Convert that protocol leak
	// into the same structured clarification UI.
	if !run.Direct && len(toolCalls) == 0 {
		if followUp, ok := parseNarratedFollowUp(contentBuf.String()); ok {
			run.Paused = true
			contentBuf.Reset()
			emit("replace_content", map[string]any{"text": ""})
			emit("follow_up", followUp)
			messages = append(messages, OllamaMessage{Role: "assistant", Content: followUp["question"].(string)})
			return true, messages, nil
		}
	}

	// Case 1: No tool calls → final response, done
	if len(toolCalls) == 0 {
		assistantMsg := OllamaMessage{
			Role:     "assistant",
			Content:  contentBuf.String(),
			Thinking: thinkingBuf.String(),
		}
		messages = append(messages, assistantMsg)
		return true, messages, nil
	}

	// Case 2: Tool calls → execute them, not done yet
	assistantMsg := OllamaMessage{
		Role:      "assistant",
		Content:   contentBuf.String(),
		Thinking:  thinkingBuf.String(),
		ToolCalls: toolCalls,
	}
	messages = append(messages, assistantMsg)

	for i, tc := range toolCalls {
		callID := tc.ID
		if callID == "" {
			callID = fmt.Sprintf("call_%d_%d", run.Steps, i)
		}

		// Parse arguments
		var args ToolInput
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = ToolInput{}
			}
		} else {
			args = ToolInput{}
		}

		// Clarification is a pause in the agent run, not an executable action.
		// The next user response resumes naturally as a new chat turn.
		if tc.Function.Name == "ask_follow_up" {
			run.Paused = true
			question, _ := args["question"].(string)
			if strings.TrimSpace(question) == "" {
				question = "Could you clarify what you would like me to do?"
			}
			options := stringSlice(args["options"])
			inputType, _ := args["input_type"].(string)
			if inputType != "text" && inputType != "select" && inputType != "multiselect" {
				if len(options) > 0 {
					inputType = "select"
				} else {
					inputType = "text"
				}
			}
			emit("replace_content", map[string]any{"text": ""})
			emit("follow_up", map[string]any{"question": question, "options": options, "input_type": inputType})

			clarification := question
			if len(options) > 0 {
				clarification += "\nOptions: " + strings.Join(options, "; ")
			}
			messages = append(messages, OllamaMessage{Role: "assistant", Content: clarification})
			return true, messages, nil
		}

		emit("tool_call", map[string]any{
			"id":    callID,
			"name":  tc.Function.Name,
			"input": args,
		})

		// Look up and execute the tool
		tool, found := registry.Get(tc.Function.Name)
		var result ToolResult
		if !found {
			result = ToolResult{OK: false, Error: fmt.Sprintf("unknown tool: %s", tc.Function.Name)}
		} else {
			var execErr error
			meta := ToolMeta{SessionID: run.SessionID, RunID: run.RunID, WorkspaceRoot: registry.WorkspaceRoot(), DB: run.DB, State: run.State}
			result, execErr = tool.Execute(ctx, args, meta)
			if execErr != nil {
				result = ToolResult{OK: false, Error: execErr.Error()}
			}
		}

		// Emitted off the result's own "staged" flag rather than the tool's
		// declared Safety tier: run_terminal is Safe (most calls execute
		// instantly) but stages destructive commands through this exact same
		// path, so it needs the same command_staged event run_command gets.
		if found && result.OK {
			if output, ok := result.Content.(map[string]any); ok {
				staged, _ := output["staged"].(bool)
				if staged && (tc.Function.Name == "run_command" || tc.Function.Name == "run_terminal") {
					emit("command_staged", map[string]any{
						"run_id": output["run_id"], "command": output["command"], "cwd": output["cwd"],
					})
				} else if staged {
					emit("patch_staged", map[string]any{
						"patch_id": output["patch_id"], "file_path": output["file_path"],
						"operation": output["operation"], "diff": output["diff"],
					})
				}
				// run_terminal's non-destructive path already wrote these
				// changes to disk before this result came back — surfaced
				// as "applied" (Keep/Undo) rather than "staged" (Approve/
				// Reject), since there's nothing left to approve.
				if changes, ok := output["file_changes"].([]map[string]any); ok {
					for _, change := range changes {
						emit("patch_applied", change)
					}
				}
			}
		}

		emit("tool_result", map[string]any{
			"id":     callID,
			"name":   tc.Function.Name,
			"output": result.Content,
			"ok":     result.OK,
			"error":  result.Error,
		})

		// Build the tool result message for the conversation
		resultBytes, _ := json.Marshal(result)
		toolMsg := OllamaMessage{
			Role:       "tool",
			Content:    string(resultBytes),
			ToolCallID: callID,
		}
		messages = append(messages, toolMsg)
	}

	return false, messages, nil
}

func normalizeThinkLevel(level string) string {
	switch strings.ToLower(level) {
	case "light", "low":
		return "low"
	case "medium":
		return "medium"
	case "high", "extra", "supreme":
		return "high"
	default:
		return "medium"
	}
}

// customThinkingModels lists locally `ollama create`d models (name:tag) whose
// FROM base isn't reflected in their own name, so the prefix check below
// can't see it — nativestudio:coder is FROM qwen3:4b, which does support
// thinking. Named exactly rather than by a blanket "nativestudio" prefix so
// other custom models (e.g. nativestudio:max, built on a non-thinking base)
// aren't misreported as thinking-capable too.
var customThinkingModels = map[string]bool{
	"nativestudio:coder": true,
}

// SupportsNativeThinking reports whether the given Ollama model name supports
// the native thinking/reasoning trace feature (the `think` request field).
// The check is name-based because Ollama does not yet expose a stable
// capabilities field across all versions — this mirrors the same rule used
// inside the agent loop and is the single source of truth for the whole app.
func SupportsNativeThinking(model string) bool {
	lower := strings.ToLower(model)
	if customThinkingModels[lower] {
		return true
	}
	base := strings.SplitN(lower, ":", 2)[0]
	return strings.HasPrefix(base, "qwen3") ||
		strings.HasPrefix(base, "deepseek-r1") ||
		strings.HasPrefix(base, "deepseek-v3.1") ||
		strings.HasPrefix(base, "gpt-oss")
}

// supportsNativeThinking is the package-private alias used inside the loop.
func supportsNativeThinking(model string) bool { return SupportsNativeThinking(model) }

func parseNarratedFollowUp(content string) (map[string]any, bool) {
	trimmed := strings.TrimSpace(content)
	lower := strings.ToLower(trimmed)
	if !strings.HasPrefix(lower, "ask the user") {
		return nil, false
	}
	question := strings.TrimSpace(trimmed[len("Ask the user"):])
	question = strings.TrimLeft(question, ":, ")
	if question == "" {
		question = "What would you like me to do next?"
	}
	options := []string{}
	inputType := "text"
	questionLower := strings.ToLower(question)
	if strings.Contains(questionLower, "whether") || strings.Contains(questionLower, "confirm") || strings.Contains(questionLower, "okay with") {
		options = []string{"Yes", "No"}
		inputType = "select"
	}
	return map[string]any{"question": question, "options": options, "input_type": inputType}, true
}

// parseTextToolCalls supports models that emit the tool protocol in the text
// channel. Ordinary JSON remains untouched unless its name matches a real tool.
func parseTextToolCalls(content string, registry *Registry) []OllamaToolCall {
	raw := strings.TrimSpace(content)
	if strings.HasPrefix(raw, "```json") && strings.HasSuffix(raw, "```") {
		raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "```json"), "```"))
	}

	type textFunction struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	type textCall struct {
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
		Function  textFunction    `json:"function"`
	}
	var envelope struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
		ToolCalls []textCall      `json:"tool_calls"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return nil
	}

	calls := envelope.ToolCalls
	if envelope.Name != "" {
		calls = append(calls, textCall{Name: envelope.Name, Arguments: envelope.Arguments})
	}

	result := make([]OllamaToolCall, 0, len(calls))
	for _, call := range calls {
		name := call.Function.Name
		arguments := call.Function.Arguments
		if name == "" {
			name = call.Name
			arguments = call.Arguments
		}
		if _, ok := registry.Get(name); !ok {
			return nil
		}
		if len(arguments) == 0 || string(arguments) == "null" {
			arguments = json.RawMessage(`{}`)
		}
		// Arguments may themselves be a JSON-encoded string.
		var encoded string
		if json.Unmarshal(arguments, &encoded) == nil {
			arguments = json.RawMessage(encoded)
		}
		var tc OllamaToolCall
		tc.ID = call.ID
		tc.Type = "function"
		tc.Function.Name = name
		tc.Function.Arguments = ToolArguments(arguments)
		result = append(result, tc)
	}
	return result
}

func stringSlice(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			result = append(result, text)
		}
	}
	return result
}
