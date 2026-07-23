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

func TestGuardRejectsNullByte(t *testing.T) {
	root := t.TempDir()
	if _, err := (Guard{Root: root}).Resolve("controllers/Site\x00Controller.php"); err == nil {
		t.Fatal("expected a path containing a null byte to be rejected")
	}
}

// Absolute paths outside the workspace and sibling-prefix paths (a directory
// whose name merely starts with the workspace's name, e.g. "workspace-other"
// next to "workspace") must never resolve to a location outside the
// workspace — Resolve reinterprets them as workspace-relative rather than
// honoring the absolute/sibling path, so the result always stays contained.
func TestGuardNeverEscapesForAbsoluteOrSiblingInput(t *testing.T) {
	root := t.TempDir()
	guard := Guard{Root: root}

	outsideDir := t.TempDir()
	siblingDir := root + "-other"
	if err := os.Mkdir(siblingDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []string{
		"../../.ssh",
		"/etc",
		outsideDir,
		siblingDir,
		"../" + filepath.Base(root) + "-other",
	}
	for _, requested := range cases {
		resolved, err := guard.Resolve(requested)
		if err != nil {
			continue // rejecting outright also satisfies "must not escape"
		}
		if !guard.IsInsideWorkspace(root, resolved) {
			t.Errorf("Resolve(%q) escaped the workspace: %s", requested, resolved)
		}
	}
}

func TestGuardResolveDirectoryRequiresExistingDirectory(t *testing.T) {
	root := t.TempDir()
	guard := Guard{}

	if _, err := guard.ResolveDirectory(root, "does-not-exist"); err == nil {
		t.Fatal("expected a missing directory to be rejected")
	}

	filePath := filepath.Join(root, "file.txt")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := guard.ResolveDirectory(root, "file.txt"); err == nil {
		t.Fatal("expected a regular file to be rejected as a directory")
	}

	if err := os.MkdirAll(filepath.Join(root, "src", "services"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := guard.ResolveDirectory(root, "src/services")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != filepath.Join(root, "src", "services") {
		t.Fatalf("got %s", resolved)
	}

	// The workspace root itself must resolve correctly for both "" and ".".
	for _, path := range []string{"", "."} {
		if _, err := guard.ResolveDirectory(root, path); err != nil {
			t.Fatalf("resolving the workspace root (%q) failed: %v", path, err)
		}
	}
}

func TestGuardRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable in this environment: %v", err)
	}

	guard := Guard{Root: root}
	if _, err := guard.Resolve("escape/secret.txt"); err == nil {
		t.Fatal("expected a symlink pointing outside the workspace to be rejected")
	}
}

func TestGuardIsInsideWorkspace(t *testing.T) {
	root := t.TempDir()
	guard := Guard{}
	if !guard.IsInsideWorkspace(root, filepath.Join(root, "src", "main.go")) {
		t.Fatal("expected a path under the workspace root to be reported inside")
	}
	if guard.IsInsideWorkspace(root, root+"-other/main.go") {
		t.Fatal("expected a sibling-prefix path to be reported outside")
	}
}
