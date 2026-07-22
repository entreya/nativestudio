ALTER TABLE repository_files ADD COLUMN modified_at_ns INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS index_jobs (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    run_id TEXT REFERENCES indexing_runs(id) ON DELETE SET NULL,
    path TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    job_type TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 100,
    status TEXT NOT NULL DEFAULT 'queued',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    available_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at DATETIME,
    completed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(workspace_id, path, job_type)
);
CREATE INDEX IF NOT EXISTS idx_index_jobs_claim ON index_jobs(workspace_id, job_type, status, priority, available_at, created_at);
