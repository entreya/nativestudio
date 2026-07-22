CREATE TABLE IF NOT EXISTS indexing_skips (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES indexing_runs(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    reason TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_indexing_skips_workspace ON indexing_skips(workspace_id, created_at);
