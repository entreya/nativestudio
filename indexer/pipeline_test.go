package indexer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/entreya/nativestudio/db"
	"github.com/entreya/nativestudio/knowledge"
)

type blockingModels struct {
	started chan struct{}
	once    sync.Once
}

func (m *blockingModels) Embed(ctx context.Context, _ []string) ([][]float32, error) {
	m.once.Do(func() { close(m.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}
func (*blockingModels) SummarizeSymbol(context.Context, string, string, string, string) (string, error) {
	return "", errors.New("unexpected symbol summary")
}
func (*blockingModels) SummarizeFile(context.Context, string, string, []string, []string) (string, error) {
	return "", errors.New("unexpected file summary")
}
func (*blockingModels) SummarizeModule(context.Context, string, []string) (string, error) {
	return "", errors.New("unexpected module summary")
}
func (*blockingModels) SummarizeProject(context.Context, string, []string) (string, error) {
	return "", errors.New("unexpected project summary")
}

func TestStructuralIndexCompletesWhileModelEnrichmentIsBlocked(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	project, err := database.CreateProject("project", "Project", root, "")
	if err != nil {
		t.Fatal(err)
	}
	broker := NewBroker()
	events, unsubscribe := broker.Subscribe(project.ID)
	defer unsubscribe()
	models := &blockingModels{started: make(chan struct{})}
	coordinator := &Coordinator{DB: database, Scanner: Scanner{Config: DefaultScanConfig()}, Parser: TreeSitterParser{Fallback: StructuralParser{}}, Chunker: SymbolChunker{}, Models: models, SummaryModel: "summary", EmbeddingModel: "embed", BatchSize: 2, Broker: broker}
	coordinator.Start(project.ID, root)
	defer coordinator.Cancel()

	deadline := time.After(3 * time.Second)
	completed := false
	for !completed {
		select {
		case event := <-events:
			completed = event.Type == "index_completed"
		case <-deadline:
			t.Fatal("structural index waited for model enrichment")
		}
	}

	select {
	case <-models.started:
	case <-time.After(3 * time.Second):
		t.Fatal("background enrichment did not start")
	}
	var files, symbols int
	if err := database.QueryRow(`SELECT COUNT(*) FROM repository_files WHERE workspace_id=?`, project.ID).Scan(&files); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM symbols WHERE workspace_id=?`, project.ID).Scan(&symbols); err != nil {
		t.Fatal(err)
	}
	if files != 1 || symbols == 0 {
		t.Fatalf("expected structural data before enrichment, got files=%d symbols=%d", files, symbols)
	}
}

func TestWarmIndexReusesPersistentFileManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	project, err := database.CreateProject("project", "Project", root, "")
	if err != nil {
		t.Fatal(err)
	}

	newCoordinator := func(broker *Broker) *Coordinator {
		return &Coordinator{DB: database, Scanner: Scanner{Config: DefaultScanConfig()}, Parser: TreeSitterParser{Fallback: StructuralParser{}}, Chunker: SymbolChunker{}, Broker: broker}
	}
	firstBroker := NewBroker()
	firstEvents, firstUnsubscribe := firstBroker.Subscribe(project.ID)
	first := newCoordinator(firstBroker)
	first.Start(project.ID, root)
	waitForEvent(t, firstEvents, "structural_ready")
	first.Cancel()
	firstUnsubscribe()

	secondBroker := NewBroker()
	secondEvents, secondUnsubscribe := secondBroker.Subscribe(project.ID)
	defer secondUnsubscribe()
	second := newCoordinator(secondBroker)
	second.Start(project.ID, root)
	defer second.Cancel()
	event := waitForEvent(t, secondEvents, "structural_ready")
	data, ok := event.Data.(map[string]any)
	if !ok || data["reused"] != 1 {
		t.Fatalf("expected one manifest reuse on warm index, got %#v", event.Data)
	}
}

func TestRunningEnrichmentJobResumesAfterCoordinatorRestart(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	project, err := database.CreateProject("project", "Project", root, "")
	if err != nil {
		t.Fatal(err)
	}
	scanner := Scanner{Config: DefaultScanConfig()}
	file, skipped, err := scanner.ScanFile(context.Background(), root, path)
	if err != nil || skipped != nil {
		t.Fatalf("scan failed: skipped=%#v err=%v", skipped, err)
	}
	parser := TreeSitterParser{Fallback: StructuralParser{}}
	parsed, err := parser.Parse(context.Background(), file.Path, file.Content)
	if err != nil {
		t.Fatal(err)
	}
	chunks, err := (SymbolChunker{}).Chunk(parsed)
	if err != nil {
		t.Fatal(err)
	}
	record := knowledge.RepositoryFile{ID: db.NewID(), WorkspaceID: project.ID, Path: file.Path, Language: file.Language, SizeBytes: file.Size, ModifiedAtNS: file.ModifiedAtNS, ContentHash: file.Hash}
	if err := database.ReplaceIndexedFile(context.Background(), project.ID, record, parsed.Symbols, chunks, nil, "embed"); err != nil {
		t.Fatal(err)
	}
	runID, err := database.StartIndexRun(context.Background(), project.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.EnqueueIndexJob(context.Background(), project.ID, runID, file.Path, file.Hash, "file_enrichment", 100); err != nil {
		t.Fatal(err)
	}
	claimed, err := database.ClaimIndexJob(context.Background(), project.ID, "file_enrichment")
	if err != nil || claimed == nil {
		t.Fatalf("could not simulate interrupted job: job=%#v err=%v", claimed, err)
	}

	broker := NewBroker()
	events, unsubscribe := broker.Subscribe(project.ID)
	defer unsubscribe()
	coordinator := &Coordinator{DB: database, Scanner: scanner, Parser: parser, Chunker: SymbolChunker{}, Models: fakeModels{}, SummaryModel: "summary", EmbeddingModel: "embed", BatchSize: 2, Broker: broker}
	coordinator.Start(project.ID, root)
	defer coordinator.Cancel()
	waitForEvent(t, events, "enrichment_progress")
	var status string
	if err := database.QueryRow(`SELECT status FROM index_jobs WHERE id=?`, claimed.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "completed" {
		t.Fatalf("expected resumed job to complete, got %q", status)
	}
	var summaries int
	if err := database.QueryRow(`SELECT COUNT(*) FROM file_summaries WHERE workspace_id=? AND status='active'`, project.ID).Scan(&summaries); err != nil {
		t.Fatal(err)
	}
	if summaries != 1 {
		t.Fatalf("expected resumed enrichment to persist a summary, got %d", summaries)
	}
}

func waitForEvent(t *testing.T, events <-chan Event, eventType string) Event {
	t.Helper()
	deadline := time.After(4 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Type == eventType {
				return event
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %s", eventType)
		}
	}
}
