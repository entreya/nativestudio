package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/entreya/nativestudio/db"
	"github.com/entreya/nativestudio/indexer"
	workspacefs "github.com/entreya/nativestudio/workspace"
)

type ChangesHandler struct {
	db          *db.DB
	coordinator *indexer.Coordinator
}

func NewChangesHandler(database *db.DB, coordinator *indexer.Coordinator) *ChangesHandler {
	return &ChangesHandler{db: database, coordinator: coordinator}
}
func (h *ChangesHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/changes/{id}/approve", h.Approve)
	mux.HandleFunc("POST /api/changes/{id}/reject", h.Reject)
}
func (h *ChangesHandler) Approve(w http.ResponseWriter, r *http.Request) {
	change, err := h.db.GetFileChange(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, "change not found", 404)
		return
	}
	if change.Status != "pending" {
		http.Error(w, "change is no longer pending", 409)
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}
	project, err := h.db.GetProject(change.WorkspaceID)
	if err != nil {
		http.Error(w, "workspace not found", 404)
		return
	}
	target, err := (workspacefs.Guard{Root: project.Path}).Resolve(change.Path)
	if err != nil {
		http.Error(w, err.Error(), 403)
		return
	}
	if current, readErr := os.ReadFile(target); readErr == nil && change.OriginalHash != "" {
		currentSum := sha256.Sum256(current)
		if hex.EncodeToString(currentSum[:]) != change.OriginalHash {
			http.Error(w, "file changed since this proposal was created; regenerate the change", http.StatusConflict)
			return
		}
	}
	if err := atomicWrite(target, []byte(req.Content)); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	sum := sha256.Sum256([]byte(req.Content))
	hash := hex.EncodeToString(sum[:])
	if err := h.db.SetFileChangeStatus(r.Context(), change.ID, "approved", hash); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	learning, _ := json.Marshal(map[string]any{"files_changed": []string{change.Path}, "change_summary": change.Summary, "patch_status": "approved"})
	_ = h.db.RecordSessionLearning(r.Context(), change.WorkspaceID, change.SessionID, "change:"+change.ID, string(learning), "approved")
	_ = h.db.RecordDecision(r.Context(), change.WorkspaceID, change.SessionID, change.Summary, "Accepted AI change", "accepted")
	go h.coordinator.IndexFile(context.Background(), change.WorkspaceID, project.Path, target)
	writeJSON(w, map[string]bool{"ok": true})
}
func (h *ChangesHandler) Reject(w http.ResponseWriter, r *http.Request) {
	change, err := h.db.GetFileChange(r.Context(), r.PathValue("id"))
	if err != nil {
		http.Error(w, "change not found", 404)
		return
	}
	if err := h.db.SetFileChangeStatus(r.Context(), change.ID, "rejected", ""); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	learning, _ := json.Marshal(map[string]any{"files_changed": []string{}, "rejected_path": change.Path, "rejected_approach": change.Summary, "patch_status": "rejected"})
	_ = h.db.RecordSessionLearning(r.Context(), change.WorkspaceID, change.SessionID, "change:"+change.ID, string(learning), "rejected")
	_ = h.db.RecordDecision(r.Context(), change.WorkspaceID, change.SessionID, change.Summary, "Rejected AI change", "rejected")
	writeJSON(w, map[string]bool{"ok": true})
}
func atomicWrite(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".nativestudio-change-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err = temp.Write(content); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if info, statErr := os.Stat(path); statErr == nil {
		_ = os.Chmod(name, info.Mode())
	}
	return os.Rename(name, path)
}
