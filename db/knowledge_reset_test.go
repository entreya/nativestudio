package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestResetProjectKnowledgeClearsDerivedDataOnly(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "knowledge-reset.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	project, err := database.CreateProject("project-reset", "sample", "/work/sample", "")
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`INSERT INTO repository_files(id,workspace_id,path,language,size_bytes,content_hash) VALUES('file-1',?,'main.go','go',10,'hash')`,
		`INSERT INTO module_summaries(id,workspace_id,module_path,summary,source_hash) VALUES('module-1',?,'.','{}','hash')`,
		`INSERT INTO project_summaries(id,workspace_id,summary,source_hash) VALUES('summary-1',?,'{}','hash')`,
		`INSERT INTO project_facts(id,workspace_id,category,fact) VALUES('fact-1',?,'runtime','Go service')`,
		`INSERT INTO indexing_runs(id,workspace_id,status) VALUES('run-1',?,'completed')`,
		`INSERT INTO project_index_settings(workspace_id) VALUES(?)`,
	}
	for _, statement := range statements {
		if _, err := database.Exec(statement, project.ID); err != nil {
			t.Fatal(err)
		}
	}

	if err := database.ResetProjectKnowledge(context.Background(), project.ID); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"repository_files", "module_summaries", "project_summaries", "project_facts", "indexing_runs"} {
		var count int
		if err := database.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE workspace_id=?", project.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("expected %s to be cleared, got %d rows", table, count)
		}
	}
	var settings int
	if err := database.QueryRow(`SELECT COUNT(*) FROM project_index_settings WHERE workspace_id=?`, project.ID).Scan(&settings); err != nil {
		t.Fatal(err)
	}
	if settings != 1 {
		t.Fatalf("expected indexing settings to be preserved, got %d rows", settings)
	}
}
