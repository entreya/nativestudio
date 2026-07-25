package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// withSettingsFile points settingsFilePath at a temp file for the duration
// of the test and restores the previous value after, since it's a shared
// package-level var read by both currentMaxThinkingTokens and
// currentForceUnloadThreshold.
func withSettingsFile(t *testing.T, contents string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	if contents != "" {
		if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	previous := settingsFilePath
	settingsFilePath = path
	t.Cleanup(func() { settingsFilePath = previous })
}

func TestCurrentMaxThinkingTokensFallsBackWhenUnset(t *testing.T) {
	withSettingsFile(t, "")
	if got := currentMaxThinkingTokens(); got != defaultMaxThinkingTokens {
		t.Fatalf("expected default %d, got %d", defaultMaxThinkingTokens, got)
	}
}

func TestCurrentMaxThinkingTokensReadsOverride(t *testing.T) {
	withSettingsFile(t, `{"agentSettings":{"maxThinkingTokens":500}}`)
	if got := currentMaxThinkingTokens(); got != 500 {
		t.Fatalf("expected 500, got %d", got)
	}
}

func TestCurrentMaxThinkingTokensRejectsOutOfRange(t *testing.T) {
	withSettingsFile(t, `{"agentSettings":{"maxThinkingTokens":5}}`)
	if got := currentMaxThinkingTokens(); got != defaultMaxThinkingTokens {
		t.Fatalf("expected fallback to default for an out-of-range value, got %d", got)
	}
}

func TestNextChatKeepAliveForcesUnloadAtThreshold(t *testing.T) {
	withSettingsFile(t, `{"agentSettings":{"forceUnloadAfterChats":3}}`)
	chatRequestCount.Store(0)

	results := make([]any, 4)
	for i := range results {
		results[i] = nextChatKeepAlive()
	}
	if results[0] != ollamaKeepAlive || results[1] != ollamaKeepAlive {
		t.Fatalf("expected the first two calls to use the normal keep_alive, got %v", results[:2])
	}
	if results[2] != 0 {
		t.Fatalf("expected the 3rd call to force an unload (0), got %v", results[2])
	}
	if results[3] != ollamaKeepAlive {
		t.Fatalf("expected the counter to reset after forcing an unload, got %v", results[3])
	}
}

func TestNextChatKeepAliveDisabledByZeroThreshold(t *testing.T) {
	withSettingsFile(t, `{"agentSettings":{"forceUnloadAfterChats":0}}`)
	chatRequestCount.Store(0)
	for i := 0; i < 500; i++ {
		if got := nextChatKeepAlive(); got != ollamaKeepAlive {
			t.Fatalf("expected keep_alive to never be forced to 0 when disabled, got %v at iteration %d", got, i)
		}
	}
}
