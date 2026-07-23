package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/entreya/nativestudio/db"
)

type ProjectsHandler struct {
	db *db.DB
}

func NewProjectsHandler(database *db.DB) *ProjectsHandler {
	return &ProjectsHandler{db: database}
}

func (h *ProjectsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects", h.HandleListProjects)
	mux.HandleFunc("POST /api/projects", h.HandleCreateProject)
	mux.HandleFunc("GET /api/projects/{id}", h.HandleGetProject)
	mux.HandleFunc("DELETE /api/projects/{id}", h.HandleDeleteProject)
}

func (h *ProjectsHandler) HandleListProjects(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	projects, err := h.db.ListProjects()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if projects == nil {
		projects = []db.Project{}
	}
	json.NewEncoder(w).Encode(map[string]any{"projects": projects})
}

func (h *ProjectsHandler) HandleCreateProject(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		Name        string `json:"name"`
		Path        string `json:"path"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Opening a folder that's already registered as a project (e.g. via the
	// "Open Folder" picker) must reuse that project rather than fail on the
	// path's UNIQUE constraint — the caller just wants to switch to it.
	if existing, err := h.db.GetProjectByPath(req.Path); err == nil {
		json.NewEncoder(w).Encode(map[string]any{"project": existing})
		return
	}

	id := db.NewID()
	project, err := h.db.CreateProject(id, req.Name, req.Path, req.Description)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"project": project})
}

func (h *ProjectsHandler) HandleGetProject(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing ID", http.StatusBadRequest)
		return
	}

	project, err := h.db.GetProject(id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"project": project})
}

func (h *ProjectsHandler) HandleDeleteProject(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing ID", http.StatusBadRequest)
		return
	}

	if err := h.db.DeleteProject(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
