package agent

import (
	"context"
	"github.com/entreya/nativestudio/db"
	"log"
	"sort"
	"strings"
)

// factSubjectKey turns a question into a stable lookup key so differently
// worded questions about the same thing resolve to the same stored fact:
// "what is the latest PHP release" and "latest release of PHP?" both reduce
// to "php". Built from core tokens (subject-bearing words only), sorted so
// word order doesn't matter.
func factSubjectKey(question string) string {
	tokens := coreSearchTokens(question)
	if len(tokens) == 0 {
		// Fall back to the significant tokens rather than returning an empty
		// key — an empty key would collide across every unrelated question.
		tokens = significantSearchTokens(question)
	}
	if len(tokens) == 0 {
		return ""
	}
	sorted := append([]string(nil), tokens...)
	sort.Strings(sorted)
	return strings.Join(sorted, " ")
}

// verificationConfirmedPhrase is the option text offered by the escalation
// follow-up. The frontend sends a chosen option back as an ordinary user
// message ("Regarding your question ..., my answer is: <option>"), so this is
// how the confirmation is recognised on the way back in.
const verificationConfirmedPhrase = "that's correct — save it as verified"

// userConfirmedVerification reports whether this incoming prompt is the user
// answering an escalation follow-up with a confirmation.
func userConfirmedVerification(prompt string) bool {
	return strings.Contains(strings.ToLower(prompt), verificationConfirmedPhrase)
}

// promoteLastAnswerToVerifiedFact stores the claim the user just confirmed.
// The claim is the previous assistant message and the subject comes from the
// question before it, since the confirmation message itself ("that's
// correct") carries no subject of its own.
func (a *Agent) promoteLastAnswerToVerifiedFact(ctx context.Context, workspaceID string, history []ctxMessage) {
	claim, question := "", ""
	for i := len(history) - 1; i >= 0; i-- {
		if claim == "" && history[i].Role == "assistant" {
			claim = history[i].Content
			continue
		}
		if claim != "" && history[i].Role == "user" && !userConfirmedVerification(history[i].Content) {
			question = history[i].Content
			break
		}
	}
	if claim == "" || question == "" {
		return
	}
	a.rememberVerifiedFact(ctx, workspaceID, question, claim, nil, "user")
}

// ctxMessage is the minimal shape needed from stored history, kept local so
// this file doesn't depend on the context package's full message type.
type ctxMessage struct {
	Role    string
	Content string
}

// recallVerifiedFact looks for a previously verified answer to this question.
// Returns empty when nothing has been verified for the subject.
func (a *Agent) recallVerifiedFact(ctx context.Context, workspaceID, question string) (string, bool) {
	if a.DB == nil || workspaceID == "" {
		return "", false
	}
	subject := factSubjectKey(question)
	if subject == "" {
		return "", false
	}

	// Exact key first (cheap), then a token-overlap match. Requiring the key
	// to match exactly made recall brittle: "PHP release, the latest one"
	// keys as "one php" and would miss a fact stored under "php", so the
	// agent would re-research a question it had already settled — the exact
	// waste this store exists to prevent.
	fact, ok := a.DB.LookupVerifiedFact(ctx, workspaceID, subject)
	if !ok {
		fact, ok = a.findFactBySubjectOverlap(ctx, workspaceID, subject)
	}
	if !ok {
		return "", false
	}

	var note strings.Builder
	note.WriteString("PREVIOUSLY VERIFIED — this exact subject was already checked and confirmed, so it does not need researching again:\n")
	note.WriteString("- " + fact.Fact + "\n")
	if len(fact.Sources) > 0 {
		note.WriteString("  (corroborated by: " + strings.Join(fact.Sources, ", ") + ")\n")
	}
	if fact.VerifiedBy == "user" {
		note.WriteString("  (confirmed directly by the user)\n")
	}
	note.WriteString("Use this directly unless the user's question is about something that would have changed since.\n")
	return note.String(), true
}

// subjectOverlapThreshold is how much of the smaller token set must be shared
// for two subjects to be considered the same thing. Measured against the
// smaller set so a more specific question still finds a fact stored under a
// broader subject ("php" matches "one php"), while genuinely different
// subjects ("php" vs "python") share nothing and stay apart.
const subjectOverlapThreshold = 0.75

// findFactBySubjectOverlap scans this workspace's verified facts for one
// whose subject substantially overlaps. The scan is in Go rather than SQL
// because the set is small by construction — only cross-source-verified or
// human-confirmed statements ever land here, so this is tens of rows, not
// the whole knowledge base.
func (a *Agent) findFactBySubjectOverlap(ctx context.Context, workspaceID, subject string) (db.VerifiedFact, bool) {
	facts, err := a.DB.ListVerifiedFacts(ctx, workspaceID, 200)
	if err != nil || len(facts) == 0 {
		return db.VerifiedFact{}, false
	}

	queryTokens := map[string]bool{}
	for _, token := range strings.Fields(subject) {
		queryTokens[token] = true
	}

	best, bestScore := db.VerifiedFact{}, 0.0
	for _, candidate := range facts {
		candidateTokens := strings.Fields(candidate.Subject)
		if len(candidateTokens) == 0 {
			continue
		}
		shared := 0
		for _, token := range candidateTokens {
			if queryTokens[token] {
				shared++
			}
		}
		smaller := len(candidateTokens)
		if len(queryTokens) < smaller {
			smaller = len(queryTokens)
		}
		if smaller == 0 {
			continue
		}
		score := float64(shared) / float64(smaller)
		if score > bestScore {
			best, bestScore = candidate, score
		}
	}
	if bestScore >= subjectOverlapThreshold {
		return best, true
	}
	return db.VerifiedFact{}, false
}

// recallApprovedChanges surfaces edits the user previously accepted that
// touched the same files this request is about. This is the read side of a
// memory that was previously write-only: session_learnings has been
// accumulating rows for a long time (52 in this workspace when the gap was
// found) but nothing ever fed them back to the model, so every run
// re-derived what earlier runs had already settled.
//
// Only approved rows are considered — see db.ListApprovedChanges for why
// recalling the unapproved ones would actively teach bad behaviour.
func (a *Agent) recallApprovedChanges(ctx context.Context, workspaceID, request string) string {
	if a.DB == nil || workspaceID == "" {
		return ""
	}
	changes, err := a.DB.ListApprovedChanges(ctx, workspaceID, 40)
	if err != nil || len(changes) == 0 {
		return ""
	}

	tokens := coreSearchTokens(request)
	if len(tokens) == 0 {
		return ""
	}

	var relevant []db.ApprovedChange
	for _, change := range changes {
		haystack := strings.ToLower(change.Summary + " " + strings.Join(change.Files, " "))
		if matchesAnyToken(tokens, haystack) {
			relevant = append(relevant, change)
		}
		if len(relevant) >= 5 {
			break
		}
	}
	if len(relevant) == 0 {
		return ""
	}

	var note strings.Builder
	note.WriteString("PREVIOUSLY APPROVED WORK on these files — the user accepted these changes, so follow the same conventions rather than working them out again:\n")
	for _, change := range relevant {
		note.WriteString("- ")
		if change.Summary != "" {
			note.WriteString(change.Summary)
		}
		if len(change.Files) > 0 {
			note.WriteString(" (" + strings.Join(change.Files, ", ") + ")")
		}
		note.WriteString("\n")
	}
	return note.String()
}

// rememberVerifiedFact stores a fact that has passed a verification gate.
// Callers must only reach here after cross-source agreement or explicit user
// confirmation — the whole value of this store is that everything in it was
// checked, so writing an unverified claim would poison future recall.
func (a *Agent) rememberVerifiedFact(ctx context.Context, workspaceID, question, fact string, sources []string, verifiedBy string) {
	if a.DB == nil || workspaceID == "" {
		return
	}
	fact = strings.TrimSpace(fact)
	if fact == "" {
		return
	}
	subject := factSubjectKey(question)
	if subject == "" {
		return
	}
	if err := a.DB.SaveVerifiedFact(ctx, workspaceID, subject, question, fact, sources, verifiedBy); err != nil {
		log.Printf("[agent] could not save verified fact for %q: %v", subject, err)
	}
}
