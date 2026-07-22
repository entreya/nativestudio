package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGuardAcceptsProjectRelativeLeadingSlash(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "controllers", "SiteController.php")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("<?php"), 0644); err != nil {
		t.Fatal(err)
	}
	resolved, err := (Guard{Root: root}).Resolve("/controllers/SiteController.php")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != path {
		t.Fatalf("got %s want %s", resolved, path)
	}
}
func TestGuardBlocksTraversal(t *testing.T) {
	root := t.TempDir()
	if _, err := (Guard{Root: root}).Resolve("../../outside.txt"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}
