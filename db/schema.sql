PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

-- projects: a named local directory the user works on
CREATE TABLE IF NOT EXISTS projects (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    path        TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- sessions: a conversation thread tied to a project
CREATE TABLE IF NOT EXISTS sessions (
    id           TEXT PRIMARY KEY,
    project_id   TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title        TEXT,
    model        TEXT NOT NULL,
    status       TEXT DEFAULT 'active',
    summary      TEXT NOT NULL DEFAULT '',
    token_budget INTEGER DEFAULT 8192,
    created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- messages: every user/assistant/system turn
CREATE TABLE IF NOT EXISTS messages (
    id              TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    role            TEXT NOT NULL,
    content         TEXT NOT NULL,
    is_checkpoint   BOOLEAN DEFAULT FALSE,
    token_estimate  INTEGER DEFAULT 0,
    sequence        INTEGER NOT NULL,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, sequence);

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

-- checkpoints: summaries of compressed message ranges
CREATE TABLE IF NOT EXISTS checkpoints (
    id                TEXT PRIMARY KEY,
    session_id        TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    summary           TEXT NOT NULL,
    tokens_before     INTEGER,
    tokens_after      INTEGER,
    message_seq_start INTEGER,
    message_seq_end   INTEGER,
    created_at        DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- editor_states: latest editor state snapshot per session (one row per session, id=session_id)
CREATE TABLE IF NOT EXISTS editor_states (
    id              TEXT PRIMARY KEY,
    session_id      TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    message_id      TEXT,
    active_file     TEXT,
    open_files      TEXT DEFAULT '[]',
    cursor_line     INTEGER DEFAULT 0,
    cursor_column   INTEGER DEFAULT 0,
    selection_start INTEGER,
    selection_end   INTEGER,
    selection_text  TEXT,
    selected_symbol TEXT,
    recent_files    TEXT DEFAULT '[]',
    recent_edits    TEXT DEFAULT '[]',
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);
