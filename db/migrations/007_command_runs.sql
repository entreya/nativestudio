CREATE TABLE IF NOT EXISTS command_runs (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    run_id      TEXT NOT NULL,
    command     TEXT NOT NULL,
    cwd         TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'pending',
    output      TEXT NOT NULL DEFAULT '',
    exit_code   INTEGER,
    started_at  DATETIME,
    finished_at DATETIME,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_command_runs_session ON command_runs(session_id, status);
