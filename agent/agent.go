package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/entreya/nativestudio/db"
	"github.com/entreya/nativestudio/editor"
	"github.com/entreya/nativestudio/knowledge"
	"github.com/entreya/nativestudio/resolver"

	ctxpkg "github.com/entreya/nativestudio/context"
)

const assistantSystemPrompt = `You are the coding assistant inside NativeStudio.
Answer the user in clear natural language, not as JSON. Use the provided tools through native tool calls; never print a tool-call JSON object in the answer.
Use the active file and editor context when relevant. When the user's request is short or does not name a target (e.g. "explain", "fix this", "refactor", "what does this do"), assume they mean the active file open in the editor and answer about that file — do not ask them to paste code you already have access to; use read_file if you need more of it than is already shown. CRITICAL: Never write code blocks in the chat response. To edit a file, use replace_in_file or apply_patch. To create a file, use create_file. To remove a file, use delete_file. File mutations are staged for the user to approve or reject. Your text response should only explain briefly what you changed and why. If the request is materially ambiguous and choosing incorrectly could change the result, call ask_follow_up directly with one concise question, the appropriate input type, and useful short options. Use multiselect when several answers can apply. Never narrate an instruction such as "Ask the user". If a request depends on unfamiliar or current facts, call search_internet before answering instead of guessing.`

// Agent orchestrates a multi-step agent run with tool calling.
type Agent struct {
	OllamaURL string
	Registry  *Registry
	DB        *db.DB
	EditorSvc *editor.EditorContextService
	Resolver  *resolver.ReferenceResolver
}

// NewAgent creates an Agent instance with all dependencies.
func NewAgent(
	ollamaURL string,
	registry *Registry,
	database *db.DB,
	editorSvc *editor.EditorContextService,
	res *resolver.ReferenceResolver,
) *Agent {
	return &Agent{
		OllamaURL: ollamaURL,
		Registry:  registry,
		DB:        database,
		EditorSvc: editorSvc,
		Resolver:  res,
	}
}

// generateRunID returns a short unique hex string for tracing an agent run.
func generateRunID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// Run executes a full agent loop for a user prompt.
func (a *Agent) Run(
	ctx context.Context,
	sessionID string,
	prompt string,
	model string,
	think bool,
	thinkLevel string,
	state editor.EditorState,
	emit func(string, any),
) (string, error) {

	runID := generateRunID()
	directConversation := isLightweightConversation(prompt)
	sessionRecord, _ := a.DB.GetSession(sessionID)
	if !directConversation {
		emit("agent_start", map[string]any{
			"run_id":    runID,
			"max_steps": DefaultMaxSteps,
		})
	}
	if think && !supportsNativeThinking(model) {
		think = false
		emit("thinking_unavailable", map[string]any{
			"message": "Thinking mode is not supported by this model; continuing in normal mode.",
		})
	}

	// 1. Save editor state to DB
	if a.EditorSvc != nil && state.ActiveFile != "" {
		if err := a.EditorSvc.Save(ctx, sessionID, runID, state); err != nil {
			log.Printf("[agent] failed to save editor state: %v", err)
		}
	}

	// 2. Resolve candidates and build context note
	var contextBuilder strings.Builder
	var webResults []internetSearchResult
	var webQuery string
	var webGroundingMessage string
	var knowledgeCandidates []knowledge.Candidate

	// Current-event questions must not depend on whether a small local model
	// remembers to call a tool. Research them before the first model step and
	// expose both the query and results through the normal tool timeline.
	if needsInternetSearch(prompt) {
		query := currentSearchQuery(prompt, time.Now())
		webQuery = query
		callID := "internet_preflight_" + runID
		emit("tool_call", map[string]any{
			"id": callID, "name": "search_internet", "input": map[string]any{"query": query, "max_results": 5},
		})
		results, err := searchCurrentEvent(ctx, query, 5)
		if err != nil {
			emit("tool_result", map[string]any{"id": callID, "name": "search_internet", "ok": false, "error": err.Error()})
			contextBuilder.WriteString("Internet search was attempted but failed. State that current information could not be verified.\n")
		} else if len(results) == 0 {
			emit("tool_result", map[string]any{
				"id": callID, "name": "search_internet", "ok": false,
				"error": "No relevant current-event results were found",
			})
			contextBuilder.WriteString("The news search completed but found no relevant results. Say that the event could not be verified; do not substitute unrelated facts.\n")
		} else {
			webResults = results
			emit("tool_result", map[string]any{
				"id": callID, "name": "search_internet", "ok": true,
				"output": map[string]any{"query": query, "results": results},
			})
			var grounding strings.Builder
			grounding.WriteString("WEB RESEARCH SUCCEEDED for the user's immediately preceding question. You MUST answer from these current results and include source URLs. Do not claim you lack real-time access, do not mention a training cutoff, and do not ask whether the user wants you to search.\n")
			for _, result := range results {
				grounding.WriteString(fmt.Sprintf("- %s — %s — %s\n", result.Title, result.Description, result.URL))
			}
			webGroundingMessage = grounding.String()
			contextBuilder.WriteString(webGroundingMessage)
		}
	}
	if a.Resolver != nil && !directConversation {
		history := ctxpkg.Store.GetMessages(sessionID)
		dbHistory := make([]db.Message, len(history))
		for i, m := range history {
			dbHistory[i] = db.Message{Role: m.Role, Content: m.Content}
		}

		candidates, err := a.Resolver.Resolve(ctx, prompt, state, dbHistory)
		if err != nil {
			log.Printf("[agent] resolver error: %v", err)
		}

		if len(candidates) > 0 {
			best := candidates[0]
			emit("context_resolved", map[string]any{
				"file":       best.Path,
				"symbol":     best.Symbol,
				"confidence": best.Confidence,
				"reasons":    best.Reasons,
			})

			sym := ""
			if best.Symbol != nil {
				sym = " › " + best.Symbol.Name
			}
			contextBuilder.WriteString(fmt.Sprintf("Resolved: %s%s (confidence: %s)\n", best.Path, sym, best.Confidence))
		}
		knowledgeCandidates, err = a.Resolver.RetrieveKnowledge(ctx, prompt, state, candidates)
		if err != nil {
			log.Printf("[agent] knowledge retrieval error: %v", err)
		}
		for _, candidate := range knowledgeCandidates {
			emit("context_candidate", candidate)
		}
	}

	// 3. Append user message to DB
	userMsg := ctxpkg.Message{
		Role:      "user",
		Content:   prompt,
		Timestamp: time.Now(),
	}
	savedUser := ctxpkg.Store.AppendMessage(sessionID, userMsg)
	if savedUser != nil {
		emit("message_saved", map[string]any{"role": "user", "id": savedUser.ID})
	}
	// Replace the placeholder immediately so a failed or stopped model run does
	// not leave a permanent "New Conversation" entry. The metadata model refines
	// this provisional title and summary after the assistant response completes.
	if session, err := a.DB.GetSession(sessionID); err == nil && isPlaceholderTitle(session.Title) {
		title, summary := fallbackConversationMetadata(prompt, "")
		if err := a.DB.UpdateSessionMetadata(sessionID, title, summary); err == nil {
			emit("session_metadata", map[string]any{"session_id": sessionID, "title": title, "summary": summary})
		}
	}

	// 4. Load full message history from DB and convert to OllamaMessage format
	dbMessages := ctxpkg.Store.GetMessages(sessionID)
	contextState := state
	if directConversation {
		contextState = editor.EditorState{}
	}
	modelContext := ctxpkg.BuildModelContext(ctxpkg.BuildInput{Model: model, State: contextState, History: dbMessages, Candidates: knowledgeCandidates})
	if modelContext.EditorAndKnowledge != "" {
		contextBuilder.WriteString(modelContext.EditorAndKnowledge)
	}
	if !directConversation {
		emit("context_built", map[string]any{"tokens": modelContext.Tokens, "budget": modelContext.Budget, "items": modelContext.Items})
	}
	var messages []OllamaMessage

	// Always define the response/tool contract. Editor context stays in a
	// system message so the user's stored prompt remains clean.
	systemContent := assistantSystemPrompt
	if directConversation {
		systemContent = "You are the NativeStudio assistant. Respond naturally and briefly to casual conversation. Do not mention project files, tools, context, or agent planning unless the user asks about them."
		think = false
	}
	if think {
		systemContent += "\nReasoning mode is enabled at " + reasoningGuidance(thinkLevel) + " effort."
	}
	if contextBuilder.Len() > 0 {
		systemContent += "\n\nCurrent editor context:\n" + contextBuilder.String()
	}
	messages = append(messages, OllamaMessage{Role: "system", Content: systemContent})

	// Small local models answer a bare "explain" or "fix this" literally
	// instead of inferring "the file I have open" the way a larger model
	// would. Anchor the current turn to the active file by name so the model
	// has an explicit target — this only changes what's sent to Ollama, never
	// what's stored in the session or shown in the chat UI.
	anchoredPrompt := anchorPromptToActiveFile(prompt, contextState)
	for _, m := range modelContext.History {
		content := m.Content
		if m.Role == "user" && content == prompt {
			content = anchoredPrompt
		}
		messages = append(messages, OllamaMessage{
			Role:    m.Role,
			Content: content,
		})
	}
	// Some small local-model templates pay disproportionate attention to the
	// final message. Repeat successful research after the current user message
	// so it cannot be displaced by older conversation history.
	if webGroundingMessage != "" {
		messages = append(messages, OllamaMessage{
			Role:    "user",
			Content: "Use these completed web-search results to answer my preceding question now.\n" + webGroundingMessage,
		})
	}

	// 5. Create the agent run — carry editor state for get_editor_context tool
	run := &AgentRun{
		RunID:      runID,
		SessionID:  sessionID,
		Model:      model,
		Steps:      0,
		MaxSteps:   DefaultMaxSteps,
		Think:      think,
		ThinkLevel: thinkLevel,
		Direct:     directConversation,
		State:      state,
		DB:         a.DB,
	}

	// Inject the editor state into the registry so get_editor_context returns it
	a.Registry.InjectEditorState(state)

	// 6. Execute the loop
	var finalContent string
	runEmit := func(event string, data any) { emit(event, data) }
	for {
		done, newMessages, err := run.Step(ctx, messages, a.Registry, a.OllamaURL, runEmit)
		if err != nil {
			log.Printf("[agent] run %s step %d error: %v", runID, run.Steps, err)
			emit("error", map[string]any{"message": err.Error()})
			break
		}
		messages = newMessages

		if done {
			for i := len(messages) - 1; i >= 0; i-- {
				if messages[i].Role == "assistant" {
					finalContent = messages[i].Content
					break
				}
			}
			break
		}
	}

	if len(webResults) > 0 && !responseGroundedInSuccessfulSearch(finalContent, webResults) {
		finalContent = groundedSearchFallback(webQuery, webResults)
		emit("replace_content", map[string]any{"text": finalContent})
	}

	// 7. Save the final assistant response to DB.
	if finalContent != "" {
		assistantMsg := ctxpkg.Message{
			Role:      "assistant",
			Content:   finalContent,
			Timestamp: time.Now(),
		}
		savedAssistant := ctxpkg.Store.AppendMessage(sessionID, assistantMsg)
		if savedAssistant != nil {
			emit("message_saved", map[string]any{"role": "assistant", "id": savedAssistant.ID})
		}

		if session, err := a.DB.GetSession(sessionID); err == nil {
			if directConversation {
				title, summary := fallbackConversationMetadata(prompt, finalContent)
				if err := a.DB.UpdateSessionMetadata(sessionID, title, summary); err == nil {
					emit("session_metadata", map[string]any{"session_id": sessionID, "title": title, "summary": summary})
				}
			} else {
				title, summary, metadataErr := a.generateConversationMetadata(ctx, model, session.Title, session.Summary, prompt, finalContent)
				if metadataErr != nil {
					log.Printf("[agent] failed to generate conversation metadata: %v", metadataErr)
					title, summary = fallbackConversationMetadata(prompt, finalContent)
				}
				if err := a.DB.UpdateSessionMetadata(sessionID, title, summary); err != nil {
					log.Printf("[agent] failed to save conversation metadata: %v", err)
				} else {
					emit("session_metadata", map[string]any{"session_id": sessionID, "title": title, "summary": summary})
				}
			}
		}
	}
	if sessionRecord != nil && !directConversation {
		paths := make([]string, 0, len(knowledgeCandidates))
		seen := map[string]bool{}
		for _, candidate := range knowledgeCandidates {
			if candidate.Path != "" && !seen[candidate.Path] {
				seen[candidate.Path] = true
				paths = append(paths, candidate.Path)
			}
		}
		learning, _ := json.Marshal(map[string]any{"user_request": prompt, "files_inspected": paths, "knowledge_candidates": len(knowledgeCandidates), "assistant_result": truncateRunes(finalContent, 2000)})
		_ = a.DB.RecordSessionLearning(ctx, sessionRecord.ProjectID, sessionID, runID, string(learning), "")
	}

	// 8. Emit agent done after persistence so message actions have stable IDs.
	emit("agent_done", map[string]any{
		"run_id": runID,
		"steps":  run.Steps,
	})

	return finalContent, nil
}

var lightweightConversationPattern = regexp.MustCompile(`(?i)^\s*(hi+|hello+|hey+|good\s+(morning|afternoon|evening)|how\s+are\s+you|thanks?|thank\s+you|ok(?:ay)?|bye|goodbye)[!?.\s]*$`)

func isLightweightConversation(prompt string) bool {
	return lightweightConversationPattern.MatchString(prompt)
}

// ambiguousPromptPattern matches short imperative requests that omit their
// object ("explain", "fix this", "refactor", "what does this do", "review").
// Larger models infer these mean "the file I have open"; smaller local
// models tend to answer the verb literally instead.
var ambiguousPromptPattern = regexp.MustCompile(`(?i)^\s*(explain|fix|refactor|review|optimi[sz]e|simplify|document|comment|clean\s*up|improve|rewrite|format|lint|debug|summarize|test)\b`)

// anchorPromptToActiveFile rewrites a short, target-less prompt to explicitly
// name the active file, so the model has something concrete to act on
// instead of guessing from a bare verb. Only used for the copy of the prompt
// sent to the model — the stored/displayed message is untouched.
func anchorPromptToActiveFile(prompt string, state editor.EditorState) string {
	trimmed := strings.TrimSpace(prompt)
	if state.ActiveFile == "" || trimmed == "" {
		return prompt
	}
	if len(strings.Fields(trimmed)) > 6 {
		return prompt
	}
	if !ambiguousPromptPattern.MatchString(trimmed) {
		return prompt
	}
	base := state.ActiveFile
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	if base != "" && strings.Contains(strings.ToLower(trimmed), strings.ToLower(base)) {
		return prompt // already names the file
	}
	return fmt.Sprintf("%s `%s` (the file I currently have open).", trimmed, state.ActiveFile)
}

func (a *Agent) generateConversationMetadata(ctx context.Context, model, currentTitle, currentSummary, userPrompt, assistantResponse string) (string, string, error) {
	format := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":   map[string]any{"type": "string"},
			"summary": map[string]any{"type": "string"},
		},
		"required":             []string{"title", "summary"},
		"additionalProperties": false,
	}
	prompt := fmt.Sprintf(`Create metadata for a coding-assistant conversation.
Choose the best specific title of 3-8 words yourself. Update the summary in 1-3 concise sentences, retaining files, requested changes, decisions, and unresolved questions.
Treat the existing title as a suggestion and replace generic or automatically generated wording.

Existing title: %s
Existing summary: %s
Latest user message: %s
Latest assistant response: %s`, currentTitle, currentSummary, truncateRunes(userPrompt, 4000), truncateRunes(assistantResponse, 8000))
	payload := map[string]any{
		"model":  model,
		"stream": false,
		"format": format,
		"messages": []map[string]string{
			{"role": "system", "content": "Return only the requested JSON conversation metadata."},
			{"role": "user", "content": prompt},
		},
		"options": map[string]any{"temperature": 0.2},
	}
	send := func() (*http.Response, error) {
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.OllamaURL+"/api/chat", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return http.DefaultClient.Do(req)
	}
	resp, err := send()
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode == http.StatusBadRequest {
		resp.Body.Close()
		delete(payload, "format")
		resp, err = send()
		if err != nil {
			return "", "", err
		}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("ollama metadata request returned status %d", resp.StatusCode)
	}
	var ollamaResponse struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResponse); err != nil {
		return "", "", err
	}
	var metadata struct {
		Title   string `json:"title"`
		Summary string `json:"summary"`
	}
	raw := extractJSONObject(ollamaResponse.Message.Content)
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return "", "", fmt.Errorf("decode metadata: %w", err)
	}
	metadata.Title = strings.TrimSpace(metadata.Title)
	metadata.Summary = strings.TrimSpace(metadata.Summary)
	if metadata.Title == "" || metadata.Summary == "" {
		return "", "", fmt.Errorf("model returned empty conversation metadata")
	}
	titleRunes := []rune(metadata.Title)
	if len(titleRunes) > 80 {
		metadata.Title = string(titleRunes[:80])
	}
	summaryRunes := []rune(metadata.Summary)
	if len(summaryRunes) > 2000 {
		metadata.Summary = string(summaryRunes[:2000])
	}
	return metadata.Title, metadata.Summary, nil
}

func extractJSONObject(text string) string {
	raw := strings.TrimSpace(text)
	raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw, "```json"), "```"))
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end >= start {
		return raw[start : end+1]
	}
	return raw
}

func fallbackConversationMetadata(userPrompt, assistantResponse string) (string, string) {
	clean := strings.Join(strings.Fields(userPrompt), " ")
	titleWords := strings.Fields(strings.Trim(clean, " .!?,:;-"))
	if len(titleWords) > 7 {
		titleWords = titleWords[:7]
	}
	title := strings.Join(titleWords, " ")
	if title == "" {
		title = "Conversation"
	}
	titleRunes := []rune(title)
	titleRunes[0] = []rune(strings.ToUpper(string(titleRunes[0])))[0]
	title = string(titleRunes)

	response := strings.Join(strings.Fields(assistantResponse), " ")
	if len([]rune(response)) > 320 {
		response = truncateRunes(response, 320) + "…"
	}
	summary := "The user asked: " + clean
	if response != "" {
		summary += ". The assistant responded: " + response
	}
	return title, summary
}

func isPlaceholderTitle(title string) bool {
	normalized := strings.ToLower(strings.TrimSpace(title))
	return normalized == "" || normalized == "new conversation" || normalized == "untitled conversation"
}

func needsInternetSearch(prompt string) bool {
	lower := strings.ToLower(prompt)
	markers := []string{
		"recent", "latest", "today", "yesterday", "currently", "current ", "breaking",
		"news", "protest", "election", "price", "weather", "this week", "this month", "this year",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func currentSearchQuery(prompt string, now time.Time) string {
	normalized := strings.ToLower(strings.Join(strings.Fields(prompt), " "))
	normalized = strings.NewReplacer(
		"do you know about", "", "what do you know about", "", "can you tell me about", "", "tell me about", "",
		"could you find", "", "can you find", "", "would you find", "", "i want to know about", "",
		"recent", "", "latest", "", "please", "",
	).Replace(normalized)
	ordinal := regexp.MustCompile(`\b(\d{1,2})(st|nd|rd|th)\b`)
	normalized = ordinal.ReplaceAllString(normalized, "$1")
	fields := strings.Fields(normalized)
	noise := map[string]bool{
		"at": true, "on": true, "the": true, "a": true, "an": true, "of": true,
		"for": true, "to": true, "in": true, "is": true, "was": true, "were": true,
	}
	terms := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, " ,.!?:;()[]{}")
		if field != "" && !noise[field] {
			terms = append(terms, field)
		}
	}
	query := strings.Join(terms, " ")
	for year := 2000; year <= now.Year()+1; year++ {
		if strings.Contains(query, fmt.Sprintf("%d", year)) {
			return query
		}
	}
	return fmt.Sprintf("%s %d", query, now.Year())
}

func responseIgnoredSuccessfulSearch(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	if lower == "" {
		return true
	}
	markers := []string{
		"training data", "training cutoff", "knowledge cutoff", "knowledge is up to",
		"don't have specific", "do not have specific", "can't access real-time", "cannot access real-time",
		"recommend checking reliable", "would you like help finding", "i can't verify", "i cannot verify",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func responseGroundedInSuccessfulSearch(content string, results []internetSearchResult) bool {
	if responseIgnoredSuccessfulSearch(content) {
		return false
	}
	for _, result := range results {
		if result.URL != "" && strings.Contains(content, result.URL) {
			return true
		}
	}
	return false
}

func groundedSearchFallback(query string, results []internetSearchResult) string {
	var answer strings.Builder
	answer.WriteString("Yes. I found current reporting that matches your question")
	if query != "" {
		answer.WriteString(" (searched: “" + query + "”)")
	}
	answer.WriteString(". Here are the most relevant reports:\n\n")
	limit := len(results)
	if limit > 5 {
		limit = 5
	}
	for index, result := range results[:limit] {
		answer.WriteString(fmt.Sprintf("%d. %s\n   %s\n", index+1, result.Title, result.URL))
	}
	answer.WriteString("\nThese are live search results, so the answer is grounded in the retrieved coverage rather than the model's training cutoff.")
	return answer.String()
}

func truncateRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

func reasoningGuidance(level string) string {
	switch strings.ToLower(level) {
	case "light":
		return "light (quick validation only)"
	case "high":
		return "high (careful multi-step analysis)"
	case "extra":
		return "extra (deep analysis with edge-case review)"
	case "supreme":
		return "supreme (maximum-depth analysis, verification, and self-review)"
	default:
		return "medium (balanced analysis)"
	}
}
