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
	tokensAfter := EstimateTokens(summary)

	checkpointMeta := Checkpoint{
		CreatedAt:    time.Now(),
		TokensBefore: tokensBefore,
		TokensAfter:  tokensAfter,
		Summary:      summary,
	}

	// Everything at or after keepFromSeq is left untouched, so any message a
	// concurrent chat turn appends while this summarization is in flight is
	// never lost — it simply lands after the threshold. See ReplaceWithCheckpoint.
	var keepFromSeq int
	switch {
	case len(recentMessages) > 0:
		keepFromSeq = recentMessages[0].Sequence
	case len(oldMessages) > 0:
		keepFromSeq = oldMessages[len(oldMessages)-1].Sequence + 1
	default:
		return nil
	}

	// 5. Collapse messages before keepFromSeq into a single checkpoint message.
	if err := Store.ReplaceWithCheckpoint(sessionID, keepFromSeq, checkpointMeta); err != nil {
		return fmt.Errorf("failed to save checkpoint: %w", err)
	}

	return nil
}
