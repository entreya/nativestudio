package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/entreya/nativestudio/context"
	"github.com/entreya/nativestudio/db"
)

type SessionsHandler struct {
	db *db.DB
}

func NewSessionsHandler(database *db.DB) *SessionsHandler {
	return &SessionsHandler{db: database}
}

func (h *SessionsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sessions", h.HandleListAllSessions)
	mux.HandleFunc("GET /api/projects/{id}/sessions", h.HandleListSessions)
	mux.HandleFunc("POST /api/projects/{id}/sessions", h.HandleCreateSession)
	mux.HandleFunc("GET /api/sessions/{id}/messages", h.HandleGetMessages)
	mux.HandleFunc("DELETE /api/sessions/{id}", h.HandleDeleteSession)
	mux.HandleFunc("PATCH /api/messages/{id}", h.HandleUpdateMessage)
}

func (h *SessionsHandler) HandleUpdateMessage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	messageID := r.PathValue("id")
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if messageID == "" || req.Content == "" {
		http.Error(w, "message id and content are required", http.StatusBadRequest)
		return
	}
	if err := h.db.UpdateMessage(messageID, req.Content, context.EstimateTokens(req.Content)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (h *SessionsHandler) HandleListAllSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	conversations, err := h.db.ListConversationOverviews()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if conversations == nil {
		conversations = []db.ConversationOverview{}
	}
	for i := range conversations {
		conversations[i].TokenBudget = context.GetModelContextWindow(conversations[i].Model)
	}
	json.NewEncoder(w).Encode(map[string]any{"conversations": conversations})
}

func (h *SessionsHandler) HandleListSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	projectID := r.PathValue("id")

	sessions, err := h.db.ListSessions(projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sessions == nil {
		sessions = []db.Session{}
	}
	json.NewEncoder(w).Encode(map[string]any{"sessions": sessions})
}

func (h *SessionsHandler) HandleCreateSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	projectID := r.PathValue("id")

	var req struct {
		Model string `json:"model"`
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	id := db.NewID()
	session, err := h.db.CreateSession(id, projectID, req.Model, req.Title)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"session": session})
}

func (h *SessionsHandler) HandleGetMessages(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	sessionID := r.PathValue("id")

	messages, err := h.db.GetMessages(sessionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if messages == nil {
		messages = []db.Message{}
	}
	json.NewEncoder(w).Encode(map[string]any{"messages": messages})
}

func (h *SessionsHandler) HandleDeleteSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	sessionID := r.PathValue("id")

	if err := h.db.DeleteSession(sessionID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Session messages, checkpoints, and editor state are removed by the
	// database's ON DELETE CASCADE constraints.
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
