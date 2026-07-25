package db

import (
	"context"
	"encoding/json"
	"time"
)

// VerifiedFact is a statement that passed a verification gate — either
// several independent sources agreed on it, or a human confirmed it. Nothing
// reaches this type without one of those, which is what makes it safe to
// recall later as fact instead of re-researching it every turn.
type VerifiedFact struct {
	ID         string    `json:"id"`
	Subject    string    `json:"subject"`
	Question   string    `json:"question"`
	Fact       string    `json:"fact"`
	Sources    []string  `json:"sources"`
	VerifiedBy string    `json:"verified_by"`
	CreatedAt  time.Time `json:"created_at"`
}

// SaveVerifiedFact records (or refreshes) a verified statement. Re-verifying
// the same subject updates the existing row rather than accumulating
// duplicates, so recall stays small and the newest evidence wins.
func (d *DB) SaveVerifiedFact(ctx context.Context, workspaceID, subject, question, fact string, sources []string, verifiedBy string) error {
	if sources == nil {
		sources = []string{}
	}
	encoded, err := json.Marshal(sources)
	if err != nil {
		return err
	}

	result, err := d.ExecContext(ctx,
		`UPDATE verified_facts SET fact=?, question=?, sources=?, verified_by=?, updated_at=CURRENT_TIMESTAMP
		 WHERE workspace_id=? AND subject=?`,
		fact, question, string(encoded), verifiedBy, workspaceID, subject)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err == nil && affected > 0 {
		return nil
	}

	_, err = d.ExecContext(ctx,
		`INSERT INTO verified_facts(id,workspace_id,subject,question,fact,sources,verified_by)
		 VALUES(?,?,?,?,?,?,?)`,
		NewID(), workspaceID, subject, question, fact, string(encoded), verifiedBy)
	return err
}

// LookupVerifiedFact returns a previously verified answer for a subject key,
// or ok=false when nothing has been verified for it yet.
func (d *DB) LookupVerifiedFact(ctx context.Context, workspaceID, subject string) (VerifiedFact, bool) {
	var fact VerifiedFact
	var encodedSources string
	err := d.QueryRowContext(ctx,
		`SELECT id,subject,question,fact,sources,verified_by,created_at FROM verified_facts
		 WHERE workspace_id=? AND subject=? LIMIT 1`, workspaceID, subject).
		Scan(&fact.ID, &fact.Subject, &fact.Question, &fact.Fact, &encodedSources, &fact.VerifiedBy, &fact.CreatedAt)
	if err != nil {
		return VerifiedFact{}, false
	}
	_ = json.Unmarshal([]byte(encodedSources), &fact.Sources)
	return fact, true
}

// ListVerifiedFacts returns the most recently verified facts for a workspace,
// newest first — used by the Knowledge page so the user can see and audit
// what the agent believes it has confirmed.
func (d *DB) ListVerifiedFacts(ctx context.Context, workspaceID string, limit int) ([]VerifiedFact, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT id,subject,question,fact,sources,verified_by,created_at FROM verified_facts
		 WHERE workspace_id=? ORDER BY updated_at DESC LIMIT ?`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var facts []VerifiedFact
	for rows.Next() {
		var fact VerifiedFact
		var encodedSources string
		if err := rows.Scan(&fact.ID, &fact.Subject, &fact.Question, &fact.Fact, &encodedSources, &fact.VerifiedBy, &fact.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(encodedSources), &fact.Sources)
		facts = append(facts, fact)
	}
	return facts, rows.Err()
}

// ApprovedChange is a past edit the user actually accepted. Recall is
// restricted to approved rows on purpose: session_learnings also holds rows
// the agent wrote about its own runs with an empty status, and those record
// the model's claim about its result rather than a checked outcome. Feeding
// those back would teach it to repeat whatever it did last time, including
// mistakes.
type ApprovedChange struct {
	Summary   string    `json:"summary"`
	Files     []string  `json:"files"`
	CreatedAt time.Time `json:"created_at"`
}

// ListApprovedChanges returns recent user-approved changes for a workspace.
func (d *DB) ListApprovedChanges(ctx context.Context, workspaceID string, limit int) ([]ApprovedChange, error) {
	rows, err := d.QueryContext(ctx,
		`SELECT learning,created_at FROM session_learnings
		 WHERE workspace_id=? AND patch_status='approved'
		 ORDER BY created_at DESC LIMIT ?`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var changes []ApprovedChange
	for rows.Next() {
		var raw string
		var createdAt time.Time
		if err := rows.Scan(&raw, &createdAt); err != nil {
			return nil, err
		}
		var payload struct {
			FilesChanged  []string `json:"files_changed"`
			ChangeSummary string   `json:"change_summary"`
		}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			continue
		}
		if payload.ChangeSummary == "" && len(payload.FilesChanged) == 0 {
			continue
		}
		changes = append(changes, ApprovedChange{
			Summary: payload.ChangeSummary, Files: payload.FilesChanged, CreatedAt: createdAt,
		})
	}
	return changes, rows.Err()
}

// DeleteVerifiedFact removes a fact — needed because a "verified" fact can
// still go stale (a version number, a price), and the user must be able to
// throw one away without clearing the whole workspace.
func (d *DB) DeleteVerifiedFact(ctx context.Context, workspaceID, id string) error {
	_, err := d.ExecContext(ctx, `DELETE FROM verified_facts WHERE workspace_id=? AND id=?`, workspaceID, id)
	return err
}
