CREATE TABLE IF NOT EXISTS repository_files (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    language TEXT NOT NULL DEFAULT '',
    size_bytes INTEGER NOT NULL DEFAULT 0,
    content_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    skip_reason TEXT NOT NULL DEFAULT '',
    indexed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(workspace_id, path)
);
CREATE INDEX IF NOT EXISTS idx_repository_files_workspace_path ON repository_files(workspace_id, path);
CREATE INDEX IF NOT EXISTS idx_repository_files_hash ON repository_files(content_hash);
CREATE INDEX IF NOT EXISTS idx_repository_files_status ON repository_files(workspace_id, status);

CREATE TABLE IF NOT EXISTS symbols (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    file_id TEXT NOT NULL REFERENCES repository_files(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    symbol_name TEXT NOT NULL,
    qualified_name TEXT NOT NULL DEFAULT '',
    symbol_type TEXT NOT NULL,
    signature TEXT NOT NULL DEFAULT '',
    start_line INTEGER NOT NULL,
    end_line INTEGER NOT NULL,
    content_hash TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_symbols_workspace_name ON symbols(workspace_id, symbol_name);
CREATE INDEX IF NOT EXISTS idx_symbols_workspace_type ON symbols(workspace_id, symbol_type);
CREATE INDEX IF NOT EXISTS idx_symbols_file ON symbols(file_id);
CREATE INDEX IF NOT EXISTS idx_symbols_status ON symbols(workspace_id, status);

CREATE TABLE IF NOT EXISTS symbol_relationships (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source_symbol_id TEXT REFERENCES symbols(id) ON DELETE CASCADE,
    source_path TEXT NOT NULL,
    target_name TEXT NOT NULL,
    target_symbol_id TEXT REFERENCES symbols(id) ON DELETE SET NULL,
    relationship_type TEXT NOT NULL,
    confidence REAL NOT NULL DEFAULT 1,
    status TEXT NOT NULL DEFAULT 'active',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_relationships_workspace_source ON symbol_relationships(workspace_id, source_path);
CREATE INDEX IF NOT EXISTS idx_relationships_target ON symbol_relationships(workspace_id, target_name);

CREATE TABLE IF NOT EXISTS code_chunks (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    file_id TEXT NOT NULL REFERENCES repository_files(id) ON DELETE CASCADE,
    symbol_id TEXT REFERENCES symbols(id) ON DELETE SET NULL,
    path TEXT NOT NULL,
    start_line INTEGER NOT NULL,
    end_line INTEGER NOT NULL,
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    token_estimate INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'active',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_chunks_workspace_path ON code_chunks(workspace_id, path);
CREATE INDEX IF NOT EXISTS idx_chunks_hash ON code_chunks(content_hash);
CREATE INDEX IF NOT EXISTS idx_chunks_status ON code_chunks(workspace_id, status);

CREATE TABLE IF NOT EXISTS embeddings (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    chunk_id TEXT NOT NULL REFERENCES code_chunks(id) ON DELETE CASCADE,
    model TEXT NOT NULL,
    dimensions INTEGER NOT NULL,
    embedding BLOB NOT NULL,
    content_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(chunk_id, model)
);
CREATE INDEX IF NOT EXISTS idx_embeddings_workspace_model ON embeddings(workspace_id, model, status);

CREATE TABLE IF NOT EXISTS file_summaries (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    file_id TEXT NOT NULL REFERENCES repository_files(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    summary TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    model TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(workspace_id, path, content_hash)
);
CREATE INDEX IF NOT EXISTS idx_file_summaries_workspace_path ON file_summaries(workspace_id, path, status);

CREATE TABLE IF NOT EXISTS module_summaries (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    module_path TEXT NOT NULL,
    summary TEXT NOT NULL,
    source_hash TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    model TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(workspace_id, module_path, source_hash)
);

CREATE TABLE IF NOT EXISTS project_summaries (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    summary TEXT NOT NULL,
    source_hash TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    model TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_project_summaries_workspace ON project_summaries(workspace_id, status, updated_at);

CREATE TABLE IF NOT EXISTS summary_versions (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    summary_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    version INTEGER NOT NULL,
    input_hash TEXT NOT NULL,
    prompt_version TEXT NOT NULL,
    model TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'valid',
    error TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS project_facts (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    category TEXT NOT NULL,
    fact TEXT NOT NULL,
    confidence REAL NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'proposed',
    pinned INTEGER NOT NULL DEFAULT 0,
    is_rule INTEGER NOT NULL DEFAULT 0,
    last_verified_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_project_facts_workspace_status ON project_facts(workspace_id, status, updated_at);

CREATE TABLE IF NOT EXISTS fact_sources (
    id TEXT PRIMARY KEY,
    fact_id TEXT NOT NULL REFERENCES project_facts(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    symbol_name TEXT NOT NULL DEFAULT '',
    start_line INTEGER,
    end_line INTEGER,
    content_hash TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_fact_sources_path_hash ON fact_sources(path, content_hash);

CREATE TABLE IF NOT EXISTS project_decisions (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
    decision TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_project_decisions_workspace_status ON project_decisions(workspace_id, status, updated_at);

CREATE TABLE IF NOT EXISTS session_learnings (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    session_id TEXT REFERENCES sessions(id) ON DELETE CASCADE,
    run_id TEXT NOT NULL,
    learning TEXT NOT NULL,
    patch_status TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS indexing_runs (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    processed INTEGER NOT NULL DEFAULT 0,
    total INTEGER NOT NULL DEFAULT 0,
    skipped INTEGER NOT NULL DEFAULT 0,
    errors INTEGER NOT NULL DEFAULT 0,
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME,
    error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_indexing_runs_workspace ON indexing_runs(workspace_id, started_at);

CREATE TABLE IF NOT EXISTS indexing_errors (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES indexing_runs(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    path TEXT NOT NULL DEFAULT '',
    stage TEXT NOT NULL,
    error TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS file_changes (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
    path TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    original_hash TEXT NOT NULL DEFAULT '',
    proposed_hash TEXT NOT NULL DEFAULT '',
    patch TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    tests TEXT NOT NULL DEFAULT '[]',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_file_changes_workspace_status ON file_changes(workspace_id, status, updated_at);

CREATE TABLE IF NOT EXISTS project_index_settings (
    workspace_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    include_paths TEXT NOT NULL DEFAULT '[]',
    exclude_paths TEXT NOT NULL DEFAULT '[]',
    sensitive_patterns TEXT NOT NULL DEFAULT '[]',
    maximum_file_size_bytes INTEGER NOT NULL DEFAULT 1048576,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
