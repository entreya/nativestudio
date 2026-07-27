package agent

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
)

// SearchEngine describes one configurable source the agent can query. Engines
// are data, not code, so the user can add their own from Settings without a
// rebuild — probe_search_engine (agent/probe.go) works out these fields for a
// candidate URL and shows the result before anything is saved.
type SearchEngine struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// Builtin engines ship with the app and can be disabled but not deleted,
	// so a bad edit can never leave the agent with no way to search at all.
	Builtin bool `json:"builtin"`
	// Kind selects the response parser: "rss" or "json".
	Kind string `json:"kind"`
	// URLTemplate contains {query}, replaced with the URL-escaped search term.
	URLTemplate string `json:"url_template"`
	// Category is informational ("web", "news", "reference") and is also used
	// to weight corroboration: a reference source agreeing with a news source
	// is stronger evidence than two news aggregators repeating one wire story.
	Category string `json:"category"`

	// JSON extraction (Kind == "json"). ResultsPath is a dotted path to the
	// array of results; the *Field values are keys within each element.
	ResultsPath string `json:"results_path,omitempty"`
	TitleField  string `json:"title_field,omitempty"`
	URLField    string `json:"url_field,omitempty"`
	DescField   string `json:"desc_field,omitempty"`
	// URLPrefix builds a link when the API returns titles but no URLs
	// (Wikipedia's search API does exactly this).
	URLPrefix string `json:"url_prefix,omitempty"`

	// DerivesFrom names another source this engine republishes rather than
	// independently reporting. DuckDuckGo's Instant Answer abstracts are
	// mostly Wikipedia text, so counting DDG and Wikipedia as two agreeing
	// sources would be false corroboration — crossCheck collapses them.
	DerivesFrom string `json:"derives_from,omitempty"`
}

// builtinSearchEngines are the defaults, each verified to actually return
// results before being included (Mojeek's RSS endpoint was tested and
// dropped: it answers 200 but returns zero items).
func builtinSearchEngines() []SearchEngine {
	return []SearchEngine{
		{
			ID: "bing", Name: "Bing", Enabled: true, Builtin: true, Kind: "rss", Category: "web",
			URLTemplate: "https://www.bing.com/search?format=rss&q={query}",
		},
		{
			ID: "google-news", Name: "Google News", Enabled: true, Builtin: true, Kind: "rss", Category: "news",
			URLTemplate: "https://news.google.com/rss/search?q={query}&hl=en&gl=US&ceid=US:en",
		},
		{
			ID: "wikipedia", Name: "Wikipedia", Enabled: true, Builtin: true, Kind: "json", Category: "reference",
			URLTemplate: "https://en.wikipedia.org/w/api.php?action=query&list=search&srsearch={query}&format=json&srlimit=5",
			ResultsPath: "query.search", TitleField: "title", DescField: "snippet",
			URLPrefix: "https://en.wikipedia.org/wiki/",
		},
		{
			ID: "duckduckgo", Name: "DuckDuckGo Instant Answer", Enabled: true, Builtin: true, Kind: "json", Category: "reference",
			URLTemplate: "https://api.duckduckgo.com/?q={query}&format=json&no_html=1",
			ResultsPath: "RelatedTopics", TitleField: "Text", URLField: "FirstURL", DescField: "Text",
			DerivesFrom: "wikipedia",
		},
	}
}

type storedSearchEngines struct {
	SearchEngines []SearchEngine `json:"searchEngines"`
}

// loadSearchEngines merges user-configured engines from the settings file over
// the builtin defaults. Reads fresh each call (the file is tiny) so a change
// in Settings takes effect on the next search with no restart, matching how
// currentMaxThinkingTokens already behaves.
// LoadSearchEngines is the exported entry point for the Settings UI to list
// every configured engine (builtin and custom, enabled or not) — the agent's
// own search path uses enabledSearchEngines internally, which is this same
// data filtered down to what should actually be queried.
func LoadSearchEngines() []SearchEngine { return loadSearchEngines() }

func loadSearchEngines() []SearchEngine {
	engines := builtinSearchEngines()

	data, err := os.ReadFile(settingsFilePath)
	if err != nil {
		return engines
	}
	var parsed storedSearchEngines
	if err := json.Unmarshal(data, &parsed); err != nil {
		return engines
	}

	byID := make(map[string]int, len(engines))
	for i, engine := range engines {
		byID[engine.ID] = i
	}
	for _, stored := range parsed.SearchEngines {
		if stored.ID == "" {
			continue
		}
		if index, exists := byID[stored.ID]; exists {
			// A builtin may only be enabled/disabled and renamed from
			// settings — its URL and parser stay fixed so a malformed saved
			// value can't silently break a known-good default.
			engines[index].Enabled = stored.Enabled
			if stored.Name != "" {
				engines[index].Name = stored.Name
			}
			continue
		}
		if stored.URLTemplate == "" || !strings.Contains(stored.URLTemplate, "{query}") {
			continue
		}
		stored.Builtin = false
		if stored.Kind != "rss" && stored.Kind != "json" {
			stored.Kind = "rss"
		}
		engines = append(engines, stored)
	}
	return engines
}

// enabledSearchEngines returns only the engines that should actually be
// queried, preserving order so results stay deterministic.
func enabledSearchEngines() []SearchEngine {
	var enabled []SearchEngine
	for _, engine := range loadSearchEngines() {
		if engine.Enabled {
			enabled = append(enabled, engine)
		}
	}
	return enabled
}

// buildSearchURL substitutes the escaped query into an engine's template.
func buildSearchURL(template, query string) string {
	return strings.ReplaceAll(template, "{query}", url.QueryEscape(query))
}

// sourceDomain returns the registrable-looking host for a result URL, used to
// judge whether two results are genuinely independent. Falls back to the
// engine ID when a link is missing or unparseable so an anonymous result
// never accidentally counts as agreeing with everything else.
func sourceDomain(rawURL, fallback string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return fallback
	}
	host := strings.ToLower(parsed.Host)
	host = strings.TrimPrefix(host, "www.")
	return host
}
