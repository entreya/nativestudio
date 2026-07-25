package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/entreya/nativestudio/db"
	"github.com/entreya/nativestudio/indexer"
	"github.com/entreya/nativestudio/knowledge"
)

type KnowledgeHandler struct {
	db          *db.DB
	coordinator *indexer.Coordinator
	broker      *indexer.Broker
}

func NewKnowledgeHandler(database *db.DB, coordinator *indexer.Coordinator, broker *indexer.Broker) *KnowledgeHandler {
	return &KnowledgeHandler{db: database, coordinator: coordinator, broker: broker}
}
func (h *KnowledgeHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{id}/knowledge", h.Overview)
	mux.HandleFunc("GET /api/projects/{id}/index/status", h.Status)
	mux.HandleFunc("GET /api/projects/{id}/index/events", h.Events)
	mux.HandleFunc("POST /api/projects/{id}/index", h.Reindex)
	mux.HandleFunc("POST /api/projects/{id}/index/stop", h.StopIndex)
	mux.HandleFunc("POST /api/projects/{id}/knowledge/enrichment/approve", h.ApproveEnrichment)
	mux.HandleFunc("POST /api/projects/{id}/knowledge/enrichment/decline", h.DeclineEnrichment)
	mux.HandleFunc("POST /api/projects/{id}/knowledge/enrichment/pause", h.PauseEnrichment)
	mux.HandleFunc("POST /api/projects/{id}/knowledge/enrichment/resume", h.ResumeEnrichment)
	mux.HandleFunc("POST /api/projects/{id}/index/files", h.ReindexFile)
	mux.HandleFunc("POST /api/projects/{id}/knowledge/relearn", h.Relearn)
	mux.HandleFunc("PATCH /api/projects/{id}/knowledge/facts/{factID}", h.UpdateFact)
	mux.HandleFunc("DELETE /api/projects/{id}/knowledge/facts/{factID}", h.DeleteFact)
	mux.HandleFunc("GET /api/projects/{id}/knowledge/settings", h.GetSettings)
	mux.HandleFunc("PUT /api/projects/{id}/knowledge/settings", h.SaveSettings)
}
func (h *KnowledgeHandler) defaultSettings() knowledge.IndexSettings {
	config := indexer.DefaultScanConfig()
	return knowledge.IndexSettings{ExcludePaths: config.ExcludedDirectories, SensitivePatterns: config.SensitivePatterns, MaximumFileSizeBytes: config.MaximumFileSizeBytes}
}
func (h *KnowledgeHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.db.GetIndexSettings(r.Context(), r.PathValue("id"), h.defaultSettings())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, settings)
}
func (h *KnowledgeHandler) SaveSettings(w http.ResponseWriter, r *http.Request) {
	var settings knowledge.IndexSettings
	if json.NewDecoder(r.Body).Decode(&settings) != nil || settings.MaximumFileSizeBytes <= 0 {
		http.Error(w, "invalid settings", 400)
		return
	}
	if err := h.db.SaveIndexSettings(r.Context(), r.PathValue("id"), settings); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	h.coordinator.Configure(settings)
	writeJSON(w, map[string]bool{"ok": true})
}
func (h *KnowledgeHandler) ReindexFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.Path == "" {
		http.Error(w, "path is required", 400)
		return
	}
	project, err := h.db.GetProject(r.PathValue("id"))
	if err != nil {
		http.Error(w, "project not found", 404)
		return
	}
	go h.coordinator.IndexFile(context.Background(), project.ID, project.Path, req.Path)
	writeJSON(w, map[string]bool{"ok": true})
}
func (h *KnowledgeHandler) UpdateFact(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Fact   string `json:"fact"`
		Status string `json:"status"`
		Pinned bool   `json:"pinned"`
		IsRule bool   `json:"is_rule"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.Fact == "" {
		http.Error(w, "invalid fact", 400)
		return
	}
	if req.Status == "" {
		req.Status = "verified"
	}
	if err := h.db.UpdateFact(r.Context(), r.PathValue("id"), r.PathValue("factID"), req.Fact, req.Status, req.Pinned, req.IsRule); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
func (h *KnowledgeHandler) DeleteFact(w http.ResponseWriter, r *http.Request) {
	if err := h.db.DeleteFact(r.Context(), r.PathValue("id"), r.PathValue("factID")); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
func (h *KnowledgeHandler) Overview(w http.ResponseWriter, r *http.Request) {
	result, err := h.db.KnowledgeOverview(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, result)
}
func (h *KnowledgeHandler) Status(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("id")
	result, err := h.db.LatestIndexStatus(r.Context(), workspaceID, db.EnrichmentGate{
		Approved: h.coordinator.IsEnrichmentApproved(workspaceID),
		Declined: h.coordinator.IsEnrichmentDeclined(workspaceID),
		Paused:   h.coordinator.IsEnrichmentPaused(workspaceID),
	})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, result)
}
func (h *KnowledgeHandler) Reindex(w http.ResponseWriter, r *http.Request) {
	project, err := h.db.GetProject(r.PathValue("id"))
	if err != nil {
		http.Error(w, "project not found", 404)
		return
	}
	h.coordinator.Restart(project.ID, project.Path)
	writeJSON(w, map[string]bool{"ok": true})
}
func (h *KnowledgeHandler) StopIndex(w http.ResponseWriter, r *http.Request) {
	h.coordinator.Cancel()
	writeJSON(w, map[string]bool{"ok": true})
}
func (h *KnowledgeHandler) ApproveEnrichment(w http.ResponseWriter, r *http.Request) {
	h.coordinator.ApproveEnrichment(r.PathValue("id"))
	writeJSON(w, map[string]bool{"ok": true})
}
func (h *KnowledgeHandler) DeclineEnrichment(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("id")
	h.coordinator.DeclineEnrichment(workspaceID)
	h.broker.Emit(workspaceID, "enrichment_declined", map[string]any{})
	writeJSON(w, map[string]bool{"ok": true})
}
func (h *KnowledgeHandler) PauseEnrichment(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("id")
	h.coordinator.PauseEnrichment(workspaceID)
	writeJSON(w, map[string]bool{"ok": true})
}
func (h *KnowledgeHandler) ResumeEnrichment(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("id")
	h.coordinator.ResumeEnrichment(workspaceID)
	h.broker.Emit(workspaceID, "enrichment_resumed", map[string]any{})
	writeJSON(w, map[string]bool{"ok": true})
}
func (h *KnowledgeHandler) Relearn(w http.ResponseWriter, r *http.Request) {
	project, err := h.db.GetProject(r.PathValue("id"))
	if err != nil {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	h.coordinator.Cancel()
	if err := h.db.ResetProjectKnowledge(r.Context(), project.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.broker.Emit(project.ID, "knowledge_reset", map[string]any{"workspace_id": project.ID})
	h.coordinator.Restart(project.ID, project.Path)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
func (h *KnowledgeHandler) Events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	events, unsubscribe := h.broker.Subscribe(r.PathValue("id"))
	defer unsubscribe()
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-events:
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(value)
}
