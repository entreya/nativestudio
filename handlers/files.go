package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	workspacefs "github.com/entreya/nativestudio/workspace"
)

// FileNode represents a file or directory in the file tree
type FileNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	Type     string      `json:"type"` // "file" or "dir"
	Children []*FileNode `json:"children,omitempty"`
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

// ListFiles walks the RootDir and returns a JSON tree
func (h *FileHandler) ListFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rootDir := h.GetRootDir()
	tree, err := h.buildTree(rootDir, rootDir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return an empty array if tree has no children
	if tree == nil || tree.Children == nil {
		tree = &FileNode{Children: []*FileNode{}}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tree.Children)
}

// buildTree recursively builds the file tree
func (h *FileHandler) buildTree(currentPath string, root string) (*FileNode, error) {
	info, err := os.Stat(currentPath)
	if err != nil {
		return nil, err
	}

	relPath, err := filepath.Rel(root, currentPath)
	if err != nil {
		return nil, err
	}

	// Prepend slash to represent absolute path from root
	relPath = "/" + relPath
	if relPath == "/." {
		relPath = "/"
	}
	// Normalize path separators for Windows to use forward slashes in API
	relPath = strings.ReplaceAll(relPath, "\\", "/")

	node := &FileNode{
		Name: info.Name(),
		Path: relPath,
		Type: "file",
	}

	if info.IsDir() {
		node.Type = "dir"
		entries, err := os.ReadDir(currentPath)
		if err != nil {
			return nil, err
		}

		node.Children = make([]*FileNode, 0)
		for _, entry := range entries {
			// Skip hidden files/directories
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}

			childPath := filepath.Join(currentPath, entry.Name())

			// If it's a file, check extensions
			if !entry.IsDir() {
				ext := filepath.Ext(entry.Name())
				allowed := false
				for _, allowedExt := range h.AllowedExtensions {
					if ext == allowedExt {
						allowed = true
						break
					}
				}
				if !allowed {
					continue
				}
			}

			childNode, err := h.buildTree(childPath, root)
			if err != nil {
				continue // Skip unreadable
			}
			node.Children = append(node.Children, childNode)
		}
	}

	return node, nil
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

	// Check extension
	ext := filepath.Ext(fullPath)
	allowed := false
	for _, allowedExt := range h.AllowedExtensions {
		if ext == allowedExt {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("file extension %s not allowed", ext)
	}

	return fullPath, nil
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
