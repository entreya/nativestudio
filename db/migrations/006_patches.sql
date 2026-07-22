CREATE TABLE IF NOT EXISTS patches (
    id               TEXT PRIMARY KEY,
    session_id       TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    run_id           TEXT NOT NULL,
    file_path        TEXT NOT NULL,
    operation        TEXT NOT NULL,
    diff             TEXT,
    new_content      TEXT,
    original_content TEXT,
    rename_to        TEXT,
    status           TEXT DEFAULT 'pending',
    applied_at       DATETIME,
    created_at       DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_patches_session ON patches(session_id, status);
