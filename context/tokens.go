package context

// EstimateTokens approximates tokens using 1 token ≈ 4 characters
func EstimateTokens(text string) int {
	return len(text) / 4
}

// EstimateMessagesTokens sums EstimateTokens over all message content
func EstimateMessagesTokens(messages []Message) int {
	var total int
	for _, m := range messages {
		total += EstimateTokens(m.Content)
	}
	return total
}

// GetModelContextWindow returns known limits
func GetModelContextWindow(model string) int {
	switch model {
	case "codellama":
		return 4096
	case "llama3":
		return 8192
	case "deepseek-coder":
		return 16384
	default:
		return 4096
	}
}
