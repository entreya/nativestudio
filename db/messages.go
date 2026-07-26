package db

import (
	"time"
)

type Message struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	// Timeline is the JSON event log of the agent's reasoning trace for an
	// assistant message ("" for user messages). The client replays it to
	// rebuild the same timeline it showed live.
	Timeline      string    `json:"timeline,omitempty"`
	IsCheckpoint  bool      `json:"is_checkpoint"`
	TokenEstimate int       `json:"token_estimate"`
	Sequence      int       `json:"sequence"`
	CreatedAt     time.Time `json:"created_at"`
}

type Checkpoint struct {
	ID              string    `json:"id"`
	SessionID       string    `json:"session_id"`
	Summary         string    `json:"summary"`
	TokensBefore    int       `json:"tokens_before"`
	TokensAfter     int       `json:"tokens_after"`
	MessageSeqStart int       `json:"message_seq_start"`
	MessageSeqEnd   int       `json:"message_seq_end"`
	CreatedAt       time.Time `json:"created_at"`
}

func (d *DB) AppendMessage(id, sessionID, role, content string, isCheckpoint bool, tokenEstimate int) (*Message, error) {
	return d.AppendMessageWithTimeline(id, sessionID, role, content, "", isCheckpoint, tokenEstimate)
}

// AppendMessageWithTimeline stores a message along with the agent's reasoning
// trace. Pass "" for timeline when there is none (user messages, direct chat).
func (d *DB) AppendMessageWithTimeline(id, sessionID, role, content, timeline string, isCheckpoint bool, tokenEstimate int) (*Message, error) {
	// Need to find max sequence and increment it
	tx, err := d.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var nextSeq int
	err = tx.QueryRow(`SELECT IFNULL(MAX(sequence), 0) + 1 FROM messages WHERE session_id = ?`, sessionID).Scan(&nextSeq)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(`
		INSERT INTO messages (id, session_id, role, content, timeline, is_checkpoint, token_estimate, sequence)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, sessionID, role, content, timeline, isCheckpoint, tokenEstimate, nextSeq)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &Message{
		ID:            id,
		SessionID:     sessionID,
		Role:          role,
		Content:       content,
		Timeline:      timeline,
		IsCheckpoint:  isCheckpoint,
		TokenEstimate: tokenEstimate,
		Sequence:      nextSeq,
		CreatedAt:     time.Now(), // Approx for return value
	}, nil
}

func (d *DB) GetMessages(sessionID string) ([]Message, error) {
	rows, err := d.Query(`
		SELECT id, session_id, role, content, COALESCE(timeline,''), is_checkpoint, token_estimate, sequence, created_at
		FROM messages WHERE session_id = ? ORDER BY sequence ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.Timeline, &m.IsCheckpoint, &m.TokenEstimate, &m.Sequence, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func (d *DB) UpdateMessage(id, content string, tokenEstimate int) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var sessionID string
	if err := tx.QueryRow(`SELECT session_id FROM messages WHERE id = ?`, id).Scan(&sessionID); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		UPDATE messages SET content = ?, token_estimate = ? WHERE id = ?
	`, content, tokenEstimate, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE sessions SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, sessionID); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) ReplaceMessagesWithCheckpoint(sessionID, checkpointID, msgID string, keepFromSeq int, summary string, tokensBefore, tokensAfter int) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Delete messages with sequence < keepFromSeq
	_, err = tx.Exec(`DELETE FROM messages WHERE session_id = ? AND sequence < ?`, sessionID, keepFromSeq)
	if err != nil {
		return err
	}

	// 2. Insert new checkpoint message at sequence 0 (so it appears first)
	_, err = tx.Exec(`
		INSERT INTO messages (id, session_id, role, content, is_checkpoint, token_estimate, sequence)
		VALUES (?, ?, 'system', ?, TRUE, ?, 0)
	`, msgID, sessionID, summary, tokensAfter)
	if err != nil {
		return err
	}

	// 3. Insert checkpoint row
	_, err = tx.Exec(`
		INSERT INTO checkpoints (id, session_id, summary, tokens_before, tokens_after, message_seq_start, message_seq_end)
		VALUES (?, ?, ?, ?, ?, 0, ?)
	`, checkpointID, sessionID, summary, tokensBefore, tokensAfter, keepFromSeq-1)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (d *DB) DeleteMessages(sessionID string) error {
	_, err := d.Exec(`DELETE FROM messages WHERE session_id = ?`, sessionID)
	return err
}

func (d *DB) GetCheckpoints(sessionID string) ([]Checkpoint, error) {
	rows, err := d.Query(`
		SELECT id, session_id, summary, tokens_before, tokens_after, message_seq_start, message_seq_end, created_at
		FROM checkpoints WHERE session_id = ? ORDER BY created_at ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chks []Checkpoint
	for rows.Next() {
		var c Checkpoint
		if err := rows.Scan(&c.ID, &c.SessionID, &c.Summary, &c.TokensBefore, &c.TokensAfter, &c.MessageSeqStart, &c.MessageSeqEnd, &c.CreatedAt); err != nil {
			return nil, err
		}
		chks = append(chks, c)
	}
	return chks, nil
}
