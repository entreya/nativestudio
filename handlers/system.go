package handlers

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type SystemHandler struct {
	home              string
	allowedExtensions map[string]struct{}
}

type browserEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"`
	HasChildren bool   `json:"has_children"`
}

func NewSystemHandler(allowedExtensions []string) *SystemHandler {
	home, err := os.UserHomeDir()
	if err != nil {
		home = string(filepath.Separator)
	}
	allowed := make(map[string]struct{}, len(allowedExtensions))
	for _, extension := range allowedExtensions {
		allowed[strings.ToLower(extension)] = struct{}{}
	}
	return &SystemHandler{home: filepath.Clean(home), allowedExtensions: allowed}
}

func (h *SystemHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/system/browse", h.Browse)
}

func (h *SystemHandler) Browse(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		http.Error(w, "filesystem browsing is only available locally", http.StatusForbidden)
		return
	}

	requested := strings.TrimSpace(r.URL.Query().Get("path"))
	if requested == "" {
		requested = h.home
	}
	absolute, err := filepath.Abs(requested)
	if err != nil {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	absolute = filepath.Clean(absolute)
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		http.Error(w, "directory not found", http.StatusNotFound)
		return
	}

	mode := r.URL.Query().Get("mode")
	if mode != "folder" {
		mode = "file"
	}
	entries, err := os.ReadDir(absolute)
	if err != nil {
		http.Error(w, "directory is not readable", http.StatusForbidden)
		return
	}

	items := make([]browserEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		entryType := "file"
		if entry.IsDir() {
			entryType = "dir"
		} else {
			if mode == "folder" || !h.extensionAllowed(entry.Name()) {
				continue
			}
		}
		path := filepath.Join(absolute, entry.Name())
		items = append(items, browserEntry{
			Name:        entry.Name(),
			Path:        path,
			Type:        entryType,
			HasChildren: entry.IsDir() && h.hasVisibleChildren(path, mode),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Type != items[j].Type {
			return items[i].Type == "dir"
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})

	parent := filepath.Dir(absolute)
	if parent == absolute {
		parent = ""
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"current": absolute,
		"home":    h.home,
		"parent":  parent,
		"entries": items,
	})
}

func (h *SystemHandler) extensionAllowed(name string) bool {
	_, ok := h.allowedExtensions[strings.ToLower(filepath.Ext(name))]
	return ok
}

func (h *SystemHandler) hasVisibleChildren(path, mode string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if entry.IsDir() || (mode == "file" && h.extensionAllowed(entry.Name())) {
			return true
		}
	}
	return false
}

func isLoopbackRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
