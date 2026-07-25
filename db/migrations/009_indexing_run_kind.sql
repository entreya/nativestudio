-- Distinguishes a full workspace scan from the single-file re-index the file
-- watcher fires on every save. Without this, LatestIndexStatus (which reads the
-- most recent run) would report a watcher run's "1 file" totals, wiping a
-- full scan's real progress off the UI the moment any file changed on disk.
ALTER TABLE indexing_runs ADD COLUMN kind TEXT NOT NULL DEFAULT 'full';

CREATE INDEX IF NOT EXISTS idx_indexing_runs_workspace_kind
    ON indexing_runs(workspace_id, kind, started_at);
