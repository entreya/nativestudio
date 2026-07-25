-- Facts the agent has actually verified, so a settled question is never
-- re-researched from scratch on the next turn.
--
-- Deliberately separate from session_learnings: that table records what
-- happened during a run (episodic, unverified — it stores the model's own
-- claim about its result, which may be wrong). This table only ever receives
-- statements that passed a verification gate, either agreement across
-- several independent sources or an explicit human confirmation. Keeping
-- them apart makes "only verified content is ever recalled as fact" a
-- property of the schema rather than a rule someone has to remember.
CREATE TABLE IF NOT EXISTS verified_facts (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    -- subject is the normalized lookup key (sorted core query tokens), so a
    -- differently-worded question about the same thing still finds it.
    subject TEXT NOT NULL,
    question TEXT NOT NULL,
    fact TEXT NOT NULL,
    -- JSON array of the independent source domains that backed this.
    sources TEXT NOT NULL DEFAULT '[]',
    -- 'cross_source' (several independent sources agreed) or 'user'
    -- (a human explicitly confirmed it).
    verified_by TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_verified_facts_lookup
    ON verified_facts(workspace_id, subject);
