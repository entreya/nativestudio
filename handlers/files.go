package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	workspacefs "github.com/entreya/nativestudio/workspace"
)

// FileEntry represents one immediate child of a directory listed by
// GET /api/files.
type FileEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"` // "file" or "dir"
	HasChildren bool   `json:"has_children,omitempty"`
}

// excludedTreeDirectories are never listed, so common dependency and
// build-output directories don't blow up the tree for large projects — they
// can easily contain tens of thousands of files that happen to match an
// allowed extension (e.g. .php inside vendor/, .js inside node_modules/).
var excludedTreeDirectories = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true,
	"coverage": true, "target": true, "tmp": true, "cache": true, ".cache": true,
	".next": true, ".nuxt": true, "__pycache__": true,
}

// FileHandler manages filesystem endpoints
type FileHandler struct {
	mu                sync.RWMutex
	RootDir           string
	AllowedExtensions []string
	onWorkspaceChange func(string, string)
}

// SetWorkspaceChangeHandler keeps other workspace-scoped services in sync.
func (h *FileHandler) SetWorkspaceChangeHandler(callback func(string, string)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onWorkspaceChange = callback
}

// NewFileHandler creates a new FileHandler
func NewFileHandler(rootDir string, allowedExtensions []string) *FileHandler {
	return &FileHandler{
		RootDir:           rootDir,
		AllowedExtensions: allowedExtensions,
	}
}

// RegisterRoutes registers the file endpoints on the given mux
func (h *FileHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/files", h.ListFiles)
	mux.HandleFunc("/api/file", h.HandleFile)
	mux.HandleFunc("/api/workspace", h.WorkspaceHandler)
	mux.HandleFunc("POST /api/files/create", h.CreateEntry)
	mux.HandleFunc("POST /api/files/rename", h.RenameEntry)
	mux.HandleFunc("POST /api/files/delete", h.DeleteEntry)
}

func (h *FileHandler) GetRootDir() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.RootDir
}

func (h *FileHandler) SetRootDir(path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.RootDir = path
}

// ListFiles handles GET /api/files?path=... and returns the immediate
// children of the given directory (the workspace root if path is omitted),
// one level at a time. The frontend fetches deeper levels lazily as the user
// expands them — returning the whole tree recursively in one response was
// what made opening a large project hang the browser, since it eagerly
// walked and shipped every file under the workspace (including inside
// node_modules/vendor/etc, which weren't even excluded) before anything
// could render.
func (h *FileHandler) ListFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rootDir := h.GetRootDir()
	full, err := (workspacefs.Guard{Root: rootDir}).Resolve(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	info, err := os.Stat(full)
	if err != nil || !info.IsDir() {
		http.Error(w, "not a directory", http.StatusBadRequest)
		return
	}

	entries, err := h.listDirectory(full, rootDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

// listDirectory returns the visible, allowed immediate children of dir,
// directories first then alphabetically.
func (h *FileHandler) listDirectory(dir, root string) ([]FileEntry, error) {
	rawEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := make([]FileEntry, 0, len(rawEntries))
	for _, entry := range rawEntries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		childPath := filepath.Join(dir, entry.Name())
		// entry.IsDir() reflects the dirent's own type, which is never "dir"
		// for a symlink even when it points at one — resolve through the
		// link so symlinked files/folders aren't silently dropped below.
		isDir, ok := resolvedIsDir(entry, childPath)
		if !ok {
			continue // broken symlink
		}
		if isDir {
			if excludedTreeDirectories[entry.Name()] {
				continue
			}
		} else if !h.extensionAllowed(entry.Name()) {
			continue
		}
		relPath, err := filepath.Rel(root, childPath)
		if err != nil {
			continue
		}
		relPath = "/" + strings.ReplaceAll(relPath, "\\", "/")
		entryType := "file"
		hasChildren := false
		if isDir {
			entryType = "dir"
			hasChildren = h.hasVisibleTreeChildren(childPath)
		}
		entries = append(entries, FileEntry{Name: entry.Name(), Path: relPath, Type: entryType, HasChildren: hasChildren})
	}
	sort.Slice(entries, func(i, j int) bool {
		if (entries[i].Type == "dir") != (entries[j].Type == "dir") {
			return entries[i].Type == "dir"
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

// hasVisibleTreeChildren reports whether dir has at least one entry that
// listDirectory would actually show, so the frontend knows whether to render
// an expand arrow without having to fetch and discard an empty result.
func (h *FileHandler) hasVisibleTreeChildren(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		isDir, ok := resolvedIsDir(entry, filepath.Join(dir, entry.Name()))
		if !ok {
			continue
		}
		if isDir {
			if !excludedTreeDirectories[entry.Name()] {
				return true
			}
			continue
		}
		if h.extensionAllowed(entry.Name()) {
			return true
		}
	}
	return false
}

// HandleFile routes GET and POST for single files
func (h *FileHandler) HandleFile(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.ReadFile(w, r)
	case http.MethodPost:
		h.WriteFile(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// isPathAllowed checks if the target path is within the root directory and has an allowed extension
func (h *FileHandler) isPathAllowed(targetPath string) (string, error) {
	fullPath, err := (workspacefs.Guard{Root: h.GetRootDir()}).Resolve(targetPath)
	if err != nil {
		return "", err
	}
	if !h.extensionAllowed(fullPath) {
		return "", fmt.Errorf("file extension %s not allowed", filepath.Ext(fullPath))
	}
	return fullPath, nil
}

// extensionAllowed reports whether name's extension is in AllowedExtensions.
func (h *FileHandler) extensionAllowed(name string) bool {
	ext := filepath.Ext(name)
	for _, allowedExt := range h.AllowedExtensions {
		if ext == allowedExt {
			return true
		}
	}
	return false
}

// ReadFile handles GET /api/file?path=...
func (h *FileHandler) ReadFile(w http.ResponseWriter, r *http.Request) {
	reqPath := r.URL.Query().Get("path")
	if reqPath == "" {
		http.Error(w, "path parameter is required", http.StatusBadRequest)
		return
	}

	fullPath, err := h.isPathAllowed(reqPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "file not found", http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	response := map[string]string{
		"path":    reqPath,
		"content": string(content),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// WriteFile handles POST /api/file
func (h *FileHandler) WriteFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	fullPath, err := h.isPathAllowed(req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		http.Error(w, "failed to create directory", http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(fullPath, []byte(req.Content), 0644); err != nil {
		http.Error(w, "failed to write file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// WorkspaceHandler handles GET and POST for workspace root
func (h *FileHandler) WorkspaceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"root_dir": h.GetRootDir()})
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Repointing the workspace root lets any later /api/file call read or write
	// anywhere under the chosen directory, so this must never be reachable from
	// the network even if the server is misconfigured to listen non-locally.
	if !isLoopbackRequest(r) {
		http.Error(w, "changing the workspace root is only available locally", http.StatusForbidden)
		return
	}
	var req struct {
		Path      string `json:"path"`
		ProjectID string `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	info, err := os.Stat(req.Path)
	if err != nil || !info.IsDir() {
		http.Error(w, "invalid directory path", http.StatusBadRequest)
		return
	}

	h.SetRootDir(req.Path)
	h.mu.RLock()
	callback := h.onWorkspaceChange
	h.mu.RUnlock()
	if callback != nil {
		callback(req.ProjectID, req.Path)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// resolveNonRoot resolves a workspace-relative path and additionally rejects
// the workspace root itself — used by rename/delete, where operating on the
// root would be catastrophic and is never a legitimate request.
func (h *FileHandler) resolveNonRoot(reqPath string) (string, error) {
	root := h.GetRootDir()
	full, err := (workspacefs.Guard{Root: root}).Resolve(reqPath)
	if err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if filepath.Clean(full) == filepath.Clean(absRoot) {
		return "", fmt.Errorf("cannot operate on the workspace root")
	}
	return full, nil
}

// CreateEntry handles POST /api/files/create — creates a new empty file or
// directory. This is a direct user action from the file tree (not an agent
// tool call), so it happens immediately rather than being staged for review.
func (h *FileHandler) CreateEntry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Type string `json:"type"` // "file" or "dir"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	if req.Type != "file" && req.Type != "dir" {
		http.Error(w, `type must be "file" or "dir"`, http.StatusBadRequest)
		return
	}

	full, err := (workspacefs.Guard{Root: h.GetRootDir()}).Resolve(req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if req.Type == "file" && !h.extensionAllowed(full) {
		http.Error(w, fmt.Sprintf("file extension %s not allowed", filepath.Ext(full)), http.StatusForbidden)
		return
	}
	if _, statErr := os.Lstat(full); statErr == nil {
		http.Error(w, "already exists", http.StatusConflict)
		return
	}
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		http.Error(w, "failed to create parent directory", http.StatusInternalServerError)
		return
	}

	if req.Type == "dir" {
		if err := os.Mkdir(full, 0755); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else if err := os.WriteFile(full, nil, 0644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// RenameEntry handles POST /api/files/rename — renames or moves a file or
// directory within the workspace.
func (h *FileHandler) RenameEntry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string `json:"path"`
		NewPath string `json:"new_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Path == "" || req.NewPath == "" {
		http.Error(w, "path and new_path are required", http.StatusBadRequest)
		return
	}

	source, err := h.resolveNonRoot(req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if _, statErr := os.Lstat(source); statErr != nil {
		http.Error(w, "source not found", http.StatusNotFound)
		return
	}
	destination, err := (workspacefs.Guard{Root: h.GetRootDir()}).Resolve(req.NewPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if _, statErr := os.Lstat(destination); statErr == nil {
		http.Error(w, "a file or directory already exists at the new path", http.StatusConflict)
		return
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		http.Error(w, "failed to create parent directory", http.StatusInternalServerError)
		return
	}
	if err := os.Rename(source, destination); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// DeleteEntry handles POST /api/files/delete — permanently removes a file or
// directory (recursively). The frontend must confirm with the user before
// calling this; there is no undo.
func (h *FileHandler) DeleteEntry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	full, err := h.resolveNonRoot(req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if _, statErr := os.Lstat(full); statErr != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := os.RemoveAll(full); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
