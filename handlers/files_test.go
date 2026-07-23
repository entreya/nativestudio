package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileReturnsWorkspaceRelativeContent(t *testing.T) {
	root := t.TempDir()
	controllerDir := filepath.Join(root, "controllers")
	if err := os.MkdirAll(controllerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := "<?php\nclass SiteController {}\n"
	if err := os.WriteFile(filepath.Join(controllerDir, "SiteController.php"), []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := NewFileHandler(root, []string{".php"})
	request := httptest.NewRequest(http.MethodGet, "/api/file?path=%2Fcontrollers%2FSiteController.php", nil)
	response := httptest.NewRecorder()
	handler.ReadFile(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "class SiteController") {
		t.Fatalf("file content was not returned: %s", response.Body.String())
	}
}

func listFiles(t *testing.T, handler *FileHandler, path string) []FileEntry {
	t.Helper()
	url := "/api/files"
	if path != "" {
		url += "?path=" + path
	}
	request := httptest.NewRequest(http.MethodGet, url, nil)
	response := httptest.NewRecorder()
	handler.ListFiles(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
	}
	var entries []FileEntry
	if err := json.Unmarshal(response.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestListFilesReturnsOneLevelAtATime(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "controllers"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "controllers", "SiteController.php"), []byte("<?php"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("ignore"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := NewFileHandler(root, []string{".go", ".php"})

	rootEntries := listFiles(t, handler, "")
	if len(rootEntries) != 2 || rootEntries[0].Name != "controllers" || rootEntries[0].Type != "dir" || !rootEntries[0].HasChildren {
		t.Fatalf("unexpected root listing: %#v", rootEntries)
	}
	if rootEntries[1].Name != "main.go" {
		t.Fatalf("unexpected root listing: %#v", rootEntries)
	}

	nested := listFiles(t, handler, "/controllers")
	if len(nested) != 1 || nested[0].Name != "SiteController.php" || nested[0].Type != "file" {
		t.Fatalf("unexpected nested listing: %#v", nested)
	}
}

func TestListFilesExcludesDependencyDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "leftpad"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "leftpad", "index.js"), []byte("module.exports={}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}

	handler := NewFileHandler(root, []string{".js"})
	entries := listFiles(t, handler, "")
	if len(entries) != 1 || entries[0].Name != "app.js" {
		t.Fatalf("expected node_modules to be excluded, got: %#v", entries)
	}
}

func TestListFilesFollowsSymlinkedDirectory(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real-dir")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "linked.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, filepath.Join(root, "linked-dir")); err != nil {
		t.Skipf("symlinks unavailable in this environment: %v", err)
	}

	handler := NewFileHandler(root, []string{".go"})
	entries := listFiles(t, handler, "")
	var linked *FileEntry
	for i := range entries {
		if entries[i].Name == "linked-dir" {
			linked = &entries[i]
		}
	}
	if linked == nil || linked.Type != "dir" || !linked.HasChildren {
		t.Fatalf("expected linked-dir to be listed as a dir with children, got %#v", entries)
	}
}

func postJSON(t *testing.T, handlerFunc http.HandlerFunc, body any) *httptest.ResponseRecorder {
	t.Helper()
	data, _ := json.Marshal(body)
	request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(data))
	recorder := httptest.NewRecorder()
	handlerFunc(recorder, request)
	return recorder
}

func TestCreateEntryCreatesFileAndDirectory(t *testing.T) {
	root := t.TempDir()
	handler := NewFileHandler(root, []string{".go"})

	recorder := postJSON(t, handler.CreateEntry, map[string]string{"path": "src/main.go", "type": "file"})
	if recorder.Code != http.StatusOK {
		t.Fatalf("create file: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "src", "main.go")); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}

	recorder = postJSON(t, handler.CreateEntry, map[string]string{"path": "src/nested", "type": "dir"})
	if recorder.Code != http.StatusOK {
		t.Fatalf("create dir: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	info, err := os.Stat(filepath.Join(root, "src", "nested"))
	if err != nil || !info.IsDir() {
		t.Fatalf("expected directory to exist: %v", err)
	}
}

func TestCreateEntryRejectsExistingAndDisallowedExtension(t *testing.T) {
	root := t.TempDir()
	handler := NewFileHandler(root, []string{".go"})

	if r := postJSON(t, handler.CreateEntry, map[string]string{"path": "main.exe", "type": "file"}); r.Code != http.StatusForbidden {
		t.Fatalf("expected disallowed extension to be rejected, got %d", r.Code)
	}

	postJSON(t, handler.CreateEntry, map[string]string{"path": "main.go", "type": "file"})
	if r := postJSON(t, handler.CreateEntry, map[string]string{"path": "main.go", "type": "file"}); r.Code != http.StatusConflict {
		t.Fatalf("expected creating an existing file to conflict, got %d", r.Code)
	}
}

func TestCreateEntryRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	handler := NewFileHandler(root, []string{".go"})
	if r := postJSON(t, handler.CreateEntry, map[string]string{"path": "../../etc/evil.go", "type": "file"}); r.Code == http.StatusOK {
		t.Fatal("expected path traversal to be rejected")
	}
}

func TestRenameEntryMovesFileAndRejectsCollision(t *testing.T) {
	root := t.TempDir()
	handler := NewFileHandler(root, []string{".go"})
	if err := os.WriteFile(filepath.Join(root, "old.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}

	recorder := postJSON(t, handler.RenameEntry, map[string]string{"path": "old.go", "new_path": "renamed/new.go"})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "renamed", "new.go")); err != nil {
		t.Fatalf("expected file at new path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "old.go")); err == nil {
		t.Fatal("expected the old path to no longer exist")
	}

	if err := os.WriteFile(filepath.Join(root, "other.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := postJSON(t, handler.RenameEntry, map[string]string{"path": "other.go", "new_path": "renamed/new.go"}); r.Code != http.StatusConflict {
		t.Fatalf("expected renaming onto an existing path to conflict, got %d", r.Code)
	}
}

func TestRenameEntryRejectsWorkspaceRootAndTraversal(t *testing.T) {
	root := t.TempDir()
	handler := NewFileHandler(root, []string{".go"})
	if r := postJSON(t, handler.RenameEntry, map[string]string{"path": ".", "new_path": "somewhere.go"}); r.Code == http.StatusOK {
		t.Fatal("expected renaming the workspace root to be rejected")
	}
	if r := postJSON(t, handler.RenameEntry, map[string]string{"path": "../../etc/passwd", "new_path": "x.go"}); r.Code == http.StatusOK {
		t.Fatal("expected traversal in the source path to be rejected")
	}
}

func TestDeleteEntryRemovesFileAndDirectory(t *testing.T) {
	root := t.TempDir()
	handler := NewFileHandler(root, []string{".go"})
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "a.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "single.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if r := postJSON(t, handler.DeleteEntry, map[string]string{"path": "single.go"}); r.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", r.Code, r.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "single.go")); err == nil {
		t.Fatal("expected file to be removed")
	}

	if r := postJSON(t, handler.DeleteEntry, map[string]string{"path": "pkg"}); r.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", r.Code, r.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "pkg")); err == nil {
		t.Fatal("expected directory to be recursively removed")
	}
}

func TestDeleteEntryRejectsWorkspaceRootAndTraversal(t *testing.T) {
	root := t.TempDir()
	handler := NewFileHandler(root, []string{".go"})
	if r := postJSON(t, handler.DeleteEntry, map[string]string{"path": "."}); r.Code == http.StatusOK {
		t.Fatal("expected deleting the workspace root to be rejected")
	}
	if r := postJSON(t, handler.DeleteEntry, map[string]string{"path": "../../etc"}); r.Code == http.StatusOK {
		t.Fatal("expected traversal to be rejected")
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal("the workspace root itself must still exist")
	}
}
