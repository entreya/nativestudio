package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/entreya/nativestudio/db"
)

type fakeModels struct{}

func (fakeModels) SummarizeSymbol(_ context.Context, _, name, _, _ string) (string, error) {
	return `{"purpose":"` + name + `"}`, nil
}

func (fakeModels) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{float32(i + 1), 1}
	}
	return out, nil
}
func (fakeModels) SummarizeFile(_ context.Context, path, language string, _ []string, _ []string) (string, error) {
	return `{"purpose":"Indexes ` + path + `","important_symbols":[],"dependencies":[],"important_behaviour":[],"side_effects":[]}`, nil
}
func (fakeModels) SummarizeModule(_ context.Context, _ string, _ []string) (string, error) {
	return `{}`, nil
}
func (fakeModels) SummarizeProject(_ context.Context, _ string, _ []string) (string, error) {
	return `{}`, nil
}

func TestIndexFileReplacesOnlyChangedFileAndStalesOldFacts(t *testing.T) {
	root := t.TempDir()
	database, err := db.Open(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	project, err := database.CreateProject("project", "Project", root, "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "package.json")
	first := `{"dependencies":{"react":"19"},"scripts":{"test":"vitest"}}`
	if err := os.WriteFile(path, []byte(first), 0644); err != nil {
		t.Fatal(err)
	}
	coordinator := &Coordinator{DB: database, Scanner: Scanner{Config: DefaultScanConfig()}, Parser: TreeSitterParser{Fallback: StructuralParser{}}, Chunker: SymbolChunker{}, Models: fakeModels{}, SummaryModel: "summary", EmbeddingModel: "embed", BatchSize: 2, Broker: NewBroker()}
	coordinator.IndexFile(context.Background(), project.ID, root, path)
	overview, overviewErr := database.KnowledgeOverview(context.Background(), project.ID)
	if overviewErr != nil {
		t.Fatal(overviewErr)
	}
	if overview.IndexedFiles != 1 || overview.Status.Status != "completed" {
		t.Fatalf("unexpected overview after indexing: %#v", overview)
	}
	var files int
	if err := database.QueryRow(`SELECT COUNT(*) FROM repository_files WHERE workspace_id=?`, project.ID).Scan(&files); err != nil || files != 1 {
		t.Fatalf("expected one indexed file, got %d: %v", files, err)
	}
	facts, err := database.ListFacts(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundReact := false
	for _, fact := range facts {
		if fact.Fact == "The project uses react" && fact.Status == "verified" {
			foundReact = true
		}
	}
	if !foundReact {
		t.Fatalf("React fact was not detected: %#v", facts)
	}
	second := `{"dependencies":{},"scripts":{"test":"vitest"}}`
	if err := os.WriteFile(path, []byte(second), 0644); err != nil {
		t.Fatal(err)
	}
	coordinator.IndexFile(context.Background(), project.ID, root, path)
	facts, err = database.ListFacts(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range facts {
		if fact.Fact == "The project uses react" && fact.Status != "stale" {
			t.Fatalf("old source-backed fact was not marked stale: %#v", fact)
		}
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM repository_files WHERE workspace_id=?`, project.ID).Scan(&files); err != nil || files != 1 {
		t.Fatalf("incremental indexing duplicated the file: %d, %v", files, err)
	}
}
