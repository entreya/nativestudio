package indexer

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/entreya/nativestudio/db"
	"github.com/entreya/nativestudio/knowledge"
)

type enrichmentJob struct {
	runID       string
	workspaceID string
	root        string
	file        ScannedFile
	symbols     []knowledge.Symbol
	chunks      []knowledge.Chunk
}

func (c *Coordinator) enqueueEnrichment(ctx context.Context, job enrichmentJob) {
	if c.Models == nil {
		return
	}
	if err := c.DB.EnqueueIndexJob(ctx, job.workspaceID, job.runID, job.file.Path, job.file.Hash, "file_enrichment", 100); err != nil {
		c.recordError(ctx, job.runID, job.workspaceID, job.file.Path, "enrichment_queue", err)
		return
	}
	c.mu.Lock()
	jobs := c.enrichmentJobs
	c.mu.Unlock()
	if jobs == nil {
		return
	}
	select {
	case jobs <- struct{}{}:
	default:
	}
	remaining, _ := c.DB.PendingIndexJobCount(ctx, job.workspaceID, "file_enrichment")
	c.Broker.Emit(job.workspaceID, "enrichment_queued", map[string]any{"path": job.file.Path, "remaining": remaining})
}

// enrichmentLoop is the supervisor goroutine. On each wake-up it drains the
// queue using a bounded worker pool (workerCount goroutines) so multiple files
// can be embedded and summarised in parallel.
func (c *Coordinator) enrichmentLoop(ctx context.Context, workspaceID, root string, jobs <-chan struct{}) {
	var aggregateTimer *time.Timer
	var aggregateC <-chan time.Time
	lastRunID := ""
	completed := 0
	// pendingConfirmationEmitted avoids re-emitting the same "waiting for
	// approval" event on every 2s poll tick while jobs sit queued.
	pendingConfirmationEmitted := false
	// pausedEmitted does the same for the paused state.
	pausedEmitted := false
	// lastChangedModules accumulates top-level module dirs that changed in the
	// current enrichment cycle. Cleared after the aggregate summary pass runs.
	lastChangedModules := map[string]struct{}{}
	// Poll every 2s as a safety net so jobs enqueued before the loop started
	// are not missed if the channel signal was dropped.
	poll := time.NewTicker(2 * time.Second)

	// 500ms debounce after the last job finishes before we kick off the heavier
	// module/project aggregate summary pass.
	const aggregateDelay = 500 * time.Millisecond

	resetAggregateTimer := func() {
		if aggregateTimer == nil {
			aggregateTimer = time.NewTimer(aggregateDelay)
		} else {
			if !aggregateTimer.Stop() {
				select {
				case <-aggregateTimer.C:
				default:
				}
			}
			aggregateTimer.Reset(aggregateDelay)
		}
		aggregateC = aggregateTimer.C
	}

	defer func() {
		poll.Stop()
		if aggregateTimer != nil {
			aggregateTimer.Stop()
		}
	}()

	// workerCount caps how many files are enriched in parallel.
	// Capped at 4 — higher values stress the Ollama semaphore and add little
	// benefit on the typical developer machine.
	workerCount := runtime.NumCPU()
	if workerCount > 4 {
		workerCount = 4
	}
	if workerCount < 1 {
		workerCount = 1
	}

	for {
		select {
		case <-ctx.Done():
			// Mirrors index()'s index_cancelled emit — without this the frontend
			// has no signal that enrichment stopped and the status bar would be
			// stuck showing "running" forever after a Cancel().
			c.Broker.Emit(workspaceID, "enrichment_cancelled", map[string]any{})
			return
		case <-jobs:
		case <-poll.C:
		case <-aggregateC:
			remaining, _ := c.DB.PendingIndexJobCount(ctx, workspaceID, "file_enrichment")
			if remaining > 0 {
				resetAggregateTimer()
				continue
			}
			aggregateC = nil
			if c.Models != nil && lastRunID != "" {
				c.Broker.Emit(workspaceID, "enrichment_aggregating", map[string]any{"remaining": 0})
				summaries, _ := c.DB.FileSummaries(ctx, workspaceID, 500)
				// Refresh only the modules that had file changes in this cycle
				// (empty string = all modules as fallback when set is lost).
				for mod := range lastChangedModules {
					c.refreshModuleSummaries(ctx, lastRunID, workspaceID, summaries, mod)
				}
				if len(lastChangedModules) == 0 {
					c.refreshModuleSummaries(ctx, lastRunID, workspaceID, summaries, "")
				}
				_ = c.refreshProjectSummary(ctx, lastRunID, workspaceID, root, summaries)
				lastChangedModules = map[string]struct{}{}
			}
			c.Broker.Emit(workspaceID, "enrichment_completed", map[string]any{"completed": completed, "remaining": 0})
			completed = 0
			continue
		}

		// Paused by the user: leave the queue exactly as it is and wait for a
		// resume. The poll ticker keeps this loop alive so no wake-up signal is
		// needed when ResumeEnrichment lands.
		if c.IsEnrichmentPaused(workspaceID) {
			if !pausedEmitted {
				remaining, _ := c.DB.PendingIndexJobCount(ctx, workspaceID, "file_enrichment")
				c.Broker.Emit(workspaceID, "enrichment_paused", map[string]any{"remaining": remaining})
				pausedEmitted = true
			}
			continue
		}
		pausedEmitted = false

		// Enrichment calls the AI model, unlike the free scan above — don't
		// touch queued jobs until the user has explicitly approved it.
		if !c.IsEnrichmentApproved(workspaceID) {
			if remaining, _ := c.DB.PendingIndexJobCount(ctx, workspaceID, "file_enrichment"); remaining > 0 {
				if !pendingConfirmationEmitted {
					c.Broker.Emit(workspaceID, "enrichment_pending_confirmation", map[string]any{"remaining": remaining})
					pendingConfirmationEmitted = true
				}
			} else {
				pendingConfirmationEmitted = false
			}
			continue
		}
		pendingConfirmationEmitted = false

		// Drain the queue with a bounded worker pool. Each worker claims one job
		// at a time from the DB (ClaimIndexJob uses a transaction — safe).
		processedAny, changedMods := c.drainWithWorkers(ctx, workspaceID, root, workerCount, &lastRunID, &completed)
		if processedAny {
			for mod := range changedMods {
				lastChangedModules[mod] = struct{}{}
			}
			resetAggregateTimer()
		}
	}
}

// drainWithWorkers spawns up to workerCount goroutines and processes all
// currently queued enrichment jobs in parallel. Returns (processedAny, changedModules)
// where changedModules is the set of top-level module dirs that had files processed.
func (c *Coordinator) drainWithWorkers(
	ctx context.Context,
	workspaceID, root string,
	workerCount int,
	lastRunID *string,
	completed *int,
) (bool, map[string]struct{}) {
	// result carries per-job outcome back to the supervisor goroutine.
	type result struct {
		runID    string
		didWork  bool
		filePath string // populated on success; used to derive module name
	}

	resultCh := make(chan result, workerCount*2)
	var wg sync.WaitGroup

	// Spawn workers; each claims one job at a time until the queue is empty.
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				// Checked before each claim so a pause lands within one file
				// rather than after the whole queue has drained.
				if c.IsEnrichmentPaused(workspaceID) {
					return
				}
				claimed, err := c.DB.ClaimIndexJob(ctx, workspaceID, "file_enrichment")
				if err != nil {
					c.Broker.Emit(workspaceID, "enrichment_error", map[string]any{"error": err.Error()})
					return
				}
				if claimed == nil {
					// Queue is empty — this worker is done.
					return
				}

				runID := claimed.RunID
				file, symbols, chunks, current, loadErr := c.DB.LoadFileForEnrichment(ctx, workspaceID, claimed.Path, claimed.ContentHash)
				if loadErr != nil {
					_ = c.DB.CompleteIndexJob(ctx, claimed.ID, claimed.ContentHash, "failed", loadErr.Error())
					c.Broker.Emit(workspaceID, "enrichment_error", map[string]any{"path": claimed.Path, "error": loadErr.Error()})
					resultCh <- result{runID: runID, didWork: false}
					continue
				}
				if !current {
					_ = c.DB.CompleteIndexJob(ctx, claimed.ID, claimed.ContentHash, "stale", "file content changed")
					c.Broker.Emit(workspaceID, "enrichment_stale", map[string]any{"path": claimed.Path})
					resultCh <- result{runID: runID, didWork: false}
					continue
				}

				remaining, _ := c.DB.PendingIndexJobCount(ctx, workspaceID, "file_enrichment")
				c.Broker.Emit(workspaceID, "enrichment_started", map[string]any{"path": claimed.Path, "remaining": remaining})
				jobStartedAt := time.Now()

				job := enrichmentJob{
					runID:       runID,
					workspaceID: workspaceID,
					root:        root,
					file: ScannedFile{
						Path: file.Path, Language: file.Language, Hash: file.ContentHash,
						GitStatus: file.GitStatus, Size: file.SizeBytes, ModifiedAtNS: file.ModifiedAtNS,
					},
					symbols: symbols,
					chunks:  chunks,
				}
				c.enrichFile(ctx, job)

				if ctx.Err() != nil {
					return
				}

				_ = c.DB.CompleteIndexJob(ctx, claimed.ID, claimed.ContentHash, "completed", "")
				remaining, _ = c.DB.PendingIndexJobCount(ctx, workspaceID, "file_enrichment")
				c.Broker.Emit(workspaceID, "enrichment_progress", map[string]any{
					"path": claimed.Path, "remaining": remaining,
					"duration_ms": time.Since(jobStartedAt).Milliseconds(),
				})
				resultCh <- result{runID: runID, didWork: true, filePath: claimed.Path}
			}
		}()
	}

	// Close results channel once all workers finish.
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Collect results in the supervisor goroutine (no lock needed — single reader).
	processedAny := false
	changedModules := map[string]struct{}{}
	for r := range resultCh {
		if r.didWork {
			processedAny = true
			*completed++
			if r.runID != "" {
				*lastRunID = r.runID
			}
			// Derive the top-level module name from the file path.
			// e.g. "agent/loop.go" → "agent", "main.go" → "."
			mod := "."
			if idx := strings.Index(filepath.ToSlash(r.filePath), "/"); idx > 0 {
				mod = filepath.ToSlash(r.filePath)[:idx]
			}
			changedModules[mod] = struct{}{}
		}
	}
	return processedAny, changedModules
}

// enrichFile embeds and summarises a single file. It first checks whether a
// valid summary already exists for the exact content hash — if so, the LLM
// summary call is skipped entirely (embedding is still regenerated if missing).
func (c *Coordinator) enrichFile(ctx context.Context, job enrichmentJob) {
	texts := make([]string, len(job.chunks))
	for i := range job.chunks {
		texts[i] = job.chunks[i].Content
	}
	batchSize := c.BatchSize
	if batchSize <= 0 {
		batchSize = 10
	}
	embedded := 0
	for start := 0; start < len(texts); start += batchSize {
		end := start + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		vectors, err := c.Models.Embed(ctx, texts[start:end])
		if err != nil || len(vectors) != end-start {
			if err == nil {
				err = fmt.Errorf("embedding count mismatch")
			}
			c.recordError(ctx, job.runID, job.workspaceID, job.file.Path, "embedding", err)
			break
		}
		for i := range vectors {
			job.chunks[start+i].Embedding = vectors[i]
			embedded++
		}
	}

	// ── Summary hash cache ──────────────────────────────────────────────────
	// If a summary already exists for this exact content hash, reuse it. This
	// turns re-indexing of unchanged files into a near-zero-cost operation and
	// is the primary reason enrichment speeds up dramatically on the second run.
	var summary *knowledge.FileSummary
	existing, cacheErr := c.DB.FileSummaryByHash(ctx, job.workspaceID, job.file.Path, job.file.Hash)
	if cacheErr == nil && existing != nil {
		// Reuse the cached summary — no LLM call needed.
		summary = existing
	} else {
		// No cached summary — generate a new one.
		names := make([]string, len(job.symbols))
		for i := range job.symbols {
			names[i] = job.symbols[i].Name
		}
		excerpts := append([]string(nil), texts...)
		if len(excerpts) > 6 {
			excerpts = excerpts[:6]
		}
		for i := range excerpts {
			if len(excerpts[i]) > 6000 {
				excerpts[i] = excerpts[i][:6000]
			}
		}
		text, err := c.Models.SummarizeFile(ctx, job.file.Path, job.file.Language, names, excerpts)
		if err != nil {
			c.recordError(ctx, job.runID, job.workspaceID, job.file.Path, "file_summary", err)
		} else {
			summary = &knowledge.FileSummary{
				ID:          db.NewID(),
				Path:        job.file.Path,
				Summary:     text,
				ContentHash: job.file.Hash,
				Version:     1,
				Model:       c.SummaryModel,
				Status:      "active",
			}
		}
	}

	saved, err := c.DB.SaveFileEnrichment(ctx, job.workspaceID, job.file.Path, job.file.Hash, c.EmbeddingModel, job.chunks, summary)
	if err != nil {
		c.recordError(ctx, job.runID, job.workspaceID, job.file.Path, "enrichment_persist", err)
		return
	}
	if !saved {
		c.Broker.Emit(job.workspaceID, "enrichment_stale", map[string]any{"path": job.file.Path})
		return
	}
	if embedded > 0 {
		c.Broker.Emit(job.workspaceID, "embedding_created", map[string]any{"path": job.file.Path, "chunks": embedded})
	}
	if summary != nil {
		c.Broker.Emit(job.workspaceID, "summary_created", map[string]any{"summary_type": "file", "path": job.file.Path})
	}
}

// refreshModuleSummaries regenerates LLM summaries for each top-level module
// directory. Pass onlyModule="" to refresh all modules.
func (c *Coordinator) refreshModuleSummaries(ctx context.Context, runID, workspaceID string, summaries []knowledge.FileSummary, onlyModule string) {
	groups := map[string][]knowledge.FileSummary{}
	for _, item := range summaries {
		path := filepath.ToSlash(item.Path)
		module := "."
		if strings.Contains(path, "/") {
			module = strings.Split(path, "/")[0]
		}
		if onlyModule == "" || onlyModule == module {
			groups[module] = append(groups[module], item)
		}
	}
	for module, items := range groups {
		parts := make([]string, len(items))
		var hashes strings.Builder
		for i, item := range items {
			parts[i] = item.Path + ": " + item.Summary
			hashes.WriteString(item.ContentHash)
		}
		text, err := c.Models.SummarizeModule(ctx, module, parts)
		if err != nil {
			c.recordError(ctx, runID, workspaceID, module, "module_summary", err)
			continue
		}
		sum := sha256sum(hashes.String())
		if err := c.DB.SaveModuleSummary(ctx, workspaceID, knowledge.ModuleSummary{
			ID: db.NewID(), ModulePath: module, Summary: text,
			SourceHash: sum, Model: c.SummaryModel, Status: "active",
		}); err != nil {
			c.recordError(ctx, runID, workspaceID, module, "module_summary_persist", err)
			continue
		}
		c.Broker.Emit(workspaceID, "summary_created", map[string]any{"summary_type": "module", "path": module})
	}
}
