package db

import (
	"context"
	"time"
)

type Patch struct {
	ID              string     `json:"patch_id"`
	SessionID       string     `json:"session_id"`
	RunID           string     `json:"run_id"`
	FilePath        string     `json:"file_path"`
	Operation       string     `json:"operation"`
	Diff            string     `json:"diff"`
	NewContent      string     `json:"new_content,omitempty"`
	OriginalContent string     `json:"original_content,omitempty"`
	RenameTo        string     `json:"rename_to,omitempty"`
	Status          string     `json:"status"`
	AppliedAt       *time.Time `json:"applied_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

func (d *DB) CreatePatch(ctx context.Context, patch Patch) error {
	if patch.ID == "" {
		patch.ID = NewID()
	}
	_, err := d.ExecContext(ctx, `INSERT INTO patches(id,session_id,run_id,file_path,operation,diff,new_content,original_content,rename_to,status)
		VALUES(?,?,?,?,?,?,?,?,?,'pending')`, patch.ID, patch.SessionID, patch.RunID, patch.FilePath, patch.Operation, patch.Diff, patch.NewContent, patch.OriginalContent, nullableString(patch.RenameTo))
	return err
}

func (d *DB) GetPatch(ctx context.Context, id string) (Patch, error) {
	var patch Patch
	err := d.QueryRowContext(ctx, `SELECT id,session_id,run_id,file_path,operation,COALESCE(diff,''),COALESCE(new_content,''),
		COALESCE(original_content,''),COALESCE(rename_to,''),status,applied_at,created_at FROM patches WHERE id=?`, id).
		Scan(&patch.ID, &patch.SessionID, &patch.RunID, &patch.FilePath, &patch.Operation, &patch.Diff, &patch.NewContent, &patch.OriginalContent, &patch.RenameTo, &patch.Status, &patch.AppliedAt, &patch.CreatedAt)
	return patch, err
}

func (d *DB) ListPatches(ctx context.Context, sessionID, status string) ([]Patch, error) {
	query := `SELECT id,session_id,run_id,file_path,operation,COALESCE(diff,''),COALESCE(new_content,''),
		COALESCE(original_content,''),COALESCE(rename_to,''),status,applied_at,created_at FROM patches WHERE session_id=?`
	args := []any{sessionID}
	if status != "" {
		query += ` AND status=?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at,id`
	rows, err := d.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var patches []Patch
	for rows.Next() {
		var patch Patch
		if err := rows.Scan(&patch.ID, &patch.SessionID, &patch.RunID, &patch.FilePath, &patch.Operation, &patch.Diff, &patch.NewContent, &patch.OriginalContent, &patch.RenameTo, &patch.Status, &patch.AppliedAt, &patch.CreatedAt); err != nil {
			return nil, err
		}
		patches = append(patches, patch)
	}
	return patches, rows.Err()
}

func (d *DB) SetPatchStatus(ctx context.Context, id, expectedStatus, status string) (bool, error) {
	result, err := d.ExecContext(ctx, `UPDATE patches SET status=?,applied_at=CASE WHEN ?='applied' THEN CURRENT_TIMESTAMP ELSE applied_at END WHERE id=? AND status=?`, status, status, id, expectedStatus)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}
