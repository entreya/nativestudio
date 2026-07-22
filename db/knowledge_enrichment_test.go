package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/entreya/nativestudio/knowledge"
)

func TestSaveFileEnrichmentRequiresCurrentContentHash(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	project, err := database.CreateProject("project", "Project", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	file := knowledge.RepositoryFile{ID: NewID(), Path: "main.go", Language: "go", ContentHash: "hash-1"}
	chunk := knowledge.Chunk{ID: NewID(), Path: file.Path, StartLine: 1, EndLine: 2, Content: "package main", ContentHash: "chunk-1"}
	if err := database.ReplaceIndexedFile(ctx, project.ID, file, nil, []knowledge.Chunk{chunk}, nil, "embed"); err != nil {
		t.Fatal(err)
	}

	chunk.Embedding = []float32{0.25, 0.75}
	summary := &knowledge.FileSummary{ID: NewID(), Path: file.Path, Summary: `{"purpose":"entry point"}`, ContentHash: file.ContentHash, Model: "summary"}
	saved, err := database.SaveFileEnrichment(ctx, project.ID, file.Path, file.ContentHash, "embed", []knowledge.Chunk{chunk}, summary)
	if err != nil || !saved {
		t.Fatalf("expected enrichment to be saved: saved=%v err=%v", saved, err)
	}
	var embeddings, summaries int
	if err := database.QueryRow(`SELECT COUNT(*) FROM embeddings WHERE workspace_id=?`, project.ID).Scan(&embeddings); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM file_summaries WHERE workspace_id=? AND status='active'`, project.ID).Scan(&summaries); err != nil {
		t.Fatal(err)
	}
	if embeddings != 1 || summaries != 1 {
		t.Fatalf("unexpected enrichment counts: embeddings=%d summaries=%d", embeddings, summaries)
	}

	newFile := file
	newFile.ID = NewID()
	newFile.ContentHash = "hash-2"
	newChunk := chunk
	newChunk.ID = NewID()
	newChunk.ContentHash = "chunk-2"
	newChunk.Embedding = nil
	if err := database.ReplaceIndexedFile(ctx, project.ID, newFile, nil, []knowledge.Chunk{newChunk}, nil, "embed"); err != nil {
		t.Fatal(err)
	}
	saved, err = database.SaveFileEnrichment(ctx, project.ID, file.Path, file.ContentHash, "embed", []knowledge.Chunk{chunk}, summary)
	if err != nil {
		t.Fatal(err)
	}
	if saved {
		t.Fatal("stale enrichment was accepted after the file changed")
	}
}

func TestOldJobCompletionCannotCompleteNewerContent(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	project, err := database.CreateProject("project", "Project", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := database.EnqueueIndexJob(ctx, project.ID, "", "main.go", "hash-1", "file_enrichment", 100); err != nil {
		t.Fatal(err)
	}
	oldJob, err := database.ClaimIndexJob(ctx, project.ID, "file_enrichment")
	if err != nil || oldJob == nil {
		t.Fatalf("claim old job: job=%#v err=%v", oldJob, err)
	}
	if err := database.EnqueueIndexJob(ctx, project.ID, "", "main.go", "hash-2", "file_enrichment", 100); err != nil {
		t.Fatal(err)
	}
	if err := database.CompleteIndexJob(ctx, oldJob.ID, oldJob.ContentHash, "completed", ""); err != nil {
		t.Fatal(err)
	}
	var hash, status string
	if err := database.QueryRow(`SELECT content_hash,status FROM index_jobs WHERE id=?`, oldJob.ID).Scan(&hash, &status); err != nil {
		t.Fatal(err)
	}
	if hash != "hash-2" || status != "queued" {
		t.Fatalf("new job was overwritten by old completion: hash=%q status=%q", hash, status)
	}
}
