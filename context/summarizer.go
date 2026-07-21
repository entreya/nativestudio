package context

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Config struct {
	YellowThreshold     float64
	RedThreshold        float64
	KeepRecentMessages  int
	SummarizeUsingModel string
	OllamaURL           string
}

func ShouldSummarize(pct float64, cfg Config) bool {
	return pct >= cfg.YellowThreshold
}

func ShouldBlock(pct float64, cfg Config) bool {
	return pct >= cfg.RedThreshold
}

func Summarize(session *Session, cfg Config) error {
	sessionID := session.ID

	// 1. Get current messages safely
	messages := Store.GetMessages(sessionID)

	if len(messages) <= cfg.KeepRecentMessages {
		return nil // Not enough messages to summarize
	}

	// 2. Split messages
	splitIdx := len(messages) - cfg.KeepRecentMessages
	oldMessages := messages[:splitIdx]
	recentMessages := messages[splitIdx:]

	tokensBefore := EstimateMessagesTokens(oldMessages)

	// 3. Build summarization prompt
	var conversation bytes.Buffer
	for _, m := range oldMessages {
		conversation.WriteString(fmt.Sprintf("%s: %s\n\n", m.Role, m.Content))
	}

	prompt := fmt.Sprintf(`You are a context compressor. Summarize this conversation history
preserving: key decisions made, file names, function names, code changes,
error patterns, user preferences about coding style.
Be concise but complete. Output only the summary, nothing else.

Conversation History:
%s`, conversation.String())

	// 4. Send to Ollama
	ollamaReq := map[string]interface{}{
		"model":  cfg.SummarizeUsingModel,
		"prompt": prompt,
		"stream": false,
	}

	reqBytes, err := json.Marshal(ollamaReq)
	if err != nil {
		return fmt.Errorf("failed to marshal summarization request: %w", err)
	}

	resp, err := http.Post(cfg.OllamaURL+"/api/generate", "application/json", bytes.NewReader(reqBytes))
	if err != nil {
		return fmt.Errorf("failed to call ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(body))
	}

	var ollamaResp struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return fmt.Errorf("failed to decode ollama response: %w", err)
	}

	summary := strings.TrimSpace(ollamaResp.Response)

	// 5. Build new messages array: [checkpoint, ...recentMessages]
	checkpointMsg := Message{
		Role:         "system",
		IsCheckpoint: true,
		Content:      summary,
		Timestamp:    time.Now(),
	}

	tokensAfter := EstimateTokens(summary)

	newMessages := []Message{checkpointMsg}
	newMessages = append(newMessages, recentMessages...)

	checkpointMeta := Checkpoint{
		CreatedAt:    time.Now(),
		TokensBefore: tokensBefore,
		TokensAfter:  tokensAfter,
		Summary:      summary,
	}

	// 6. Update store
	Store.UpdateMessagesAndCheckpoints(sessionID, newMessages, checkpointMeta)

	return nil
}
