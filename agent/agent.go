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
	"sync"
	"time"

	"github.com/entreya/nativestudio/db"
	"github.com/entreya/nativestudio/editor"
	"github.com/entreya/nativestudio/knowledge"
	"github.com/entreya/nativestudio/resolver"

	ctxpkg "github.com/entreya/nativestudio/context"
)

const assistantSystemPrompt = `You are the coding assistant inside NativeStudio.
Answer the user in clear natural language, not as JSON. Use the provided tools through native tool calls; never print a tool-call JSON object in the answer.
When you need to understand a specific piece of code rather than just its surface — before editing it, or before answering a question about its behavior — first locate it (run_terminal/find_files/search_text), then read that exact snippet, then check whether it depends on anything (an import, a called function, a referenced type/config): if so, go read that dependency's relevant snippet too. Keep following the dependency chain, reading one more piece at a time, until you actually understand how the pieces fit together — do not answer or edit based on the first snippet alone if it clearly relies on something you haven't looked at yet. Use the active file and editor context when relevant. When the user's request is short or does not name a target (e.g. "explain", "fix this", "refactor", "what does this do"), assume they mean the active file open in the editor and answer about that file — do not ask them to paste code you already have access to; use read_file if you need more of it than is already shown. When a "selected_text" block is present, that is the user's current selection — do not treat it as the whole picture: read the surrounding code in the same active file (the function/class it sits in, its callers, nearby definitions) before answering, since the selection alone is rarely enough to judge correctness, side effects, or naming. CRITICAL: Never write code blocks in the chat response. To edit a file, use replace_in_file or apply_patch. To create a file, use create_file. To remove a file, use delete_file. File mutations are staged for the user to approve or reject. For installing packages, scaffolding a new project/framework (e.g. a Composer or npm package, "yii2-app-basic", a boilerplate), or running build/test commands, use run_command instead of hand-writing the files those tools would generate yourself — you do not reliably know the exact files a framework installer produces, and guessing produces broken projects. run_command is also staged for approval before it runs. To rename a class, interface, trait, function or constant, always use rename_symbol — never replace_in_file. rename_symbol takes the symbol name rather than an exact string, and it updates the declaration, every reference across the workspace, and the file itself when the file is named after the symbol. Renaming a class without renaming its file breaks PSR-4 autoloading, and updating one file while leaving references elsewhere pointing at the old name breaks the build, so do not attempt a rename by hand.
For a quick, basic, reversible terminal task instead — checking a file's size, making a curl request, running a short script you just wrote, renaming or moving a file, generating some sample data — call run_terminal first: it executes immediately with no approval step. If the exact command looks destructive or irreversible, run_terminal stages it for the user to confirm in a popup instead of running it instantly or refusing outright, the same as run_command. Fall back to the more specific tools (read_file, create_file, etc.) when they fit the task better than a raw shell command. If none of run_terminal, run_command, or any other tool can actually do what's being asked, say so plainly in your response instead of pretending you already did it. Your response to the user must be the direct final answer only — never a transcript of your own deliberation. Do not write things like "let me figure out what they mean", "wait, but maybe they mean X", "so I should", or any other narration of your reasoning process; the user should only ever see your conclusion, not how you got there. If a request is genuinely ambiguous, either resolve it yourself by making the single most reasonable assumption and saying so in one short line, or call ask_follow_up — do not think out loud in the response as a substitute for deciding. Your text response should only explain briefly what you changed and why. If the request is materially ambiguous and choosing incorrectly could change the result, call ask_follow_up directly with one concise question, the appropriate input type, and useful short options. Use multiselect when several answers can apply. Never narrate an instruction such as "Ask the user". Not everything unfamiliar-sounding needs a search: for established, universal facts that cannot change over time (well-known history, math and science constants, language/syntax rules, standard definitions), just answer directly from what you already know — searching or asking a follow-up for something that is always true just wastes time. For the actual current date, time, day of the week, or timezone, don't search the internet and don't answer from your own knowledge either — call run_terminal with the date command and read it straight from the system clock; a web search won't reliably state today's date, and your own knowledge has a training cutoff, not live awareness of the current moment. Reserve search_internet and ask_follow_up for things that are genuinely unfamiliar, ambiguous, or that change over time — current events, prices, the latest version of something, who currently holds some position. If a request depends on unfamiliar or current facts like that, call search_internet before answering instead of guessing. search_internet queries several engines at once and returns a "confidence" field telling you how many INDEPENDENT sources agreed — read it and act on it. "high" means you can answer from the results and cite them. "medium" means answer but say the corroboration is limited. "low" means the evidence is too thin to state as fact: summarize what the results actually said, rephrase what you're looking for in plain simple words, and search again — that is how you correct a query that returned the wrong thing, rather than reasoning about it further without new information. Only after a few rounds of summarize-rephrase-search-confirm, if the confidence is still low, call ask_follow_up so the user can confirm it or point you at a better source, instead of continuing to guess.
When context includes a "PREVIOUSLY VERIFIED" note, that subject has already been checked and confirmed — answer from it directly and do not search again unless the question is about something that would have changed since. When it includes "PREVIOUSLY APPROVED WORK", follow the conventions of those accepted changes rather than working them out from scratch. More generally: if you notice yourself reconsidering the same point again without anything new to go on, that is the signal to take an action — call a tool, or call ask_follow_up — not to keep thinking it over. If the user's message is casual conversation — a greeting, a joke, song lyrics, small talk, or anything else unrelated to the code or project — that is not an error, not out of scope, and not something you lack the ability to understand: reply naturally and warmly like a friendly conversational partner, in whatever language or tone they used, then briefly invite them back to the project. For example, if the user sends song lyrics or an unrelated one-liner, a good reply looks like "Haha, love that energy! Whenever you're ready to get back to the project, I'm here." — NOT a request for clarification and NOT an apology. Never refuse, say you're not sure what they mean, or apologize for a harmless message just because it isn't a coding request.`

// Agent orchestrates a multi-step agent run with tool calling.
type Agent struct {
	OllamaURL string
	Registry  *Registry
	DB        *db.DB
	EditorSvc *editor.EditorContextService
	Resolver  *resolver.ReferenceResolver
	// MaxToolSteps caps how many tool-call iterations a single run may take.
	// Configurable (config.json's max_agent_tool_steps) rather than a fixed
	// constant, per-run limits being a required safety control for any tool
	// that can be called repeatedly (e.g. a narrowing sequence of find_files
	// calls).
	MaxToolSteps int
}

// NewAgent creates an Agent instance with all dependencies. maxToolSteps <= 0
// falls back to DefaultMaxSteps.
func NewAgent(
	ollamaURL string,
	registry *Registry,
	database *db.DB,
	editorSvc *editor.EditorContextService,
	res *resolver.ReferenceResolver,
	maxToolSteps int,
) *Agent {
	if maxToolSteps <= 0 {
		maxToolSteps = DefaultMaxSteps
	}
	return &Agent{
		OllamaURL:    ollamaURL,
		Registry:     registry,
		DB:           database,
		EditorSvc:    editorSvc,
		Resolver:     res,
		MaxToolSteps: maxToolSteps,
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

	// Tee every timeline-relevant event into a log that gets persisted with the
	// assistant message, so reloading a conversation restores the same trace
	// the user watched live instead of a bare answer with no reasoning shown.
	// Wrapping here (rather than the step loop's emit) also captures the
	// context-gathering events, which fire before the loop starts.
	var timelineLog []map[string]any
	var timelineMu sync.Mutex
	baseEmit := emit
	emit = func(event string, data any) {
		if persistedTimelineEvents[event] {
			timelineMu.Lock()
			// Thinking arrives one token at a time; appending each as its own
			// entry would store thousands of rows' worth of JSON per message.
			// Fold consecutive tokens into the preceding entry instead.
			if merged := mergeThinkingToken(timelineLog, event, data); merged {
				timelineMu.Unlock()
				baseEmit(event, data)
				return
			}
			timelineLog = append(timelineLog, map[string]any{"type": event, "data": data})
			timelineMu.Unlock()
		}
		baseEmit(event, data)
	}

	runID := generateRunID()
	directConversation := isLightweightConversation(prompt)
	contextState := state
	if directConversation {
		contextState = editor.EditorState{}
	}

	// modelPrompt is what actually gets sent to the real coding model and
	// used to derive search queries/context lookups — prompt itself stays
	// untouched for DB storage, chat history, and conversation titles, so
	// the user always sees exactly what they typed. Anchoring to the active
	// file happens BEFORE rephrasing, not after: a bare "explain this
	// function" gives the tiny rephraser model nothing concrete to work
	// with, and it was observed live to fabricate plausible-sounding but
	// wrong specifics (a guessed language, an invented folder) to fill the
	// gap — anchoring first hands it the real filename instead of a guess.
	// Casual conversation skips both steps: there's nothing to clarify in a
	// greeting, and running it through a second model is wasted latency.
	// Explain/describe/review-style requests skip rephrasing entirely too —
	// live-tested, rephraserModel doesn't rewrite that class of request, it
	// ANSWERS it, confidently inventing a description of code it has never
	// seen. Rewriting what to do is safe; rewriting a request to understand
	// existing code risks handing the real model a fabricated "fact" about
	// the codebase disguised as the user's own instruction.
	modelPrompt := prompt
	if !directConversation {
		anchoredOriginal := anchorPromptToActiveFile(prompt, contextState)
		modelPrompt = anchoredOriginal
		if !promptRequestsExplanation(prompt) {
			if rephrased, err := rephrasePrompt(ctx, a.OllamaURL, anchoredOriginal); err == nil && rephrased != anchoredOriginal {
				modelPrompt = rephrased
				// Surfaced in the chat timeline (persisted like any other
				// step) so the user can see what actually got sent to the
				// real model — rephrasing is otherwise invisible, and a
				// wrong or unexpected rewrite should be easy to spot.
				emit("prompt_rephrased", map[string]any{"original": prompt, "rephrased": rephrased})
			}
		}
	}

	sessionRecord, _ := a.DB.GetSession(sessionID)
	// The registry (file tools) and resolver (knowledge search) both hold a
	// single shared "active workspace" — set whenever a project is opened in
	// the UI, not per-request. A request for a session whose project isn't
	// the one currently open (a background/stale browser tab, or another
	// project opened since this session's conversation started) would
	// otherwise silently run tools against, and answer from, the wrong
	// project. Re-sync from the session's actual project before using either.
	if sessionRecord != nil && sessionRecord.ProjectID != "" {
		if project, err := a.DB.GetProject(sessionRecord.ProjectID); err == nil {
			if a.Registry != nil {
				a.Registry.SetWorkspaceRoot(project.Path)
			}
			if a.Resolver != nil {
				a.Resolver.SetWorkspaceRoot(project.Path)
				a.Resolver.SetKnowledgeWorkspaceID(project.ID)
			}
		}
	}
	if !directConversation {
		emit("agent_start", map[string]any{
			"run_id":    runID,
			"max_steps": a.MaxToolSteps,
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
	// webConfidence carries the cross-source rating ("high"/"medium"/"low")
	// from the preflight search through to the escalation check after the
	// model has answered. webSources are the independent domains that backed
	// it, recorded alongside a fact so the user can audit what corroborated it.
	var webConfidence string
	var webSources []string
	var knowledgeCandidates []knowledge.Candidate

	// Current-event questions must not depend on whether a small local model
	// remembers to call a tool. Research them before the first model step and
	// expose both the query and results through the normal tool timeline.
	// A subject that was already verified once does not need researching
	// again — this is the whole point of keeping a verified store separate
	// from ordinary run history. Checked before the search so a settled
	// question costs nothing.
	// The user answering an escalation follow-up with "that's correct" is the
	// human-verification branch of the ladder: promote the claim they just
	// confirmed so it is never re-researched.
	if sessionRecord != nil && userConfirmedVerification(prompt) {
		stored := ctxpkg.Store.GetMessages(sessionID)
		history := make([]ctxMessage, len(stored))
		for i, message := range stored {
			history[i] = ctxMessage{Role: message.Role, Content: message.Content}
		}
		a.promoteLastAnswerToVerifiedFact(ctx, sessionRecord.ProjectID, history)
		emit("fact_verified", map[string]any{"verified_by": "user"})
	}

	recalledFact, hasRecalledFact := "", false
	if sessionRecord != nil && !directConversation {
		recalledFact, hasRecalledFact = a.recallVerifiedFact(ctx, sessionRecord.ProjectID, modelPrompt)
		if hasRecalledFact {
			contextBuilder.WriteString(recalledFact)
			emit("fact_recalled", map[string]any{"subject": factSubjectKey(modelPrompt)})
		}
		if approved := a.recallApprovedChanges(ctx, sessionRecord.ProjectID, modelPrompt); approved != "" {
			contextBuilder.WriteString(approved)
			emit("memory_recalled", map[string]any{"kind": "approved_changes"})
		}
	}

	if needsInternetSearch(modelPrompt) && !hasRecalledFact {
		query := currentSearchQuery(modelPrompt, time.Now())
		webQuery = query
		callID := "internet_preflight_" + runID
		emit("tool_call", map[string]any{
			"id": callID, "name": "search_internet", "input": map[string]any{"query": query, "max_results": 5},
		})
		check := searchAllEngines(ctx, query, 5)
		webConfidence = check.Confidence
		webSources = check.IndependentDomains
		if len(check.Results) == 0 {
			emit("tool_result", map[string]any{
				"id": callID, "name": "search_internet", "ok": false,
				"error":  "No relevant results were found across any search engine",
				"output": map[string]any{"engines_queried": check.EnginesQueried, "engines_failed": check.EnginesFailed},
			})
			contextBuilder.WriteString("The search completed but found no relevant results. Say that this could not be verified; do not substitute unrelated facts.\n")
		} else {
			for _, result := range check.Results {
				webResults = append(webResults, result.internetSearchResult)
			}
			emit("tool_result", map[string]any{
				"id": callID, "name": "search_internet", "ok": true,
				"output": map[string]any{
					"query": query, "results": check.Results,
					"confidence": check.Confidence, "confidence_reason": check.Reason,
					"independent_sources": check.IndependentDomains,
					"engines_queried":     check.EnginesQueried,
				},
			})

			var grounding strings.Builder
			grounding.WriteString(fmt.Sprintf(
				"WEB RESEARCH was run for the user's immediately preceding question across %d search engines. Cross-source confidence: %s — %s\n",
				len(check.EnginesQueried), check.Confidence, check.Reason))
			switch check.Confidence {
			case "high":
				grounding.WriteString("Several independent sources agree, so you can answer from these results — cite the source URLs. Do not claim you lack real-time access or mention a training cutoff.\n")
			case "medium":
				grounding.WriteString("Only two independent sources back this, so answer if they clearly cover the question but say the corroboration is limited. If they don't cover it, call search_internet again with a more specific query.\n")
			default:
				grounding.WriteString("The evidence is too thin to state as fact. Do NOT present this as verified. Either call search_internet again with a more specific query, or call ask_follow_up to have the user confirm or point you at a better source.\n")
			}
			for _, result := range check.Results {
				grounding.WriteString(fmt.Sprintf("- [%s] %s — %s — %s\n", result.Domain, result.Title, result.Description, result.URL))
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

		candidates, err := a.Resolver.Resolve(ctx, modelPrompt, state, dbHistory)
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
		knowledgeCandidates, err = a.Resolver.RetrieveKnowledge(ctx, modelPrompt, state, candidates)
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

	// modelPrompt is already anchored to the active file (and, for a real
	// agent turn, rephrased) above — this only changes what's sent to
	// Ollama, never what's stored in the session or shown in the chat UI.
	anchoredPrompt := modelPrompt
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
	// final message. Repeat the research after the current user message so it
	// cannot be displaced by older conversation history — but as a decision to
	// make, not an unconditional order, matching webGroundingMessage's own
	// instruction: use it if it actually answers the question, otherwise
	// search again or ask a follow-up instead of forcing an answer out of it.
	if webGroundingMessage != "" {
		messages = append(messages, OllamaMessage{
			Role:    "user",
			Content: "Here is the completed web search for my preceding question. Check whether it actually answers what I asked before responding.\n" + webGroundingMessage,
		})
	}

	// 5. Create the agent run — carry editor state for get_editor_context tool
	run := &AgentRun{
		RunID:      runID,
		SessionID:  sessionID,
		Model:      model,
		Steps:      0,
		MaxSteps:   a.MaxToolSteps,
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
	// stepErrored marks a run that ended via a transport/timeout error rather
	// than a real model response (e.g. the 10-minute per-step deadline in
	// run.Step firing on unusually long generation). Confirmed via a live
	// repro: without this, an errored step left finalContent empty, and the
	// web-search grounding fallback below — meant only for a model that
	// ignored good search results — would fire on the empty content too,
	// silently replacing "the request failed" with a fabricated-looking
	// "Yes, I found current reporting..." answer built from stale results.
	stepErrored := false
	runEmit := func(event string, data any) { emit(event, data) }
	for {
		before := len(messages)
		done, newMessages, err := run.Step(ctx, messages, a.Registry, a.OllamaURL, runEmit)
		if err != nil {
			log.Printf("[agent] run %s step %d error: %v", runID, run.Steps, err)
			emit("error", map[string]any{"message": err.Error()})
			stepErrored = true
			break
		}
		messages = newMessages

		if done {
			if reply, ok := newAssistantReply(before, messages); ok {
				finalContent = reply
			}
			break
		}
	}

	// Small local models sometimes describe an action in prose instead of
	// actually calling the tool for it — a code block describing a file
	// change, or (just as often) a plain sentence like "please confirm the
	// installation" with no run_command call behind it at all. Both are the
	// same failure: the model narrated instead of acting. Give it exactly
	// one chance to correct course rather than silently accepting a
	// response that did nothing. Paused (asked a real clarifying question)
	// is excluded — that's a legitimate reason to have called no tool.
	narratedInsteadOfActing := containsCodeBlock(finalContent) || (promptRequestsAction(prompt) && !run.Paused)
	if !stepErrored && !directConversation && run.Steps < run.MaxSteps && narratedInsteadOfActing && !anyToolMessage(messages) {
		messages = append(messages, OllamaMessage{
			Role: "user",
			Content: "You just described an action in your response instead of performing it — the chat response must never contain code blocks or " +
				"claim a file/command change is staged unless you actually called a tool for it. Do not explain the change in prose. Call create_file, " +
				"replace_in_file, apply_patch, or delete_file now with the real content, or run_command now with the exact command, to actually do this.",
		})
		// Re-enter the same step loop (not a single extra call): the model
		// may respond to the nudge with a tool call rather than text
		// immediately, which needs further steps — including possibly more
		// tool calls — to reach a real final answer, exactly like the main
		// loop above. run.Step's own MaxSteps check bounds this.
		for {
			before := len(messages)
			done, newMessages, err := run.Step(ctx, messages, a.Registry, a.OllamaURL, runEmit)
			if err != nil {
				log.Printf("[agent] run %s corrective retry error: %v", runID, err)
				stepErrored = true
				break
			}
			messages = newMessages
			if done {
				// Only overwrite finalContent if this retry actually produced
				// something new — if it aborted, the original (narrated)
				// response from the main loop above is a better fallback
				// than blanking it out.
				if reply, ok := newAssistantReply(before, messages); ok {
					finalContent = reply
				}
				break
			}
		}
	}

	// A generation that blew past run.Step's thinking-token budget without
	// reaching content or a tool call gets exactly one direct nudge to stop
	// analyzing and commit to an action, same "one chance" pattern as
	// narratedInsteadOfActing above. If the nudge ALSO runs over budget,
	// don't retry a third time (and don't leave the user with silence) —
	// report it plainly instead.
	if !stepErrored && run.ThinkingBudgetExceeded && run.Steps < run.MaxSteps {
		run.ThinkingBudgetExceeded = false
		messages = append(messages, OllamaMessage{
			Role: "user",
			Content: "You've been reasoning for a very long time without reaching a decision. Stop analyzing further: immediately call the single " +
				"most relevant tool now, or call ask_follow_up if you genuinely need the user's input. Do not explain more — just act.",
		})
		for {
			before := len(messages)
			done, newMessages, err := run.Step(ctx, messages, a.Registry, a.OllamaURL, runEmit)
			if err != nil {
				log.Printf("[agent] run %s thinking-budget retry error: %v", runID, err)
				stepErrored = true
				break
			}
			messages = newMessages
			if done {
				// Only overwrite finalContent if this retry actually produced
				// something new. If it aborted (budget exceeded again), leave
				// finalContent as whatever it already was — which, if the
				// very first attempt also aborted with nothing new, is "" —
				// so the graceful timeout message right below actually fires,
				// instead of accidentally inheriting a stale assistant reply
				// from an earlier turn in this same conversation (live bug:
				// asking "what is today's date" twice in one session echoed
				// the first answer back as if it were a fresh one, because
				// the old backward-scan-for-any-assistant-message logic
				// doesn't distinguish "message from this attempt" from
				// "message from three turns ago").
				if reply, ok := newAssistantReply(before, messages); ok {
					finalContent = reply
				}
				break
			}
		}
	}
	// Checked unconditionally (not nested in the retry block above): if the
	// very first attempt exceeded budget but run.Steps had already reached
	// run.MaxSteps, the retry never ran at all, yet run.ThinkingBudgetExceeded
	// is still true and finalContent is still empty — this must still produce
	// a graceful message rather than fall through to the search-grounding
	// check below with nothing to show.
	if run.ThinkingBudgetExceeded && finalContent == "" {
		finalContent = "I'm having trouble reaching a clear answer for this without taking too long. Could you rephrase the question or break it into a smaller one?"
		emit("replace_content", map[string]any{"text": finalContent})
	}

	// run.Paused is excluded for the same reason as the corrective-retry check
	// above: a real ask_follow_up question is a legitimate response to
	// preflight results that turned out not to cover the question, not a
	// deflection to correct. anyToolMessage is excluded too — if the model
	// itself called search_internet again (per the grounding message's own
	// instruction to refine the query rather than force an answer), that's
	// the model actively working the problem, not something to override with
	// stale results from the first, already-rejected search. run.ThinkingBudgetExceeded
	// is excluded because the message right above is itself an honest,
	// deliberately-chosen final answer (a timeout notice) — not a dodge to
	// correct with fabricated "grounded" content pasted over it.
	if shouldApplyGroundingFallback(stepErrored, run.ThinkingBudgetExceeded, run.Paused, webResults, anyToolMessage(messages), finalContent) {
		finalContent = groundedSearchFallback(webQuery, webResults)
		emit("replace_content", map[string]any{"text": finalContent})
	}

	// Final rung of the escalation ladder. The search itself already tried to
	// resolve uncertainty (multiple engines, cross-checked against each
	// other); if the evidence still didn't corroborate, the honest move is to
	// hand the decision to the user rather than assert a weakly-sourced claim
	// as fact. The user can confirm it (which promotes it to a verified
	// memory) or send it back for another search with better terms.
	// A cross-source-verified answer is worth keeping: the next time this
	// subject comes up, recallVerifiedFact short-circuits the whole search.
	// Only "high" qualifies — several independent sources agreeing is the
	// bar, because anything weaker is exactly the false corroboration this
	// pipeline exists to catch.
	if !stepErrored && !run.Paused && webConfidence == "high" && strings.TrimSpace(finalContent) != "" &&
		sessionRecord != nil && !responseHonestlyReportsIrrelevantResults(finalContent) {
		a.rememberVerifiedFact(ctx, sessionRecord.ProjectID, modelPrompt, finalContent, webSources, "cross_source")
		emit("fact_verified", map[string]any{"sources": webSources, "verified_by": "cross_source"})
	}

	if shouldEscalateForVerification(stepErrored, webConfidence, run.Paused, finalContent) {
		emit("follow_up", map[string]any{
			"question": fmt.Sprintf("I could only find weak corroboration for this (%s). I don't want to state it as fact without checking — how should I proceed?",
				strings.ToLower(webConfidenceReason(webConfidence))),
			"options": []string{
				"That's correct — save it as verified",
				"Search again with better terms",
				"Drop it, I'll find out myself",
			},
			"input_type": "select",
		})
		run.Paused = true
	}

	// 7. Save the final assistant response to DB.
	if finalContent != "" {
		var timelineJSON string
		timelineMu.Lock()
		if len(timelineLog) > 0 {
			if encoded, err := json.Marshal(timelineLog); err == nil {
				timelineJSON = string(encoded)
			}
		}
		timelineMu.Unlock()
		assistantMsg := ctxpkg.Message{
			Role:      "assistant",
			Content:   finalContent,
			Timeline:  timelineJSON,
			Timestamp: time.Now(),
		}
		savedAssistant := ctxpkg.Store.AppendMessage(sessionID, assistantMsg)
		if savedAssistant != nil {
			emit("message_saved", map[string]any{"role": "assistant", "id": savedAssistant.ID})
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

	// 8. Emit agent done right after persistence so the UI unlocks (stop
	// button clears, next input becomes available) as soon as the visible
	// turn is actually finished — not after also generating a conversation
	// title/summary, which used to run synchronously here and could leave
	// the UI looking stuck for as long as that second, invisible model call
	// took (it competes with the main generation for the same Ollama server).
	emit("agent_done", map[string]any{
		"run_id": runID,
		"steps":  run.Steps,
	})

	// 9. Update the conversation title/summary in the background. This can
	// involve a second full model call (generateConversationMetadata), so it
	// must never block agent_done above. The HTTP handler may already have
	// returned and flushed its response by the time this finishes, so it
	// updates the DB directly rather than emitting further SSE events — the
	// title/summary simply becomes visible next time the session list loads.
	if finalContent != "" {
		if session, err := a.DB.GetSession(sessionID); err == nil {
			go func() {
				bgCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				var title, summary string
				if directConversation {
					title, summary = fallbackConversationMetadata(prompt, finalContent)
				} else {
					var metadataErr error
					title, summary, metadataErr = a.generateConversationMetadata(bgCtx, model, session.Title, session.Summary, prompt, finalContent)
					if metadataErr != nil {
						log.Printf("[agent] failed to generate conversation metadata: %v", metadataErr)
						title, summary = fallbackConversationMetadata(prompt, finalContent)
					}
				}
				if err := a.DB.UpdateSessionMetadata(sessionID, title, summary); err != nil {
					log.Printf("[agent] failed to save conversation metadata: %v", err)
				}
			}()
		}
	}

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

// promptRequestsAction matches prompts that ask for something to be done
// (create a file, install a package, run a build) rather than asked about —
// used to catch a model that responded with narration ("please confirm the
// installation...") instead of actually calling create_file/run_command/etc.
var actionRequestPattern = regexp.MustCompile(`(?i)\b(create|make|add|install|build|generate|write|implement|delete|remove|rename|fix|update|modify|refactor|scaffold|set\s?up|run|execute|initialize|init)\b`)

// requestsExplanationPattern matches prompts that ask to understand existing
// code rather than change it (explain, describe, review, analyze, "what does
// this do"). rephrasePrompt is skipped for these: live-tested against
// rephraserModel, it doesn't just rewrite this class of request — it answers
// it, confidently fabricating a description of code it has never actually
// seen (e.g. inventing a whole authentication flow for a file it was only
// given the name of). Rewriting an action request just rephrases what to do;
// rewriting an explain request risks handing the real model a fabricated
// "fact" about the codebase as if it were established truth.
var requestsExplanationPattern = regexp.MustCompile(`(?i)\b(explain|describe|review|analy[sz]\w*|understand|walk\s?me\s?through|what\s+does|how\s+does|what\s+is\s+this|summari[sz]\w*)\b`)

func promptRequestsExplanation(prompt string) bool {
	return requestsExplanationPattern.MatchString(prompt)
}

func promptRequestsAction(prompt string) bool {
	return actionRequestPattern.MatchString(prompt)
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
		"model":      model,
		"stream":     false,
		"format":     format,
		"keep_alive": ollamaKeepAlive,
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

// codingContextFollowers are words that, immediately after a time-ish marker
// like "current", signal an ordinary coding phrase ("current folder/file/
// directory/branch/project") rather than a request for current-events
// information — without this, "install X in the current folder" was
// (wrongly) triggering a web search.
var codingContextFollowers = regexp.MustCompile(`(?i)^\s*(folder|file|directory|dir|branch|project|repo|repository|workspace|module|version|state|code|line|selection|tab|working)\b`)

func needsInternetSearch(prompt string) bool {
	lower := strings.ToLower(prompt)
	markers := []string{
		"recent", "latest", "today", "yesterday", "currently", "current ", "breaking",
		"news", "protest", "election", "price", "weather", "this week", "this month", "this year",
	}
	for _, marker := range markers {
		index := strings.Index(lower, marker)
		if index < 0 {
			continue
		}
		if marker == "current " && codingContextFollowers.MatchString(lower[index+len(marker):]) {
			continue // "current folder/file/..." — not a current-events question
		}
		return true
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

// containsCodeBlock reports whether content has a fenced code block — the
// system prompt forbids these in chat responses, so seeing one is a signal
// the model wrote out a change instead of calling a file-mutation tool.
func containsCodeBlock(content string) bool {
	return strings.Contains(content, "```")
}

// anyToolMessage reports whether any tool was actually executed during this
// run. Tool-role messages only ever exist in the in-memory conversation for
// the current Run() call (only the final assistant text gets persisted), so
// this reflects this run alone, not prior turns.
func anyToolMessage(messages []OllamaMessage) bool {
	for _, m := range messages {
		if m.Role == "tool" {
			return true
		}
	}
	return false
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

// responseHonestlyReportsIrrelevantResults distinguishes "I read the results
// and they don't cover this" from the lazy training-cutoff deflections
// responseIgnoredSuccessfulSearch catches. The former is a legitimate outcome
// of a preflight search that missed — the model should not be forced into
// citing links that don't actually answer the question — while the latter is
// exactly the case the grounded-search fallback exists to correct.
func responseHonestlyReportsIrrelevantResults(content string) bool {
	lower := strings.ToLower(content)
	markers := []string{
		"don't contain information about", "do not contain information about",
		"don't address", "do not address", "don't cover", "do not cover",
		"doesn't cover", "does not cover", "don't mention", "do not mention",
		"doesn't mention", "does not mention", "no information about",
		"not relevant to", "aren't relevant to", "results don't", "results do not",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// shouldApplyGroundingFallback decides whether finalContent must be replaced
// with groundedSearchFallback's canned, results-citing answer. It must stay
// false whenever finalContent is already an honest final answer chosen
// deliberately elsewhere — a transport error (stepErrored), a thinking-budget
// timeout notice (run.ThinkingBudgetExceeded), a real clarifying question
// (run.Paused), or the model actively re-searching itself (anyToolMsg) — since
// overriding any of those pastes a fabricated "I found current reporting..."
// answer over a message that was already correct for its situation.
// newAssistantReply reports the assistant's reply from a Step call that just
// returned done=true, but only if that call actually appended something —
// i.e. len(messages) grew past beforeLen and the new tail message is the
// assistant's. A Step call that aborts (thinking-budget exceeded) returns
// messages completely unchanged, so beforeLen guards against mistaking an
// aborted attempt for a real answer by scanning backward into messages that
// were already there — which, in a multi-turn conversation, can be a
// perfectly real assistant reply from several turns ago rather than
// anything from this attempt.
func newAssistantReply(beforeLen int, messages []OllamaMessage) (string, bool) {
	if len(messages) <= beforeLen {
		return "", false
	}
	last := messages[len(messages)-1]
	if last.Role != "assistant" {
		return "", false
	}
	return last.Content, true
}

// shouldEscalateForVerification decides whether to hand a weakly-sourced
// answer to the user instead of asserting it. Only fires when a search
// actually ran and came back poorly corroborated ("low"), the run otherwise
// succeeded, and the model produced a confident-sounding answer anyway.
// Excluded cases: an errored run (nothing to verify), an answer the model
// already hedged or declined to give (it made the honest call itself), and a
// run already paused on its own ask_follow_up (two questions at once).
func shouldEscalateForVerification(stepErrored bool, webConfidence string, paused bool, finalContent string) bool {
	if stepErrored || paused || webConfidence != "low" || strings.TrimSpace(finalContent) == "" {
		return false
	}
	return !responseHonestlyReportsIrrelevantResults(finalContent)
}

func webConfidenceReason(confidence string) string {
	switch confidence {
	case "low":
		return "not enough independent sources agreed"
	case "medium":
		return "only two independent sources agreed"
	default:
		return "the sources were inconclusive"
	}
}

func shouldApplyGroundingFallback(stepErrored, thinkingBudgetExceeded, paused bool, webResults []internetSearchResult, anyToolMsg bool, finalContent string) bool {
	return !stepErrored && !thinkingBudgetExceeded && len(webResults) > 0 && !paused && !anyToolMsg && !responseGroundedInSuccessfulSearch(finalContent, webResults)
}

func responseGroundedInSuccessfulSearch(content string, results []internetSearchResult) bool {
	if responseIgnoredSuccessfulSearch(content) {
		return false
	}
	if responseHonestlyReportsIrrelevantResults(content) {
		return true
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

// persistedTimelineEvents are the events the frontend's ThinkingTimeline
// renders into entries — everything else (raw tokens, usage, bookkeeping) is
// either redundant with the final saved content or not meaningful to replay.
var persistedTimelineEvents = map[string]bool{
	"agent_step":        true,
	"thinking_token":    true,
	"tool_call":         true,
	"tool_result":       true,
	"context_resolved":  true,
	"context_candidate": true,
	"context_built":     true,
	"prompt_rephrased":  true,
}

// mergeThinkingToken folds a thinking_token event into the previous logged
// entry if it was also a thinking_token, mirroring how the frontend
// accumulates streamed thinking text into one timeline row. Returns true if
// it merged (caller should not append a new entry).
func mergeThinkingToken(log []map[string]any, event string, data any) bool {
	if event != "thinking_token" || len(log) == 0 {
		return false
	}
	last := log[len(log)-1]
	if last["type"] != "thinking_token" {
		return false
	}
	lastData, _ := last["data"].(map[string]any)
	newData, _ := data.(map[string]any)
	if lastData == nil || newData == nil {
		return false
	}
	lastText, _ := lastData["text"].(string)
	newText, _ := newData["text"].(string)
	lastData["text"] = lastText + newText
	return true
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
