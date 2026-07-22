package db

import (
	"time"
)

type Session struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Title       string    `json:"title"`
	Model       string    `json:"model"`
	Status      string    `json:"status"`
	Summary     string    `json:"summary"`
	TokenBudget int       `json:"token_budget"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ConversationOverview combines a session with the project and persisted
// memory statistics needed by the conversations dashboard.
type ConversationOverview struct {
	Session
	ProjectName     string `json:"project_name"`
	ProjectPath     string `json:"project_path"`
	MessageCount    int    `json:"message_count"`
	TokensUsed      int    `json:"tokens_used"`
	CheckpointCount int    `json:"checkpoint_count"`
	LatestMemory    string `json:"latest_memory"`
}

func (d *DB) CreateSession(id, projectID, model, title string) (*Session, error) {
	_, err := d.Exec(`
		INSERT INTO sessions (id, project_id, title, model)
		VALUES (?, ?, ?, ?)
	`, id, projectID, title, model)
	if err != nil {
		return nil, err
	}
	return d.GetSession(id)
}

func (d *DB) ListSessions(projectID string) ([]Session, error) {
	rows, err := d.Query(`
		SELECT id, project_id, title, model, status, COALESCE(summary, ''), token_budget, created_at, updated_at 
		FROM sessions WHERE project_id = ? ORDER BY updated_at DESC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.ProjectID, &s.Title, &s.Model, &s.Status, &s.Summary, &s.TokenBudget, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

func (d *DB) ListConversationOverviews() ([]ConversationOverview, error) {
	rows, err := d.Query(`
		SELECT s.id, s.project_id, s.title, s.model, s.status, COALESCE(s.summary, ''), s.token_budget,
		       s.created_at, s.updated_at, p.name, p.path,
		       (SELECT COUNT(*) FROM messages m WHERE m.session_id = s.id),
		       COALESCE((SELECT SUM(m.token_estimate) FROM messages m WHERE m.session_id = s.id), 0),
		       (SELECT COUNT(*) FROM checkpoints c WHERE c.session_id = s.id),
		       COALESCE((SELECT c.summary FROM checkpoints c WHERE c.session_id = s.id ORDER BY c.created_at DESC LIMIT 1), '')
		FROM sessions s
		JOIN projects p ON p.id = s.project_id
		ORDER BY s.updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var conversations []ConversationOverview
	for rows.Next() {
		var c ConversationOverview
		if err := rows.Scan(
			&c.ID, &c.ProjectID, &c.Title, &c.Model, &c.Status, &c.Summary, &c.TokenBudget,
			&c.CreatedAt, &c.UpdatedAt, &c.ProjectName, &c.ProjectPath,
			&c.MessageCount, &c.TokensUsed, &c.CheckpointCount, &c.LatestMemory,
		); err != nil {
			return nil, err
		}
		conversations = append(conversations, c)
	}
	return conversations, rows.Err()
}

func (d *DB) GetSession(id string) (*Session, error) {
	var s Session
	err := d.QueryRow(`
		SELECT id, project_id, title, model, status, COALESCE(summary, ''), token_budget, created_at, updated_at 
		FROM sessions WHERE id = ?
	`, id).Scan(&s.ID, &s.ProjectID, &s.Title, &s.Model, &s.Status, &s.Summary, &s.TokenBudget, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (d *DB) UpdateSessionTitle(id, title string) error {
	_, err := d.Exec(`UPDATE sessions SET title = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, title, id)
	return err
}

func (d *DB) UpdateSessionMetadata(id, title, summary string) error {
	_, err := d.Exec(`
		UPDATE sessions SET title = ?, summary = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?
	`, title, summary, id)
	return err
}

func (d *DB) DeleteSession(id string) error {
	_, err := d.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (d *DB) TouchSession(id string) error {
	_, err := d.Exec(`UPDATE sessions SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, id)
	return err
}
