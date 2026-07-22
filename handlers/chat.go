package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/entreya/nativestudio/agent"
	"github.com/entreya/nativestudio/context"
	"github.com/entreya/nativestudio/db"
	"github.com/entreya/nativestudio/editor"
)

// ChatHandler manages chat requests proxying to Ollama via the Agent.
type ChatHandler struct {
	OllamaURL  string
	ContextCfg context.Config
	Agent      *agent.Agent
	DB         *db.DB
}

// NewChatHandler creates a new ChatHandler.
func NewChatHandler(ollamaURL string, ctxCfg context.Config, agentRunner *agent.Agent, database *db.DB) *ChatHandler {
	return &ChatHandler{
		OllamaURL:  ollamaURL,
		ContextCfg: ctxCfg,
		Agent:      agentRunner,
		DB:         database,
	}
}

// RegisterRoutes registers the chat endpoints.
func (h *ChatHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/chat", h.HandleChat)
	mux.HandleFunc("/api/context", h.GetContext)
	mux.HandleFunc("/api/context/reset", h.ResetContext)
}

// HandleChat proxies chat to Ollama using the agent loop for tool calling.
func (h *ChatHandler) HandleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqBody struct {
		Prompt        string              `json:"prompt"`
		Model         string              `json:"model"`
		SessionID     string              `json:"session_id"`
		Think         bool                `json:"think"`
		ThinkLevel    string              `json:"think_level"`
		EditorContext *editor.EditorState `json:"editor_context,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if reqBody.SessionID == "" {
		http.Error(w, "session_id is required", http.StatusBadRequest)
		return
	}

	// Validate session exists in DB — return 400 if not found
	if _, err := h.DB.GetSession(reqBody.SessionID); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "session_not_found",
			"message": fmt.Sprintf("session %s not found", reqBody.SessionID),
		})
		return
	}

	// Build editor state from request (zero value if not provided)
	state := editor.EditorState{}
	if reqBody.EditorContext != nil {
		state = *reqBody.EditorContext
	}

	// Check token usage
	_, _, pct := context.Store.GetTokenUsage(reqBody.SessionID, reqBody.Model)
	if context.ShouldBlock(pct, h.ContextCfg) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "context_full",
			"message": "Summarizing...",
		})
		return
	}

	// If approaching context limit, kick off summarization in background
	if context.ShouldSummarize(pct, h.ContextCfg) {
		sess := context.Store.GetOrCreate(reqBody.SessionID)
		go func(s *context.Session, cfg context.Config) {
			_ = context.Summarize(s, cfg)
		}(sess, h.ContextCfg)
	}

	// Setup SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Helper to emit SSE events
	emit := func(event string, data any) {
		b, _ := json.Marshal(map[string]any{"type": event, "data": data})
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}

	// Run the Agent Loop
	_, err := h.Agent.Run(r.Context(), reqBody.SessionID, reqBody.Prompt, reqBody.Model, reqBody.Think, reqBody.ThinkLevel, state, emit)
	if err != nil {
		emit("error", map[string]any{"message": err.Error()})
	}

	// Send final done event to signal client connection close
	fmt.Fprintf(w, "data: {\"done\": true}\n\n")
	flusher.Flush()
}

// GetContext handles GET /api/context?session_id=...
func (h *ChatHandler) GetContext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		http.Error(w, "session_id parameter is required", http.StatusBadRequest)
		return
	}

	model := r.URL.Query().Get("model")
	if model == "" {
		model = "qwen2.5-coder:1.5b"
	}

	session := context.Store.GetOrCreate(sessionID)
	used, total, pct := context.Store.GetTokenUsage(sessionID, model)

	zone := "green"
	if context.ShouldBlock(pct, h.ContextCfg) {
		zone = "red"
	} else if context.ShouldSummarize(pct, h.ContextCfg) {
		zone = "yellow"
	}

	type CheckpointResp struct {
		CreatedAt    time.Time `json:"created_at"`
		TokensBefore int       `json:"tokens_before"`
		TokensAfter  int       `json:"tokens_after"`
		Summary      string    `json:"summary"`
	}

	var cpResp []CheckpointResp
	for _, cp := range session.Checkpoints {
		cpResp = append(cpResp, CheckpointResp{
			CreatedAt:    cp.CreatedAt,
			TokensBefore: cp.TokensBefore,
			TokensAfter:  cp.TokensAfter,
			Summary:      cp.Summary,
		})
	}
	if cpResp == nil {
		cpResp = []CheckpointResp{}
	}

	response := map[string]interface{}{
		"session_id":       session.ID,
		"message_count":    len(session.Messages),
		"checkpoint_count": len(session.Checkpoints),
		"tokens_used":      used,
		"tokens_total":     total,
		"usage_pct":        pct,
		"zone":             zone,
		"checkpoints":      cpResp,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// ResetContext handles POST /api/context/reset.
func (h *ChatHandler) ResetContext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqBody struct {
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if reqBody.SessionID == "" {
		http.Error(w, "session_id is required", http.StatusBadRequest)
		return
	}

	context.Store.Reset(reqBody.SessionID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
