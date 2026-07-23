package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/entreya/nativestudio/db"
)

func newTestProjectsHandler(t *testing.T) *ProjectsHandler {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "projects-handler.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return NewProjectsHandler(database)
}

func createProject(t *testing.T, handler *ProjectsHandler, name, path string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name, "path": path, "description": ""})
	request := httptest.NewRequest(http.MethodPost, "/api/projects", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	handler.HandleCreateProject(recorder, request)
	var data map[string]any
	json.Unmarshal(recorder.Body.Bytes(), &data)
	return recorder, data
}

func TestCreateProjectReusesExistingProjectForSamePath(t *testing.T) {
	handler := newTestProjectsHandler(t)
	path := "/Users/example/some-folder"

	first, firstData := createProject(t, handler, "some-folder", path)
	if first.Code != http.StatusOK {
		t.Fatalf("first create: status=%d body=%s", first.Code, first.Body.String())
	}
	firstProject := firstData["project"].(map[string]any)

	second, secondData := createProject(t, handler, "some-folder", path)
	if second.Code != http.StatusOK {
		t.Fatalf("expected re-opening an existing project path to succeed, got status=%d body=%s", second.Code, second.Body.String())
	}
	secondProject := secondData["project"].(map[string]any)

	if firstProject["id"] != secondProject["id"] {
		t.Fatalf("expected the same project to be reused, got ids %v and %v", firstProject["id"], secondProject["id"])
	}
}

func TestCreateProjectAllowsDistinctPaths(t *testing.T) {
	handler := newTestProjectsHandler(t)

	first, firstData := createProject(t, handler, "a", "/Users/example/a")
	if first.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", first.Code, first.Body.String())
	}
	second, secondData := createProject(t, handler, "b", "/Users/example/b")
	if second.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", second.Code, second.Body.String())
	}

	firstProject := firstData["project"].(map[string]any)
	secondProject := secondData["project"].(map[string]any)
	if firstProject["id"] == secondProject["id"] {
		t.Fatal("expected distinct paths to create distinct projects")
	}
}
