package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"
)

type SettingsHandler struct {
	settingsPath string
	mu           sync.RWMutex
}

func NewSettingsHandler(settingsPath string) *SettingsHandler {
	return &SettingsHandler{settingsPath: settingsPath}
}

func (h *SettingsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings", h.GetSettings)
	mux.HandleFunc("PUT /api/settings", h.UpdateSettings)
}

func (h *SettingsHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	data, err := os.ReadFile(h.settingsPath)
	h.mu.RUnlock()

	if err != nil {
		if os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{}`))
			return
		}
		http.Error(w, "Could not read settings", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func (h *SettingsHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		http.Error(w, "Failed to encode settings", http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(h.settingsPath, data, 0644); err != nil {
		http.Error(w, "Failed to write settings", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
