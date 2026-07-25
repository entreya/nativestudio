package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// rephraserModel is a tiny, fast, low-memory model dedicated to one job:
// turning a user's raw message (which may be short, mixed-language, or
// typo-laden) into one clear, unambiguous instruction before it ever reaches
// the real coding model. A clearer instruction means less time for the
// bigger model to spend just figuring out what was asked, on top of
// actually answering it.
//
// qwen2.5:0.5b and qwen2.5:1.5b were tried first and both actively
// hallucinated on Hinglish/romanized-Hindi input (e.g. turning "SiteController
// mein hello action hata do" into a plain greeting, or inventing a fake
// framework name out of a typo'd Hindi word) — confidently wrong, which is
// worse than not rephrasing at all. qwen3.5:0.8b, tested against the same
// prompts, correctly parsed the Hindi/English mixing while staying small.
const rephraserModel = "qwen3.5:0.8b"

// rephraserTimeout bounds the rephrase call itself — this is meant to be a
// near-instant preprocessing step, not something that can stall a request.
// A failure or timeout here always falls back to the original prompt
// unchanged; it never blocks or fails the real request.
const rephraserTimeout = 8 * time.Second

// rephraserKeepAlive is deliberately much longer than the main model's
// ollamaKeepAlive (agent/loop.go) — at ~0.4GB resident this tiny model is
// cheap to keep warm for the whole session, and reloading it fresh on every
// message would defeat the point of it being "instant".
const rephraserKeepAlive = "30m"

// rephraserClient is deliberately its own http.Client rather than
// http.DefaultClient — every existing agent test mocks the main model's
// calls by swapping http.DefaultClient.Transport (scriptedTransport in
// agent/agent_test.go), expecting a fixed sequence of responses. Sharing
// that client meant the rephrase call silently consumed the first scripted
// response before the real step loop ever ran, desyncing every response
// after it (confirmed live: TestAgentDoesNotRetryWhenToolAlreadyCalled broke
// this exact way when rephrasePrompt was first wired in). A dedicated
// client keeps the rephraser's own network calls independent of that
// mocking, and a real DNS failure against a test's fake Ollama host still
// falls back to the original prompt exactly as any other rephraser failure
// does — so it costs nothing in tests and isolates the two concerns.
var rephraserClient = &http.Client{Timeout: rephraserTimeout}

const rephraserSystemPrompt = `Rewrite the user's message into a single, clear, precise instruction for a coding assistant. You are NOT answering the message and NOT completing the request — you are only rewriting how it is phrased. If the message asks to explain, describe, review, or analyze something, output a rewritten REQUEST for that explanation/description/review — never write the explanation, description, or review itself, and never invent what the code supposedly does or contains; you have not seen it. Preserve every technical detail, file name, symbol name, and specific requirement exactly — never drop, add, or invent anything. Do not invent details that aren't in the original message — no guessed programming language, no invented folder/file structure, no assumed constraints; if something is genuinely unstated, leave it unstated rather than filling the gap with a plausible-sounding guess. Fix unclear phrasing, mixed languages, or typos so it reads as one direct, unambiguous sentence in English. Output ONLY the rewritten instruction — no preamble, no quotes, no explanation, no extra commentary.`

// rephrasePrompt asks the tiny rephraser model to normalize prompt into a
// single clear instruction. It never returns an error the caller needs to
// treat as fatal — any failure (model unavailable, timeout, an implausible
// result) should just mean "use the original prompt", which is why this
// returns (original, err) semantics via a plain error the caller checks
// rather than panicking or retrying.
func rephrasePrompt(ctx context.Context, ollamaURL, prompt string) (string, error) {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return "", fmt.Errorf("empty prompt")
	}

	reqCtx, cancel := context.WithTimeout(ctx, rephraserTimeout)
	defer cancel()

	body := map[string]any{
		"model":      rephraserModel,
		"stream":     false,
		"keep_alive": rephraserKeepAlive,
		// qwen3.5 is a native-thinking model; without this it can burn its
		// entire token budget reasoning about the rewrite and never actually
		// produce one (observed live: done_reason "length" with an empty
		// content field). This task needs a fast, direct rewrite, not a
		// visible reasoning trace.
		"think": false,
		"messages": []map[string]string{
			{"role": "system", "content": rephraserSystemPrompt},
			{"role": "user", "content": trimmed},
		},
		"options": map[string]any{"temperature": 0.1, "num_predict": 200},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, ollamaURL+"/api/chat", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := rephraserClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("rephraser unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("rephraser returned status %d", resp.StatusCode)
	}

	var parsed struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", fmt.Errorf("decode rephraser response: %w", err)
	}

	rephrased := strings.Trim(strings.TrimSpace(parsed.Message.Content), "\"'")
	if !plausibleRephrase(trimmed, rephrased) {
		return "", fmt.Errorf("implausible rephrase result, discarding")
	}
	return rephrased, nil
}

// plausibleRephrase is the safety net for when rephraserModel goes off the
// rails despite the system prompt's explicit instructions — live-tested,
// this happens specifically on explain/describe-style requests, where it
// answers instead of rephrasing (a fabricated multi-sentence description of
// code it never saw, in place of a one-line rewritten request). That
// failure mode is caught upstream by skipping the call entirely for such
// prompts (promptRequestsExplanation in agent.go), but this length check
// stays as a second line of defense for anything else that slips through:
// an implausibly long "rewrite" relative to the original is a reliable
// signal of rambling/invention, not a faithful one-sentence rephrase.
func plausibleRephrase(original, rephrased string) bool {
	if rephrased == "" {
		return false
	}
	return len(rephrased) <= len(original)*4+40
}
