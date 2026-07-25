package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/entreya/nativestudio/agent"
)

// ModelsHandler handles querying Ollama models
type ModelsHandler struct {
	OllamaURL    string
	DefaultModel string
}

// NewModelsHandler creates a new ModelsHandler
func NewModelsHandler(ollamaURL, defaultModel string) *ModelsHandler {
	return &ModelsHandler{
		OllamaURL:    ollamaURL,
		DefaultModel: defaultModel,
	}
}

// RegisterRoutes registers the models endpoints
func (h *ModelsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/models", h.GetModels)
	mux.HandleFunc("POST /api/models/unload", h.UnloadModels)
}

// ModelInfo is the per-model descriptor returned to the frontend.
// thinking_capable is derived from the same backend function used by the agent
// loop — one source of truth, no duplicated regex on the client side.
type ModelInfo struct {
	Name            string `json:"name"`
	ThinkingCapable bool   `json:"thinking_capable"`
}

// GetModels handles GET /api/models
func (h *ModelsHandler) GetModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp, err := http.Get(h.OllamaURL + "/api/tags")
	if err != nil {
		// Ollama is unreachable
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"models":        []ModelInfo{},
			"default_model": h.DefaultModel,
			"error":         "ollama not running",
		})
		return
	}
	defer resp.Body.Close()

	var ollamaResp struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		http.Error(w, "failed to decode ollama response", http.StatusInternalServerError)
		return
	}

	models := make([]ModelInfo, 0, len(ollamaResp.Models))
	for _, m := range ollamaResp.Models {
		models = append(models, ModelInfo{
			Name:            m.Name,
			ThinkingCapable: agent.SupportsNativeThinking(m.Name),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"models":        models,
		"default_model": h.DefaultModel,
	})
}

// UnloadModels frees the memory held by every model Ollama currently has
// resident, by sending keep_alive:0 (Ollama's documented way to force an
// immediate unload instead of waiting out the normal idle timeout). Manual
// counterpart to the short default keep_alive set on every request — for
// when the user wants memory back right now rather than waiting ~2 minutes.
func (h *ModelsHandler) UnloadModels(w http.ResponseWriter, r *http.Request) {
	resp, err := http.Get(h.OllamaURL + "/api/ps")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "ollama not running"})
		return
	}
	defer resp.Body.Close()

	var loaded struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&loaded); err != nil {
		http.Error(w, "failed to decode ollama response", http.StatusInternalServerError)
		return
	}

	unloaded := make([]string, 0, len(loaded.Models))
	for _, m := range loaded.Models {
		body, _ := json.Marshal(map[string]any{"model": m.Name, "keep_alive": 0})
		req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.OllamaURL+"/api/generate", bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		if unloadResp, err := http.DefaultClient.Do(req); err == nil {
			unloadResp.Body.Close()
			unloaded = append(unloaded, m.Name)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "unloaded": unloaded})
}
