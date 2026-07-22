package handlers

import (
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
