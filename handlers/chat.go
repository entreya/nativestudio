package handlers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/entreya/nativestudio/context"
)

// ChatHandler manages chat requests proxying to Ollama
type ChatHandler struct {
	OllamaURL  string
	ContextCfg context.Config
}

// NewChatHandler creates a new ChatHandler
func NewChatHandler(ollamaURL string, ctxCfg context.Config) *ChatHandler {
	return &ChatHandler{
		OllamaURL:  ollamaURL,
		ContextCfg: ctxCfg,
	}
}

// RegisterRoutes registers the chat endpoints
func (h *ChatHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/chat", h.HandleChat)
	mux.HandleFunc("/api/context", h.GetContext)
	mux.HandleFunc("/api/context/reset", h.ResetContext)
}

// HandleChat proxies chat to Ollama with full context management
func (h *ChatHandler) HandleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqBody struct {
		Prompt    string `json:"prompt"`
		Model     string `json:"model"`
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

	// 1. Get or create session
	session := context.Store.GetOrCreate(reqBody.SessionID)

	// 2. Check token usage
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

	// 3. If ShouldSummarize() -> background goroutine
	if context.ShouldSummarize(pct, h.ContextCfg) {
		go func(s *context.Session, cfg context.Config) {
			_ = context.Summarize(s, cfg)
		}(session, h.ContextCfg)
	}

	// 4. Append user message
	userMsg := context.Message{
		Role:      "user",
		Content:   reqBody.Prompt,
		Timestamp: time.Now(),
	}
	context.Store.AppendMessage(reqBody.SessionID, userMsg)

	// 5. Build Ollama request with all messages
	allMessages := context.Store.GetMessages(reqBody.SessionID)
	var ollamaMessages []map[string]string
	for _, m := range allMessages {
		ollamaMessages = append(ollamaMessages, map[string]string{
			"role":    m.Role,
			"content": m.Content,
		})
	}

	ollamaReq := map[string]interface{}{
		"model":    reqBody.Model,
		"messages": ollamaMessages,
		"stream":   true,
	}

	reqBytes, err := json.Marshal(ollamaReq)
	if err != nil {
		http.Error(w, "failed to marshal ollama request", http.StatusInternalServerError)
		return
	}

	// 6. Proxy and stream
	resp, err := http.Post(h.OllamaURL+"/api/chat", "application/json", bytes.NewReader(reqBytes))
	if err != nil {
		http.Error(w, "failed to reach ollama", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	var fullAssistantResponse string

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var chunk struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done bool `json:"done"`
		}

		if err := json.Unmarshal(line, &chunk); err != nil {
			continue
		}

		fullAssistantResponse += chunk.Message.Content

		sseData := map[string]interface{}{
			"token": chunk.Message.Content,
		}
		sseBytes, _ := json.Marshal(sseData)
		fmt.Fprintf(w, "data: %s\n\n", sseBytes)
		flusher.Flush()

		if chunk.Done {
			break
		}
	}

	fmt.Fprintf(w, "data: {\"done\": true}\n\n")
	flusher.Flush()

	// 7. Append assistant response
	if fullAssistantResponse != "" {
		assistantMsg := context.Message{
			Role:      "assistant",
			Content:   fullAssistantResponse,
			Timestamp: time.Now(),
		}
		context.Store.AppendMessage(reqBody.SessionID, assistantMsg)
	}
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

	// Assuming default model for token usage if not provided in query
	model := r.URL.Query().Get("model")
	if model == "" {
		model = "codellama" // Default fallback
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

// ResetContext handles POST /api/context/reset
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
