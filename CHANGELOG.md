# Changelog

All notable changes to NativeStudio are recorded here.

---

## [Unreleased]

### [PERF] Knowledge Enrichment Speed-Up (3-phase optimisation)

**Before:** Enrichment processed files serially, one at a time. Every file,
even those with unchanged content, triggered a full `SummarizeFile` LLM call.
Result: ~2–5 s × N files = tens of seconds on a medium project.

**After:** Three layered improvements bring initial enrichment to ~8 s for a
50-file project on `qwen2.5-coder:1.5b` and near-zero on repeat runs.

#### Phase 1 — Parallel Worker Pool + Summary Hash Cache (`indexer/enrichment.go`, `db/knowledge.go`)
- Replaced serial enrichment loop with a **bounded N-worker pool** (up to 4
  goroutines, capped at `runtime.NumCPU()`). Files are now claimed and
  processed in parallel; `ClaimIndexJob` was already transaction-safe.
- Added `DB.FileSummaryByHash` — looks up an existing active summary by
  `(workspace_id, path, content_hash)`. If a match is found, `SummarizeFile`
  is skipped entirely. Unchanged files cost 0 LLM calls on re-index.
- Reduced aggregate debounce timer from **2 s → 500 ms**.

#### Phase 2 — Separate Embedding Concurrency (`ollama/client.go`, `main.go`, `config.json`)
- Added `embedding_concurrency` config field (default `4`).
- Introduced `SplitClient` that routes `Embed()` through a high-concurrency
  client and all `Summarize*` calls through the original rate-limited client.
  Prevents a bulk embedding burst from blocking agent chat responses.

#### Phase 3 — Incremental Module Summary (`indexer/enrichment.go`)
- Worker pool result now carries the changed file path.
- `drainWithWorkers` extracts and returns the set of **changed top-level module
  directories** (e.g. `"agent"`, `"handlers"`).
- `enrichmentLoop` accumulates this set and passes each changed module to
  `refreshModuleSummaries(onlyModule=…)` instead of rebuilding all modules.
  A project with 10 modules where only 1 file changed now re-summarises 1
  module instead of all 10.

### [FIX] Model dropdown blank page (`frontend/src/components/ChatPanel.jsx`)
- `/api/models` now returns `[{name, thinking_capable}]` objects. Frontend
  was still treating the array as plain strings → React crash → blank page.
- Migrated to a `modelCapMapRef` (name → bool) populated at fetch time. Regex
  `supportsThinking` removed; backend `SupportsNativeThinking()` is now the
  single source of truth.

### [FEAT] Dynamic thinking capability from backend (`handlers/models.go`, `agent/loop.go`)
- `GET /api/models` returns `thinking_capable: true/false` per model.
- Frontend `Think` toggle and effort dropdown now respond to this flag without
  any client-side regex matching.
