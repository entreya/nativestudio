package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/entreya/nativestudio/knowledge"
)

func TestKnowledgeQueryTermsIgnoreGreetingsAndShortNoise(t *testing.T) {
	if terms := knowledgeQueryTerms("hi"); len(terms) != 0 {
		t.Fatalf("greeting produced search terms: %#v", terms)
	}
	terms := knowledgeQueryTerms("Please explain the SiteController login action")
	want := []string{"explain", "sitecontroller", "login", "action"}
	if len(terms) != len(want) {
		t.Fatalf("unexpected terms: %#v", terms)
	}
	for index := range want {
		if terms[index] != want[index] {
			t.Fatalf("unexpected terms: %#v", terms)
		}
	}
}

func TestFTSSearchTracksFileReplacementAndRemoval(t *testing.T) {
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
	file := knowledge.RepositoryFile{ID: NewID(), Path: "controllers/SiteController.php", Language: "php", ContentHash: "hash-1"}
	symbol := knowledge.Symbol{ID: NewID(), Path: file.Path, Name: "SiteController", QualifiedName: "app\\controllers\\SiteController", Type: "class", Signature: "class SiteController", StartLine: 1, EndLine: 8, ContentHash: "symbol-1"}
	chunk := knowledge.Chunk{ID: NewID(), SymbolID: symbol.ID, Path: file.Path, StartLine: 1, EndLine: 8, Content: "class SiteController { public function actionLogin() { authenticateUser(); } }", ContentHash: "chunk-1"}
	if err := database.ReplaceIndexedFile(ctx, project.ID, file, []knowledge.Symbol{symbol}, []knowledge.Chunk{chunk}, nil, "embed"); err != nil {
		t.Fatal(err)
	}

	results, err := database.SearchKnowledge(ctx, project.ID, "find controller login authentication", 10)
	if err != nil {
		t.Fatal(err)
	}
	foundFTS := false
	for _, result := range results {
		if result.Path != file.Path {
			continue
		}
		for _, reason := range result.Reasons {
			if reason == "fts_candidate" {
				foundFTS = true
			}
		}
	}
	if !foundFTS {
		t.Fatalf("expected FTS-backed controller result, got %#v", results)
	}

	file.ContentHash = "hash-2"
	symbol.ID = NewID()
	symbol.Name = "AccountController"
	symbol.QualifiedName = "app\\controllers\\AccountController"
	symbol.Signature = "class AccountController"
	symbol.ContentHash = "symbol-2"
	chunk.ID = NewID()
	chunk.SymbolID = symbol.ID
	chunk.Content = "class AccountController { public function actionLogout() {} }"
	chunk.ContentHash = "chunk-2"
	if err := database.ReplaceIndexedFile(ctx, project.ID, file, []knowledge.Symbol{symbol}, []knowledge.Chunk{chunk}, nil, "embed"); err != nil {
		t.Fatal(err)
	}
	results, err = database.SearchKnowledge(ctx, project.ID, "authenticateUser", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.Path == file.Path && (result.Kind == "code" || result.Kind == "symbol") {
			t.Fatalf("stale FTS entry survived replacement: %#v", results)
		}
	}

	if err := database.RemoveIndexedFile(ctx, project.ID, file.Path); err != nil {
		t.Fatal(err)
	}
	var entries int
	if err := database.QueryRow(`SELECT COUNT(*) FROM knowledge_fts WHERE workspace_id=? AND path=?`, project.ID, file.Path).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 0 {
		t.Fatalf("expected FTS entries to be removed, got %d", entries)
	}
}

func TestFTSMigrationBackfillsExistingStructuralData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knowledge.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	project, err := database.CreateProject("project", "Project", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	fileID, symbolID, chunkID := NewID(), NewID(), NewID()
	if _, err := database.Exec(`INSERT INTO repository_files(id,workspace_id,path,language,content_hash,status) VALUES(?,?,?,'php','hash','active')`, fileID, project.ID, "LegacyController.php"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO symbols(id,workspace_id,file_id,path,symbol_name,symbol_type,start_line,end_line,content_hash,status) VALUES(?,?,?,?,?,'class',1,4,'symbol','active')`, symbolID, project.ID, fileID, "LegacyController.php", "LegacyController"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO code_chunks(id,workspace_id,file_id,symbol_id,path,start_line,end_line,content,content_hash,status) VALUES(?,?,?,?,?,1,4,?,'chunk','active')`, chunkID, project.ID, fileID, symbolID, "LegacyController.php", "class LegacyController { function migrateRecords() {} }"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DROP TABLE knowledge_fts`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM schema_migrations WHERE version='005_knowledge_fts.sql'`); err != nil {
		t.Fatal(err)
	}
	database.Close()

	database, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	results, err := database.SearchKnowledge(context.Background(), project.ID, "migrate records", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].Path != "LegacyController.php" {
		t.Fatalf("migration did not backfill searchable data: %#v", results)
	}
}
