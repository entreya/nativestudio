package indexer

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/entreya/nativestudio/db"
	"github.com/entreya/nativestudio/knowledge"
)

type ModelService interface {
	Embed(context.Context, []string) ([][]float32, error)
	SummarizeSymbol(context.Context, string, string, string, string) (string, error)
	SummarizeFile(context.Context, string, string, []string, []string) (string, error)
	SummarizeModule(context.Context, string, []string) (string, error)
	SummarizeProject(context.Context, string, []string) (string, error)
}

type Coordinator struct {
	DB                           *db.DB
	Scanner                      Scanner
	Parser                       knowledge.CodeParser
	Chunker                      knowledge.Chunker
	Models                       ModelService
	SummaryModel, EmbeddingModel string
	BatchSize                    int
	Broker                       *Broker
	mu                           sync.Mutex
	cancel                       context.CancelFunc
	activeWorkspaceID            string
	activeRoot                   string
	generation                   uint64
	fileMu                       sync.Mutex
	enrichmentJobs               chan struct{}
	// enrichmentWorkspaceID/enrichmentApproved gate the enrichment worker:
	// scanning always starts automatically, but enrichment (which calls the
	// AI model) waits for an explicit user confirmation via
	// ApproveEnrichment before it processes any queued file_enrichment job.
	// These are deliberately separate from activeWorkspaceID/activeRoot,
	// which the scan goroutine clears the moment *scanning* finishes —
	// enrichment is a longer-running, independent goroutine that keeps going
	// well after that, so gating approval on activeWorkspaceID would (and
	// initially did) make every approval silently no-op as soon as the scan
	// portion completed, which is usually well before enrichment even starts.
	// Reset on every start()/Cancel() so reopening a project with leftover
	// queued jobs asks again rather than silently resuming.
	enrichmentWorkspaceID string
	enrichmentApproved    bool
}

func (c *Coordinator) Start(workspaceID, root string) {
	c.start(workspaceID, root, false)
}

// Restart explicitly replaces the active watcher/index, even for the same workspace.
func (c *Coordinator) Restart(workspaceID, root string) {
	c.start(workspaceID, root, true)
}

func (c *Coordinator) start(workspaceID, root string, force bool) {
	c.mu.Lock()
	if !force && c.cancel != nil && c.activeWorkspaceID == workspaceID && c.activeRoot == root {
		c.mu.Unlock()
		return
	}
	if c.cancel != nil {
		c.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	jobs := make(chan struct{}, 1)
	c.cancel = cancel
	c.activeWorkspaceID = workspaceID
	c.activeRoot = root
	c.enrichmentWorkspaceID = workspaceID
	c.enrichmentApproved = false
	c.generation++
	generation := c.generation
	c.enrichmentJobs = jobs
	scanner := c.Scanner
	c.mu.Unlock()
	_ = c.DB.RequeueRunningIndexJobs(context.Background(), workspaceID, "file_enrichment")
	go c.enrichmentLoop(ctx, workspaceID, root, jobs)
	select {
	case jobs <- struct{}{}:
	default:
	}
	go func() {
		defer cancel()
		c.index(ctx, workspaceID, root, scanner)
		c.mu.Lock()
		if c.generation == generation {
			c.cancel = nil
			c.activeWorkspaceID = ""
			c.activeRoot = ""
			c.enrichmentJobs = nil
		}
		c.mu.Unlock()
	}()
}
func (c *Coordinator) Configure(settings knowledge.IndexSettings) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if settings.MaximumFileSizeBytes > 0 {
		c.Scanner.Config.MaximumFileSizeBytes = settings.MaximumFileSizeBytes
	}
	c.Scanner.Config.IncludedPaths = append([]string{}, settings.IncludePaths...)
	if len(settings.ExcludePaths) > 0 {
		c.Scanner.Config.ExcludedDirectories = append([]string{}, settings.ExcludePaths...)
	}
	if len(settings.SensitivePatterns) > 0 {
		c.Scanner.Config.SensitivePatterns = append([]string{}, settings.SensitivePatterns...)
	}
}
func (c *Coordinator) scannerSnapshot() Scanner { c.mu.Lock(); defer c.mu.Unlock(); return c.Scanner }
func (c *Coordinator) Cancel() {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
	}
	c.activeWorkspaceID = ""
	c.activeRoot = ""
	c.enrichmentWorkspaceID = ""
	c.enrichmentApproved = false
	c.mu.Unlock()
}

// ApproveEnrichment unblocks the enrichment worker for workspaceID, letting
// it start (or resume) processing queued file_enrichment jobs. A no-op if
// workspaceID isn't the currently active workspace (e.g. a stale approval
// from a project the user has since navigated away from).
func (c *Coordinator) ApproveEnrichment(workspaceID string) {
	c.mu.Lock()
	if c.enrichmentWorkspaceID != workspaceID {
		c.mu.Unlock()
		return
	}
	c.enrichmentApproved = true
	jobs := c.enrichmentJobs
	c.mu.Unlock()
	if jobs != nil {
		select {
		case jobs <- struct{}{}:
		default:
		}
	}
}

// IsEnrichmentApproved reports whether enrichment has been approved for
// workspaceID. Used by the status endpoint to report "pending_confirmation"
// instead of "running" while queued jobs are waiting on user approval.
func (c *Coordinator) IsEnrichmentApproved(workspaceID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enrichmentWorkspaceID == workspaceID && c.enrichmentApproved
}

func (c *Coordinator) index(ctx context.Context, workspaceID, root string, scanner Scanner) {
	startedAt := time.Now()
	c.Broker.Emit(workspaceID, "watch_started", map[string]any{"workspace_id": workspaceID})
	go (Watcher{Scanner: scanner}).Run(ctx, root, func(path string) { c.IndexFile(ctx, workspaceID, root, path) }, func(path string) { c.RemoveFile(ctx, workspaceID, root, path) })

	scanStartedAt := time.Now()
	files, skipped, err := scanner.Discover(ctx, root)
	if err != nil {
		c.Broker.Emit(workspaceID, "index_error", map[string]any{"error": err.Error()})
		return
	}
	c.Broker.Emit(workspaceID, "scan_completed", map[string]any{"files": len(files), "skipped": len(skipped), "duration_ms": time.Since(scanStartedAt).Milliseconds()})
	manifest, err := c.DB.RepositoryManifest(ctx, workspaceID)
	if err != nil {
		c.Broker.Emit(workspaceID, "index_error", map[string]any{"error": err.Error()})
		return
	}
	total := len(files) + len(skipped)
	runID, err := c.DB.StartIndexRun(ctx, workspaceID, total)
	if err != nil {
		return
	}
	c.Broker.Emit(workspaceID, "index_started", map[string]any{"workspace_id": workspaceID, "run_id": runID, "total": total})
	c.progress(ctx, runID, workspaceID, 0, total, len(skipped), 0)
	processed, errors, changed, reused := 0, 0, 0, 0
	for _, item := range skipped {
		_ = c.DB.SaveIndexSkip(ctx, runID, workspaceID, item.Path, item.Reason)
		c.Broker.Emit(workspaceID, "file_skipped", map[string]any{"path": item.Path, "reason": item.Reason})
	}
	for _, metadata := range files {
		select {
		case <-ctx.Done():
			_ = c.DB.UpdateIndexRun(context.Background(), runID, "cancelled", processed, len(skipped), errors, ctx.Err().Error())
			c.Broker.Emit(workspaceID, "index_cancelled", map[string]any{"run_id": runID})
			return
		default:
		}
		c.Broker.Emit(workspaceID, "file_discovered", map[string]any{"path": metadata.Path})
		previous, exists := manifest[metadata.Path]
		if exists && previous.ModifiedAtNS != 0 && previous.SizeBytes == metadata.Size && previous.ModifiedAtNS == metadata.ModifiedAtNS {
			if previous.GitStatus != metadata.GitStatus {
				_ = c.DB.UpdateRepositoryFileMetadata(ctx, workspaceID, metadata.Path, metadata.GitStatus, metadata.Size, metadata.ModifiedAtNS)
			}
			processed++
			reused++
			c.progress(ctx, runID, workspaceID, processed, total, len(skipped), errors)
			continue
		}
		file, readSkip, readErr := scanner.ReadDiscoveredFile(ctx, root, metadata)
		if readErr != nil {
			errors++
			c.recordError(ctx, runID, workspaceID, metadata.Path, "read", readErr)
			processed++
			c.progress(ctx, runID, workspaceID, processed, total, len(skipped), errors)
			continue
		}
		if readSkip != nil {
			skipped = append(skipped, *readSkip)
			_ = c.DB.SaveIndexSkip(ctx, runID, workspaceID, readSkip.Path, readSkip.Reason)
			c.Broker.Emit(workspaceID, "file_skipped", map[string]any{"path": readSkip.Path, "reason": readSkip.Reason})
			c.progress(ctx, runID, workspaceID, processed, total, len(skipped), errors)
			continue
		}
		if exists && previous.ContentHash == file.Hash {
			if metadataErr := c.DB.UpdateRepositoryFileMetadata(ctx, workspaceID, file.Path, file.GitStatus, file.Size, file.ModifiedAtNS); metadataErr != nil {
				errors++
				c.recordError(ctx, runID, workspaceID, file.Path, "metadata", metadataErr)
			}
			processed++
			reused++
			c.progress(ctx, runID, workspaceID, processed, total, len(skipped), errors)
			continue
		}
		c.fileMu.Lock()
		parsed, parseErr := c.Parser.Parse(ctx, file.Path, file.Content)
		if parseErr != nil {
			c.fileMu.Unlock()
			errors++
			c.recordError(ctx, runID, workspaceID, file.Path, "parse", parseErr)
			continue
		}
		chunks, chunkErr := c.Chunker.Chunk(parsed)
		if chunkErr != nil {
			c.fileMu.Unlock()
			errors++
			c.recordError(ctx, runID, workspaceID, file.Path, "chunk", chunkErr)
			continue
		}
		c.Broker.Emit(workspaceID, "file_parsed", map[string]any{"path": file.Path, "symbols": len(parsed.Symbols), "chunks": len(chunks)})
		record := knowledge.RepositoryFile{ID: db.NewID(), WorkspaceID: workspaceID, Path: file.Path, Language: file.Language, SizeBytes: file.Size, ModifiedAtNS: file.ModifiedAtNS, ContentHash: file.Hash, GitStatus: file.GitStatus}
		if saveErr := c.DB.ReplaceIndexedFile(ctx, workspaceID, record, parsed.Symbols, chunks, nil, c.EmbeddingModel); saveErr != nil {
			c.fileMu.Unlock()
			errors++
			c.recordError(ctx, runID, workspaceID, file.Path, "persist", saveErr)
			continue
		}
		if relationshipErr := c.DB.ReplaceFileRelationships(ctx, workspaceID, file.Path, parsed.Imports); relationshipErr != nil {
			errors++
			c.recordError(ctx, runID, workspaceID, file.Path, "relationships", relationshipErr)
		}
		for _, fact := range DetectFacts(*file) {
			if factErr := c.DB.UpsertVerifiedFact(ctx, workspaceID, fact); factErr != nil {
				errors++
				c.recordError(ctx, runID, workspaceID, file.Path, "facts", factErr)
			}
		}
		c.fileMu.Unlock()
		c.enqueueEnrichment(ctx, enrichmentJob{runID: runID, workspaceID: workspaceID, root: root, file: *file})
		changed++
		processed++
		c.progress(ctx, runID, workspaceID, processed, total, len(skipped), errors)
	}
	discovered := map[string]bool{}
	for _, file := range files {
		discovered[file.Path] = true
	}
	if storedPaths, pathErr := c.DB.RepositoryPaths(ctx, workspaceID); pathErr == nil {
		for _, path := range storedPaths {
			if !discovered[path] {
				_ = c.DB.RemoveIndexedFile(ctx, workspaceID, path)
				changed++
				c.Broker.Emit(workspaceID, "file_removed", map[string]any{"path": path, "reason": "missing_during_scan"})
			}
		}
	}
	status := "completed"
	message := ""
	if errors > 0 {
		status = "completed_with_errors"
		message = fmt.Sprintf("%d indexing operations failed", errors)
	}
	_ = c.DB.UpdateIndexRun(ctx, runID, status, processed, len(skipped), errors, message)
	c.Broker.Emit(workspaceID, "index_completed", map[string]any{"processed": processed, "skipped": len(skipped), "errors": errors, "run_id": runID})
	c.Broker.Emit(workspaceID, "structural_ready", map[string]any{"processed": processed, "changed": changed, "reused": reused, "run_id": runID, "duration_ms": time.Since(startedAt).Milliseconds()})
	<-ctx.Done()
}

func (c *Coordinator) refreshProjectSummary(ctx context.Context, runID, workspaceID, root string, summaries []knowledge.FileSummary) int {
	modules, _ := c.DB.ModuleSummaries(ctx, workspaceID)
	parts := make([]string, 0, len(modules))
	var hashes strings.Builder
	for _, item := range summaries {
		hashes.WriteString(item.ContentHash)
	}
	for _, module := range modules {
		parts = append(parts, module.ModulePath+": "+module.Summary)
	}
	if len(parts) == 0 {
		for _, item := range summaries {
			parts = append(parts, item.Path+": "+item.Summary)
		}
	}
	text, err := c.Models.SummarizeProject(ctx, filepath.Base(root), parts)
	if err != nil {
		c.recordError(ctx, runID, workspaceID, "", "project_summary", err)
		return 1
	}
	if err := c.DB.SaveProjectSummary(ctx, workspaceID, knowledge.ProjectSummary{
		ID: db.NewID(), Summary: text,
		SourceHash: sha256sum(hashes.String()),
		Model:      c.SummaryModel, Status: "active",
	}); err != nil {
		c.recordError(ctx, runID, workspaceID, "", "project_summary_persist", err)
		return 1
	}
	c.Broker.Emit(workspaceID, "summary_created", map[string]any{"summary_type": "project"})
	return 0
}
func (c *Coordinator) progress(ctx context.Context, runID, workspaceID string, processed, total, skipped, errors int) {
	_ = c.DB.UpdateIndexRun(ctx, runID, "running", processed, skipped, errors, "")
	c.Broker.Emit(workspaceID, "index_progress", map[string]any{"processed": processed, "total": total, "skipped": skipped, "errors": errors})
}
func (c *Coordinator) recordError(ctx context.Context, runID, workspaceID, path, stage string, err error) {
	_ = c.DB.SaveIndexError(ctx, runID, workspaceID, path, stage, err)
	c.Broker.Emit(workspaceID, "index_error", map[string]any{"path": path, "stage": stage, "error": err.Error()})
}
