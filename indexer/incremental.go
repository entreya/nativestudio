package indexer

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/entreya/nativestudio/db"
	"github.com/entreya/nativestudio/knowledge"
	workspacefs "github.com/entreya/nativestudio/workspace"
)

func (c *Coordinator) IndexFile(ctx context.Context, workspaceID, root, path string) {
	c.fileMu.Lock()
	defer c.fileMu.Unlock()
	file, skipped, scanErr := c.scannerSnapshot().ScanFile(ctx, root, path)
	runID, runErr := c.DB.StartIncrementalIndexRun(ctx, workspaceID)
	if runErr != nil {
		return
	}
	if scanErr != nil {
		c.recordError(ctx, runID, workspaceID, path, "scan", scanErr)
		_ = c.DB.UpdateIndexRun(ctx, runID, "failed", 0, 0, 1, scanErr.Error())
		return
	}
	if skipped != nil {
		_ = c.DB.SaveIndexSkip(ctx, runID, workspaceID, skipped.Path, skipped.Reason)
		c.Broker.Emit(workspaceID, "file_skipped", map[string]any{"path": skipped.Path, "reason": skipped.Reason})
		_ = c.DB.UpdateIndexRun(ctx, runID, "completed", 0, 1, 0, "")
		return
	}
	errors := c.persistIncrementalFile(ctx, runID, workspaceID, root, *file)
	status := "completed"
	message := ""
	if errors > 0 {
		status = "completed_with_errors"
		message = fmt.Sprintf("%d indexing operations failed", errors)
	}
	_ = c.DB.UpdateIndexRun(ctx, runID, status, 1, 0, errors, message)
	c.Broker.Emit(workspaceID, "index_completed", map[string]any{"processed": 1, "skipped": 0, "errors": errors, "run_id": runID, "incremental": true})
}

func (c *Coordinator) RemoveFile(ctx context.Context, workspaceID, root, path string) {
	guard := workspacefs.Guard{Root: root}
	rel, err := guard.Relative(path)
	if err != nil {
		return
	}
	if err := c.DB.RemoveIndexedFile(ctx, workspaceID, rel); err == nil {
		c.Broker.Emit(workspaceID, "file_removed", map[string]any{"path": rel})
	}
}

func (c *Coordinator) persistIncrementalFile(ctx context.Context, runID, workspaceID, root string, file ScannedFile) int {
	c.Broker.Emit(workspaceID, "file_discovered", map[string]any{"path": file.Path, "incremental": true})
	oldHash, err := c.DB.RepositoryFileHash(ctx, workspaceID, file.Path)
	if err == nil && oldHash == file.Hash {
		return 0
	}
	if err != nil && err != sql.ErrNoRows {
		c.recordError(ctx, runID, workspaceID, file.Path, "lookup", err)
		return 1
	}
	parsed, err := c.Parser.Parse(ctx, file.Path, file.Content)
	if err != nil {
		c.recordError(ctx, runID, workspaceID, file.Path, "parse", err)
		return 1
	}
	chunks, err := c.Chunker.Chunk(parsed)
	if err != nil {
		c.recordError(ctx, runID, workspaceID, file.Path, "chunk", err)
		return 1
	}
	c.Broker.Emit(workspaceID, "file_parsed", map[string]any{"path": file.Path, "symbols": len(parsed.Symbols), "chunks": len(chunks), "incremental": true})
	errors := 0
	record := knowledge.RepositoryFile{ID: db.NewID(), WorkspaceID: workspaceID, Path: file.Path, Language: file.Language, SizeBytes: file.Size, ModifiedAtNS: file.ModifiedAtNS, ContentHash: file.Hash, GitStatus: file.GitStatus}
	if saveErr := c.DB.ReplaceIndexedFile(ctx, workspaceID, record, parsed.Symbols, chunks, nil, c.EmbeddingModel); saveErr != nil {
		errors++
		c.recordError(ctx, runID, workspaceID, file.Path, "persist", saveErr)
		return errors
	}
	if relationErr := c.DB.ReplaceFileRelationships(ctx, workspaceID, file.Path, parsed.Imports); relationErr != nil {
		errors++
		c.recordError(ctx, runID, workspaceID, file.Path, "relationships", relationErr)
	}
	for _, fact := range DetectFacts(file) {
		if factErr := c.DB.UpsertVerifiedFact(ctx, workspaceID, fact); factErr != nil {
			errors++
			c.recordError(ctx, runID, workspaceID, file.Path, "facts", factErr)
		}
	}
	c.enqueueEnrichment(ctx, enrichmentJob{runID: runID, workspaceID: workspaceID, root: root, file: file})
	return errors
}
