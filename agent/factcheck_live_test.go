package agent

import (
	"context"
	"os"
	"testing"
)

// TestLiveMultiEngineSearch hits the real search engines. Skipped by default
// so the normal suite stays offline and deterministic; run it with
// NATIVESTUDIO_LIVE_SEARCH=1 to verify the built-in engines still work when
// one of them inevitably changes its feed format or starts blocking.
func TestLiveMultiEngineSearch(t *testing.T) {
	if os.Getenv("NATIVESTUDIO_LIVE_SEARCH") == "" {
		t.Skip("set NATIVESTUDIO_LIVE_SEARCH=1 to run the live search check")
	}

	check := searchAllEngines(context.Background(), "latest PHP release", 5)

	t.Logf("engines queried: %v", check.EnginesQueried)
	t.Logf("engines failed:  %v", check.EnginesFailed)
	t.Logf("results:         %d", len(check.Results))
	t.Logf("independent:     %v", check.IndependentDomains)
	t.Logf("confidence:      %s — %s", check.Confidence, check.Reason)

	for _, result := range check.Results {
		t.Logf("  [%s/%s independent=%v] %s", result.EngineName, result.Domain, result.Independent, result.Title)
	}

	if len(check.EnginesQueried) == 0 {
		t.Fatal("no engine responded at all — every built-in default is broken")
	}
	if len(check.Results) == 0 {
		t.Fatal("engines responded but produced zero usable results")
	}
}
