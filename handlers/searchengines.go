package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/entreya/nativestudio/agent"
)

// SearchEnginesHandler exposes the agent's configurable search engines to
// Settings: listing what's currently configured, and the AI-assisted probe
// flow that tests a candidate engine before it's ever saved. Saving itself
// reuses the existing generic GET/PUT /api/settings — the frontend fetches
// current settings, adds the confirmed engine to the searchEngines array,
// and PUTs the whole blob back, the same way every other Settings section
// already works; this handler only needs to cover listing and probing.
type SearchEnginesHandler struct {
	ollamaURL    string
	defaultModel string
}

func NewSearchEnginesHandler(ollamaURL, defaultModel string) *SearchEnginesHandler {
	return &SearchEnginesHandler{ollamaURL: ollamaURL, defaultModel: defaultModel}
}

func (h *SearchEnginesHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/search-engines", h.ListEngines)
	mux.HandleFunc("POST /api/search-engines/probe", h.ProbeEngine)
}

func (h *SearchEnginesHandler) ListEngines(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, agent.LoadSearchEngines())
}

func (h *SearchEnginesHandler) ProbeEngine(w http.ResponseWriter, r *http.Request) {
	var req agent.ProbeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	result := agent.ProbeSearchEngine(r.Context(), h.ollamaURL, h.defaultModel, req)
	writeJSON(w, result)
}
