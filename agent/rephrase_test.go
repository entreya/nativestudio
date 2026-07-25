package agent

import (
	"context"
	"testing"
)

// TestPlausibleRephraseCatchesLiveFabrication reproduces the exact live
// failure: asked to rephrase "explain this function in UserController.php"
// (45 chars), rephraserModel instead fabricated a multi-sentence description
// of the file's supposed contents (520 chars) — a confident, wrong answer
// to a question it was never asked, dressed up as a "rephrase". This is the
// second line of defense behind promptRequestsExplanation (which should
// already skip the call for this exact prompt shape) for anything else that
// produces an implausibly long result.
func TestPlausibleRephraseCatchesLiveFabrication(t *testing.T) {
	original := "explain this function in UserController.php"
	fabricated := "The `UserController` class is responsible for handling user authentication and session management within your application by validating credentials against a database or external service to establish an authenticated state; it also manages the lifecycle of sessions using cookies, ensuring that users remain logged in only during their active time periods."
	if plausibleRephrase(original, fabricated) {
		t.Fatal("expected the fabricated multi-sentence description to be rejected as implausible")
	}

	faithful := "Remove all occurrences of \"hello\" from SiteController's code."
	if !plausibleRephrase("SiteController mein hello action hata do", faithful) {
		t.Fatal("expected a faithful, similarly-sized rephrase to be accepted")
	}
}

func TestRephrasePromptRejectsEmptyInput(t *testing.T) {
	if _, err := rephrasePrompt(context.Background(), "http://ollama.test", "   "); err == nil {
		t.Fatal("expected an error for an empty/whitespace-only prompt")
	}
}

// TestRephrasePromptFallsBackWhenUnreachable confirms the caller-facing
// contract: any failure here (model unavailable, timeout, bad response) must
// surface as a plain error rather than panicking, so Agent.Run's fallback to
// the original prompt always has something to fall back to.
func TestRephrasePromptFallsBackWhenUnreachable(t *testing.T) {
	// No Ollama server is actually running at this address in the test
	// environment, so this exercises the real network-failure path.
	if _, err := rephrasePrompt(context.Background(), "http://127.0.0.1:1", "explain this"); err == nil {
		t.Fatal("expected an error when the rephraser endpoint is unreachable")
	}
}
