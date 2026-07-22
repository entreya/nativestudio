package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/entreya/nativestudio/db"
	"github.com/entreya/nativestudio/indexer"
	workspacefs "github.com/entreya/nativestudio/workspace"
)

type PatchHandler struct {
	db          *db.DB
	coordinator *indexer.Coordinator
}

func NewPatchHandler(database *db.DB, coordinator *indexer.Coordinator) *PatchHandler {
	return &PatchHandler{db: database, coordinator: coordinator}
}

func (h *PatchHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sessions/{sessionId}/patches", h.List)
	mux.HandleFunc("POST /api/sessions/{sessionId}/patches/approve", h.Approve)
	mux.HandleFunc("POST /api/sessions/{sessionId}/patches/reject", h.Reject)
	mux.HandleFunc("POST /api/patches/{patchId}/rollback", h.Rollback)
}

type patchIDsRequest struct {
	PatchIDs []string `json:"patch_ids"`
}

func (h *PatchHandler) List(w http.ResponseWriter, r *http.Request) {
	if _, err := h.db.GetSession(r.PathValue("sessionId")); err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	patches, err := h.db.ListPatches(r.Context(), r.PathValue("sessionId"), r.URL.Query().Get("status"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if patches == nil {
		patches = []db.Patch{}
	}
	writeJSON(w, map[string]any{"patches": patches})
}

func (h *PatchHandler) Approve(w http.ResponseWriter, r *http.Request) {
	var request patchIDsRequest
	if json.NewDecoder(r.Body).Decode(&request) != nil || len(request.PatchIDs) == 0 {
		http.Error(w, "patch_ids are required", http.StatusBadRequest)
		return
	}
	sessionID := r.PathValue("sessionId")
	applied := make([]string, 0, len(request.PatchIDs))
	failed := make([]map[string]string, 0)
	for _, id := range request.PatchIDs {
		patch, projectRoot, target, err := h.resolvePatch(r.Context(), sessionID, id)
		if err == nil && patch.Status != "pending" {
			err = fmt.Errorf("patch is no longer pending")
		}
		if err == nil {
			err = applyStagedPatch(target, patch)
		}
		if err == nil {
			var updated bool
			updated, err = h.db.SetPatchStatus(r.Context(), id, "pending", "applied")
			if err == nil && !updated {
				err = fmt.Errorf("patch is no longer pending")
			}
		}
		if err != nil {
			failed = append(failed, map[string]string{"patch_id": id, "error": err.Error()})
			continue
		}
		applied = append(applied, id)
		h.refreshIndex(patch, projectRoot, target)
	}
	writeJSON(w, map[string]any{"applied": applied, "failed": failed})
}

func (h *PatchHandler) Reject(w http.ResponseWriter, r *http.Request) {
	var request patchIDsRequest
	if json.NewDecoder(r.Body).Decode(&request) != nil || len(request.PatchIDs) == 0 {
		http.Error(w, "patch_ids are required", http.StatusBadRequest)
		return
	}
	sessionID := r.PathValue("sessionId")
	rejected := []string{}
	failed := []map[string]string{}
	for _, id := range request.PatchIDs {
		patch, err := h.db.GetPatch(r.Context(), id)
		if err != nil || patch.SessionID != sessionID {
			failed = append(failed, map[string]string{"patch_id": id, "error": "patch not found"})
			continue
		}
		updated, err := h.db.SetPatchStatus(r.Context(), id, "pending", "rejected")
		if err != nil || !updated {
			message := "patch is no longer pending"
			if err != nil {
				message = err.Error()
			}
			failed = append(failed, map[string]string{"patch_id": id, "error": message})
			continue
		}
		rejected = append(rejected, id)
	}
	writeJSON(w, map[string]any{"ok": len(failed) == 0, "rejected": rejected, "failed": failed})
}

func (h *PatchHandler) Rollback(w http.ResponseWriter, r *http.Request) {
	patch, projectRoot, target, err := h.resolvePatch(r.Context(), "", r.PathValue("patchId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if patch.Status != "applied" {
		http.Error(w, "patch is not applied", http.StatusConflict)
		return
	}
	if err := rollbackAppliedPatch(target, patch); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	updated, err := h.db.SetPatchStatus(r.Context(), patch.ID, "applied", "rolled_back")
	if err != nil || !updated {
		http.Error(w, "could not mark patch rolled back", http.StatusConflict)
		return
	}
	h.refreshIndex(patch, projectRoot, target)
	writeJSON(w, map[string]bool{"ok": true})
}

func (h *PatchHandler) resolvePatch(ctx context.Context, sessionID, patchID string) (db.Patch, string, string, error) {
	patch, err := h.db.GetPatch(ctx, patchID)
	if err != nil || (sessionID != "" && patch.SessionID != sessionID) {
		return patch, "", "", fmt.Errorf("patch not found")
	}
	session, err := h.db.GetSession(patch.SessionID)
	if err != nil {
		return patch, "", "", fmt.Errorf("session not found")
	}
	project, err := h.db.GetProject(session.ProjectID)
	if err != nil {
		return patch, "", "", fmt.Errorf("workspace not found")
	}
	target, err := (workspacefs.Guard{Root: project.Path}).Resolve(patch.FilePath)
	if err != nil {
		return patch, "", "", err
	}
	return patch, project.Path, target, nil
}

func applyStagedPatch(target string, patch db.Patch) error {
	switch patch.Operation {
	case "create":
		if _, err := os.Lstat(target); err == nil {
			return fmt.Errorf("target already exists")
		} else if !os.IsNotExist(err) {
			return err
		}
		return atomicWrite(target, []byte(patch.NewContent))
	case "modify":
		current, err := os.ReadFile(target)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, []byte(patch.OriginalContent)) {
			return fmt.Errorf("file changed since the patch was staged")
		}
		return atomicWrite(target, []byte(patch.NewContent))
	case "delete":
		current, err := os.ReadFile(target)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, []byte(patch.OriginalContent)) {
			return fmt.Errorf("file changed since the patch was staged")
		}
		return os.Remove(target)
	default:
		return fmt.Errorf("unsupported operation %q", patch.Operation)
	}
}

func rollbackAppliedPatch(target string, patch db.Patch) error {
	switch patch.Operation {
	case "create":
		current, err := os.ReadFile(target)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, []byte(patch.NewContent)) {
			return fmt.Errorf("created file changed after the patch was applied")
		}
		return os.Remove(target)
	case "modify":
		current, err := os.ReadFile(target)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, []byte(patch.NewContent)) {
			return fmt.Errorf("file changed after the patch was applied")
		}
		return atomicWrite(target, []byte(patch.OriginalContent))
	case "delete":
		if _, err := os.Lstat(target); err == nil {
			return fmt.Errorf("deleted path was recreated after the patch was applied")
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return atomicWrite(target, []byte(patch.OriginalContent))
	default:
		return fmt.Errorf("unsupported operation %q", patch.Operation)
	}
}

func (h *PatchHandler) refreshIndex(patch db.Patch, root, target string) {
	if h.coordinator == nil {
		return
	}
	session, err := h.db.GetSession(patch.SessionID)
	if err != nil {
		return
	}
	if _, err := os.Stat(target); os.IsNotExist(err) {
		h.coordinator.RemoveFile(context.Background(), session.ProjectID, root, target)
		return
	}
	go h.coordinator.IndexFile(context.Background(), session.ProjectID, root, target)
}
