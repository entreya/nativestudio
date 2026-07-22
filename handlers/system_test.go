package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSystemBrowseFiltersAndSortsEntries(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "source"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"main.go": "package main", "notes.txt": "ignore", ".secret.go": "ignore"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	handler := NewSystemHandler([]string{".go"})
	request := httptest.NewRequest(http.MethodGet, "/api/system/browse?mode=file&path="+root, nil)
	request.RemoteAddr = "127.0.0.1:12345"
	recorder := httptest.NewRecorder()
	handler.Browse(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Entries []browserEntry `json:"entries"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Entries) != 2 || response.Entries[0].Name != "source" || response.Entries[1].Name != "main.go" {
		t.Fatalf("unexpected entries: %#v", response.Entries)
	}
}

func TestSystemBrowseRejectsNonLocalRequests(t *testing.T) {
	handler := NewSystemHandler([]string{".go"})
	request := httptest.NewRequest(http.MethodGet, "/api/system/browse", nil)
	request.RemoteAddr = "192.0.2.10:12345"
	recorder := httptest.NewRecorder()
	handler.Browse(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden, got %d", recorder.Code)
	}
}
