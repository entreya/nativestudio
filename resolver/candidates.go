package resolver

import (
	"sort"
	"strings"

	"github.com/entreya/nativestudio/db"
	"github.com/entreya/nativestudio/editor"
)

// Candidate is a resolved file/symbol that may be the target of the user's prompt.
type Candidate struct {
	Path       string         `json:"path"`
	Confidence string         `json:"confidence"`
	Symbol     *editor.Symbol `json:"symbol,omitempty"`
	Score      float64        `json:"score"`
	Reasons    []string       `json:"reasons"`
}

// ScoreCandidate calculates a relevance score for a candidate given the editor
// state and recent conversation history. Scores are additive; confidence labels
// are derived from the final value.
func ScoreCandidate(c *Candidate, state editor.EditorState, history []db.Message) float64 {
	score := c.Score // start from the rule-assigned base score

	// Active file bonus
	if c.Path != "" && c.Path == state.ActiveFile {
		score += 0.30
		c.Reasons = appendUnique(c.Reasons, "active_file")
	}

	// Open file bonus
	for _, f := range state.OpenFiles {
		if f == c.Path {
			score += 0.20
			c.Reasons = appendUnique(c.Reasons, "open_file")
			break
		}
	}

	// Recent file bonus
	for _, f := range state.RecentFiles {
		if f == c.Path {
			score += 0.15
			c.Reasons = appendUnique(c.Reasons, "recent_file")
			break
		}
	}

	// Recent edit bonus
	for _, e := range state.RecentEdits {
		if e.File == c.Path {
			score += 0.10
			c.Reasons = appendUnique(c.Reasons, "recent_edit")
			break
		}
	}

	// Mentioned in last 3 messages bonus
	last3 := history
	if len(last3) > 3 {
		last3 = last3[len(last3)-3:]
	}
	baseName := c.Path
	if idx := strings.LastIndex(baseName, "/"); idx >= 0 {
		baseName = baseName[idx+1:]
	}
	for _, m := range last3 {
		if strings.Contains(m.Content, baseName) || strings.Contains(m.Content, c.Path) {
			score += 0.05
			c.Reasons = appendUnique(c.Reasons, "mentioned_in_history")
			break
		}
	}

	c.Score = score

	// Assign confidence label
	switch {
	case score >= 0.85:
		c.Confidence = "high"
	case score >= 0.60:
		c.Confidence = "medium"
	default:
		c.Confidence = "low"
	}

	return score
}

// SortCandidates sorts candidates by score descending in-place.
func SortCandidates(candidates []Candidate) {
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})
}

// appendUnique appends s to slice only if not already present.
func appendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}
