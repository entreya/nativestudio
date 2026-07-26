package agent

import (
	"encoding/json"
	"os"
	"sync/atomic"
)

// Bounds on the user-configurable thinking budget (Settings page). Below
// minMaxThinkingTokens the model would get cut off before it could do any
// real reasoning at all; above maxMaxThinkingTokensCap defeats the point of
// having a cap. Anything outside this range, or a missing/unreadable
// settings file, falls back to defaultMaxThinkingTokens.
const (
	minMaxThinkingTokens    = 100
	maxMaxThinkingTokensCap = 20000
)

// Bounds for the Settings-page "Response Creativity" control, which maps
// directly onto Ollama's sampling temperature. 0 always picks the
// highest-probability token (deterministic, repeats itself on retries); 1 is
// as far as the slider goes before output stops reading as coherent code or
// prose for most local models. defaultResponseTemperature (0.7) matches what
// this app used implicitly before the field existed — omitting "temperature"
// from the request left each model's own Modelfile default in effect, which
// is 0.7-0.8 for the models this app ships with.
const (
	minResponseTemperature     = 0.0
	maxResponseTemperature     = 1.0
	defaultResponseTemperature = 0.7
)

// settingsFilePath points at the same JSON file handlers.SettingsHandler
// reads and writes (main.go wires this via SetSettingsPath at startup) —
// this package only ever reads it, never writes it.
var settingsFilePath = "data/.nativestudio_settings.json"

// SetSettingsPath overrides where currentMaxThinkingTokens looks for the
// live Settings-page values. Called once from main.go with the same path
// passed to handlers.NewSettingsHandler.
func SetSettingsPath(path string) {
	settingsFilePath = path
}

type storedSettings struct {
	AgentSettings struct {
		MaxThinkingTokens     int      `json:"maxThinkingTokens"`
		ForceUnloadAfterChats int      `json:"forceUnloadAfterChats"`
		// Pointer, not float64: 0 is a legitimate, meaningfully different
		// choice (fully deterministic output) from the field being absent
		// from an older settings file, and a plain zero-value check can't
		// tell those two cases apart.
		Temperature *float64 `json:"temperature"`
	} `json:"agentSettings"`
}

// defaultForceUnloadAfterChats periodically forces Ollama to unload the
// model after this many /api/chat requests, even under continuous back-to-
// back use — a plain idle-based keep_alive (agent/loop.go's ollamaKeepAlive)
// never fires if requests keep resetting Ollama's own timer first, which is
// exactly the sustained-memory-pressure scenario keep_alive was shortened
// for in the first place. 0 disables this and relies on keep_alive alone.
const defaultForceUnloadAfterChats = 100

// chatRequestCount is process-wide (resets on server restart), not
// per-session — the memory pressure this guards against comes from how long
// a model has been resident regardless of which conversation asked for it.
var chatRequestCount atomic.Int64

// currentForceUnloadThreshold reads the live Settings-page value for how
// many chat requests may run before the model is forced to unload
// afterward, falling back to defaultForceUnloadAfterChats when unset,
// invalid, or the settings file can't be read.
func currentForceUnloadThreshold() int {
	data, err := os.ReadFile(settingsFilePath)
	if err != nil {
		return defaultForceUnloadAfterChats
	}
	var parsed storedSettings
	if err := json.Unmarshal(data, &parsed); err != nil {
		return defaultForceUnloadAfterChats
	}
	if parsed.AgentSettings.ForceUnloadAfterChats < 0 {
		return defaultForceUnloadAfterChats
	}
	return parsed.AgentSettings.ForceUnloadAfterChats
}

// nextChatKeepAlive returns the keep_alive value for the next /api/chat
// request: ollamaKeepAlive normally, or the integer 0 (Ollama's documented
// "unload immediately after this response" value, same as the manual "Free
// up memory" button in handlers/models.go) every currentForceUnloadThreshold()
// requests, as a periodic memory-safety reset. A threshold of 0 disables the
// reset and always returns ollamaKeepAlive. Returns `any` because the two
// cases are different JSON types (duration string vs. integer), both valid
// for this field in Ollama's API.
func nextChatKeepAlive() any {
	threshold := currentForceUnloadThreshold()
	if threshold <= 0 {
		return ollamaKeepAlive
	}
	count := chatRequestCount.Add(1)
	if count%int64(threshold) == 0 {
		return 0
	}
	return ollamaKeepAlive
}

// currentMaxThinkingTokens reads the live Settings-page value for how many
// thinking tokens a single generation step may spend before Step cuts it
// off, falling back to defaultMaxThinkingTokens when unset, invalid, or the
// settings file can't be read. Read fresh on every call (the file is tiny)
// rather than cached, so a change in the Settings UI takes effect on the
// very next agent step with no server restart.
func currentMaxThinkingTokens() int {
	data, err := os.ReadFile(settingsFilePath)
	if err != nil {
		return defaultMaxThinkingTokens
	}
	var parsed storedSettings
	if err := json.Unmarshal(data, &parsed); err != nil {
		return defaultMaxThinkingTokens
	}
	value := parsed.AgentSettings.MaxThinkingTokens
	if value < minMaxThinkingTokens || value > maxMaxThinkingTokensCap {
		return defaultMaxThinkingTokens
	}
	return value
}

// currentResponseTemperature reads the live Settings-page "Response
// Creativity" value, falling back to defaultResponseTemperature when unset,
// out of range, or the settings file can't be read. Read fresh on every
// call, same as currentMaxThinkingTokens, so a change takes effect on the
// very next request with no server restart.
func currentResponseTemperature() float64 {
	data, err := os.ReadFile(settingsFilePath)
	if err != nil {
		return defaultResponseTemperature
	}
	var parsed storedSettings
	if err := json.Unmarshal(data, &parsed); err != nil {
		return defaultResponseTemperature
	}
	if parsed.AgentSettings.Temperature == nil {
		return defaultResponseTemperature
	}
	value := *parsed.AgentSettings.Temperature
	if value < minResponseTemperature || value > maxResponseTemperature {
		return defaultResponseTemperature
	}
	return value
}
