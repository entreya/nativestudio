package context

import (
	"strings"
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

// GetModelContextWindow returns the known context window size for a model.
func GetModelContextWindow(model string) int {
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

// SupportsThinking returns true if the model supports Ollama's "think" mode.
func SupportsThinking(model string) bool {
	base := strings.SplitN(strings.ToLower(model), ":", 2)[0]
	return strings.HasPrefix(base, "qwen3") || strings.HasPrefix(base, "deepseek-r1")
}
