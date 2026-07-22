package indexer

import (
	"context"
	"fmt"
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

func (c *Coordinator) enrichmentLoop(ctx context.Context, workspaceID, root string, jobs <-chan struct{}) {
	var aggregateTimer *time.Timer
	var aggregateC <-chan time.Time
	lastRunID := ""
	completed := 0
	poll := time.NewTicker(2 * time.Second)

	resetAggregateTimer := func() {
		if aggregateTimer == nil {
			aggregateTimer = time.NewTimer(2 * time.Second)
		} else {
			if !aggregateTimer.Stop() {
				select {
				case <-aggregateTimer.C:
				default:
				}
			}
			aggregateTimer.Reset(2 * time.Second)
		}
		aggregateC = aggregateTimer.C
	}

	defer func() {
		poll.Stop()
		if aggregateTimer != nil {
			aggregateTimer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
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
				c.refreshModuleSummaries(ctx, lastRunID, workspaceID, summaries, "")
				_ = c.refreshProjectSummary(ctx, lastRunID, workspaceID, root, summaries)
			}
			c.Broker.Emit(workspaceID, "enrichment_completed", map[string]any{"completed": completed, "remaining": 0})
			completed = 0
			continue
		}

		processedAny := false
		for {
			claimed, err := c.DB.ClaimIndexJob(ctx, workspaceID, "file_enrichment")
			if err != nil {
				c.Broker.Emit(workspaceID, "enrichment_error", map[string]any{"error": err.Error()})
				break
			}
			if claimed == nil {
				break
			}
			lastRunID = claimed.RunID
			file, symbols, chunks, current, loadErr := c.DB.LoadFileForEnrichment(ctx, workspaceID, claimed.Path, claimed.ContentHash)
			if loadErr != nil {
				_ = c.DB.CompleteIndexJob(ctx, claimed.ID, claimed.ContentHash, "failed", loadErr.Error())
				c.Broker.Emit(workspaceID, "enrichment_error", map[string]any{"path": claimed.Path, "error": loadErr.Error()})
				continue
			}
			if !current {
				_ = c.DB.CompleteIndexJob(ctx, claimed.ID, claimed.ContentHash, "stale", "file content changed")
				c.Broker.Emit(workspaceID, "enrichment_stale", map[string]any{"path": claimed.Path})
				continue
			}
			remaining, _ := c.DB.PendingIndexJobCount(ctx, workspaceID, "file_enrichment")
			c.Broker.Emit(workspaceID, "enrichment_started", map[string]any{"path": claimed.Path, "remaining": remaining})
			jobStartedAt := time.Now()
			job := enrichmentJob{runID: claimed.RunID, workspaceID: workspaceID, root: root, file: ScannedFile{Path: file.Path, Language: file.Language, Hash: file.ContentHash, GitStatus: file.GitStatus, Size: file.SizeBytes, ModifiedAtNS: file.ModifiedAtNS}, symbols: symbols, chunks: chunks}
			c.enrichFile(ctx, job)
			if ctx.Err() != nil {
				return
			}
			_ = c.DB.CompleteIndexJob(ctx, claimed.ID, claimed.ContentHash, "completed", "")
			completed++
			processedAny = true
			remaining, _ = c.DB.PendingIndexJobCount(ctx, workspaceID, "file_enrichment")
			c.Broker.Emit(workspaceID, "enrichment_progress", map[string]any{"path": claimed.Path, "completed": completed, "remaining": remaining, "duration_ms": time.Since(jobStartedAt).Milliseconds()})
		}
		if processedAny {
			resetAggregateTimer()
		}
	}
}

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
	var summary *knowledge.FileSummary
	text, err := c.Models.SummarizeFile(ctx, job.file.Path, job.file.Language, names, excerpts)
	if err != nil {
		c.recordError(ctx, job.runID, job.workspaceID, job.file.Path, "file_summary", err)
	} else {
		summary = &knowledge.FileSummary{ID: db.NewID(), Path: job.file.Path, Summary: text, ContentHash: job.file.Hash, Version: 1, Model: c.SummaryModel, Status: "active"}
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
