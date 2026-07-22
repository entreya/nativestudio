package context

import (
	"bytes"
	stdcontext "context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// EstimateTokens is a rough heuristic: ~4 characters per token for English text/code.
func EstimateTokens(text string) int {
	return len(text) / 4
}

// EstimateMessagesTokens sums token estimates across a slice of messages.
func EstimateMessagesTokens(messages []Message) int {
	var total int
	for _, m := range messages {
		total += EstimateTokens(m.Content)
	}
	return total
}

type cachedContextWindow struct {
	tokens    int
	checkedAt time.Time
}

// modelContextWindows caches each model's real context length (keyed by full
// name including tag), populated by RefreshModelContextWindow. Until a model
// has been looked up, GetModelContextWindow falls back to the name-prefix
// heuristic below.
var modelContextWindows sync.Map // model string -> cachedContextWindow

// contextWindowRetryInterval bounds how often a failed Ollama lookup is
// retried, so a temporarily unreachable Ollama doesn't add latency to every
// single chat request.
const contextWindowRetryInterval = 60 * time.Second

// GetModelContextWindow returns the context window size for a model: the real
// value from Ollama if RefreshModelContextWindow has successfully looked it
// up, otherwise a best-effort guess from well-known name prefixes.
func GetModelContextWindow(model string) int {
	if cached, ok := modelContextWindows.Load(model); ok {
		if entry := cached.(cachedContextWindow); entry.tokens > 0 {
			return entry.tokens
		}
	}
	return knownContextWindow(model)
}

// knownContextWindow is a heuristic fallback for models Ollama hasn't been
// asked about yet (or that it can't be reached for): guess from well-known
// name prefixes. Custom/fine-tuned model names that don't match any of these
// (e.g. a locally tagged "mymodel:max") silently fall through to a
// conservative 4096, which is why RefreshModelContextWindow exists — asking
// Ollama directly is the only way to get this right in general.
func knownContextWindow(model string) int {
	base := strings.SplitN(strings.ToLower(model), ":", 2)[0]
	switch {
	case strings.HasPrefix(base, "qwen2.5-coder"):
		return 32768
	case strings.HasPrefix(base, "qwen2.5"):
		return 32768
	case strings.HasPrefix(base, "qwen3"):
		return 32768
	case strings.HasPrefix(base, "phi4"):
		return 16384
	case strings.HasPrefix(base, "phi3"):
		return 4096
	case strings.HasPrefix(base, "deepseek-coder"):
		return 16384
	case strings.HasPrefix(base, "deepseek-r1"):
		return 65536
	case strings.HasPrefix(base, "llama3.1"), strings.HasPrefix(base, "llama3.2"):
		return 131072
	case strings.HasPrefix(base, "llama3"):
		return 8192
	case strings.HasPrefix(base, "codellama"):
		return 4096
	case strings.HasPrefix(base, "gemma3"):
		return 131072
	case strings.HasPrefix(base, "gemma2"):
		return 8192
	case strings.HasPrefix(base, "mistral"):
		return 32768
	default:
		return 4096
	}
}

// RefreshModelContextWindow asks Ollama for a model's actual context length
// via /api/show and caches it so GetModelContextWindow stops guessing from
// the model's name — important for custom or fine-tuned models (e.g. a local
// "myname:max" tag) that the name-prefix heuristic has never heard of and
// would otherwise silently cap at 4096 tokens. Safe to call on every request:
// once a model has a cached result (success or a recent failure) this returns
// immediately without touching the network.
func RefreshModelContextWindow(ollamaURL, model string) {
	if model == "" {
		return
	}
	if cached, ok := modelContextWindows.Load(model); ok {
		entry := cached.(cachedContextWindow)
		if entry.tokens > 0 || time.Since(entry.checkedAt) < contextWindowRetryInterval {
			return
		}
	}
	tokens := fetchContextWindow(ollamaURL, model)
	modelContextWindows.Store(model, cachedContextWindow{tokens: tokens, checkedAt: time.Now()})
}

func fetchContextWindow(ollamaURL, model string) int {
	reqCtx, cancel := stdcontext.WithTimeout(stdcontext.Background(), 2*time.Second)
	defer cancel()

	// "model" is the current field name; "name" is the deprecated alias older
	// Ollama builds still expect — sending both is harmless either way.
	body, err := json.Marshal(map[string]string{"model": model, "name": model})
	if err != nil {
		return 0
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, ollamaURL+"/api/show", bytes.NewReader(body))
	if err != nil {
		return 0
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0
	}

	var parsed struct {
		ModelInfo map[string]any `json:"model_info"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return 0
	}
	// The key is architecture-dependent (e.g. "qwen2.context_length",
	// "llama.context_length"), so scan for whichever one is present rather
	// than guessing the architecture name.
	for key, value := range parsed.ModelInfo {
		if !strings.HasSuffix(key, ".context_length") {
			continue
		}
		if length, ok := value.(float64); ok && length > 0 {
			return int(length)
		}
	}
	return 0
}

// SupportsThinking returns true if the model supports Ollama's "think" mode.
func SupportsThinking(model string) bool {
	base := strings.SplitN(strings.ToLower(model), ":", 2)[0]
	return strings.HasPrefix(base, "qwen3") || strings.HasPrefix(base, "deepseek-r1")
}
