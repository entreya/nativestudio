package handlers

import (
	"encoding/json"
	"net/http"
)

// ModelsHandler handles querying Ollama models
type ModelsHandler struct {
	OllamaURL string
}

// NewModelsHandler creates a new ModelsHandler
func NewModelsHandler(ollamaURL string) *ModelsHandler {
	return &ModelsHandler{
		OllamaURL: ollamaURL,
	}
}

// RegisterRoutes registers the models endpoints
func (h *ModelsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/models", h.GetModels)
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
			"models": []string{},
			"error":  "ollama not running",
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

	var modelNames []string
	for _, m := range ollamaResp.Models {
		modelNames = append(modelNames, m.Name)
	}

	// Ensure it's not nil when returning JSON
	if modelNames == nil {
		modelNames = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"models": modelNames,
	})
}
