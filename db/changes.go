package db

import "context"

type FileChange struct{ ID, WorkspaceID, SessionID, Path, Summary, OriginalHash, ProposedHash, Content, Status string }

func (d *DB) CreateFileChange(ctx context.Context, change FileChange) error {
	_, err := d.ExecContext(ctx, `INSERT OR IGNORE INTO file_changes(id,workspace_id,session_id,path,summary,original_hash,proposed_hash,patch,status) VALUES(?,?,?,?,?,?,?,?, 'pending')`, change.ID, change.WorkspaceID, change.SessionID, change.Path, change.Summary, change.OriginalHash, change.ProposedHash, change.Content)
	return err
}
func (d *DB) GetFileChange(ctx context.Context, id string) (FileChange, error) {
	var c FileChange
	err := d.QueryRowContext(ctx, `SELECT id,workspace_id,COALESCE(session_id,''),path,summary,original_hash,proposed_hash,patch,status FROM file_changes WHERE id=?`, id).Scan(&c.ID, &c.WorkspaceID, &c.SessionID, &c.Path, &c.Summary, &c.OriginalHash, &c.ProposedHash, &c.Content, &c.Status)
	return c, err
}
func (d *DB) SetFileChangeStatus(ctx context.Context, id, status, contentHash string) error {
	_, err := d.ExecContext(ctx, `UPDATE file_changes SET status=?,proposed_hash=CASE WHEN ?<>'' THEN ? ELSE proposed_hash END,updated_at=CURRENT_TIMESTAMP WHERE id=?`, status, contentHash, contentHash, id)
	return err
}
func (d *DB) RecordSessionLearning(ctx context.Context, workspaceID, sessionID, runID, learning, patchStatus string) error {
	_, err := d.ExecContext(ctx, `INSERT INTO session_learnings(id,workspace_id,session_id,run_id,learning,patch_status) VALUES(?,?,?,?,?,?)`, NewID(), workspaceID, nullableString(sessionID), runID, learning, patchStatus)
	return err
}
func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func (d *DB) RecordDecision(ctx context.Context, workspaceID, sessionID, decision, reason, status string) error {
	_, err := d.ExecContext(ctx, `INSERT INTO project_decisions(id,workspace_id,session_id,decision,reason,status) VALUES(?,?,?,?,?,?)`, NewID(), workspaceID, nullableString(sessionID), decision, reason, status)
	return err
}
