package agent

import (
	"path/filepath"
	"testing"
)

func TestSafeJoinAcceptsProjectRelativeLeadingSlash(t *testing.T) {
	root := t.TempDir()
	got, err := safeJoin(root, "/controllers/SiteController.php")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "controllers", "SiteController.php")
	if got != want {
		t.Fatalf("safeJoin returned %q, want %q", got, want)
	}
}

func TestSafeJoinPreservesAbsolutePathInsideWorkspace(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "controllers", "SiteController.php")
	got, err := safeJoin(root, want)
	if err != nil || got != want {
		t.Fatalf("safeJoin returned %q, %v; want %q", got, err, want)
	}
}

func TestSafeJoinKeepsTraversalInsideWorkspace(t *testing.T) {
	root := t.TempDir()
	got, err := safeJoin(root, "/../../etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "etc", "passwd")
	if got != want {
		t.Fatalf("safeJoin returned %q, want %q", got, want)
	}
}
