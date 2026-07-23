package db

import (
	"context"
	"time"
)

// CommandRun is a shell command the agent wants to run, staged for explicit
// human approval before it ever actually executes — the same pattern as
// Patch, since running an arbitrary command is at least as consequential as
// writing a file.
type CommandRun struct {
	ID         string     `json:"run_id"`
	SessionID  string     `json:"session_id"`
	AgentRunID string     `json:"agent_run_id"`
	Command    string     `json:"command"`
	Cwd        string     `json:"cwd"`
	Status     string     `json:"status"`
	Output     string     `json:"output,omitempty"`
	ExitCode   *int       `json:"exit_code,omitempty"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (d *DB) CreateCommandRun(ctx context.Context, run CommandRun) error {
	if run.ID == "" {
		run.ID = NewID()
	}
	_, err := d.ExecContext(ctx, `INSERT INTO command_runs(id,session_id,run_id,command,cwd,status)
		VALUES(?,?,?,?,?,'pending')`, run.ID, run.SessionID, run.AgentRunID, run.Command, run.Cwd)
	return err
}

func (d *DB) GetCommandRun(ctx context.Context, id string) (CommandRun, error) {
	var run CommandRun
	err := d.QueryRowContext(ctx, `SELECT id,session_id,run_id,command,cwd,status,COALESCE(output,''),exit_code,started_at,finished_at,created_at
		FROM command_runs WHERE id=?`, id).
		Scan(&run.ID, &run.SessionID, &run.AgentRunID, &run.Command, &run.Cwd, &run.Status, &run.Output, &run.ExitCode, &run.StartedAt, &run.FinishedAt, &run.CreatedAt)
	return run, err
}

func (d *DB) ListCommandRuns(ctx context.Context, sessionID, status string) ([]CommandRun, error) {
	query := `SELECT id,session_id,run_id,command,cwd,status,COALESCE(output,''),exit_code,started_at,finished_at,created_at
		FROM command_runs WHERE session_id=?`
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
	var runs []CommandRun
	for rows.Next() {
		var run CommandRun
		if err := rows.Scan(&run.ID, &run.SessionID, &run.AgentRunID, &run.Command, &run.Cwd, &run.Status, &run.Output, &run.ExitCode, &run.StartedAt, &run.FinishedAt, &run.CreatedAt); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// SetCommandRunStatus transitions a command run out of "pending" (approve →
// running, or reject → rejected) only if it is still in the expected state —
// the same optimistic-concurrency guard Patch uses, which here also doubles
// as the lock preventing a command from being executed twice concurrently.
func (d *DB) SetCommandRunStatus(ctx context.Context, id, expectedStatus, status string) (bool, error) {
	result, err := d.ExecContext(ctx, `UPDATE command_runs SET status=?,started_at=CASE WHEN ?='running' THEN CURRENT_TIMESTAMP ELSE started_at END WHERE id=? AND status=?`, status, status, id, expectedStatus)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

// FinishCommandRun records the result of a command already transitioned to
// "running" by this same request — no further ownership check is needed
// since SetCommandRunStatus already claimed it exclusively.
func (d *DB) FinishCommandRun(ctx context.Context, id, status, output string, exitCode int) error {
	_, err := d.ExecContext(ctx, `UPDATE command_runs SET status=?,output=?,exit_code=?,finished_at=CURRENT_TIMESTAMP WHERE id=?`, status, output, exitCode, id)
	return err
}
