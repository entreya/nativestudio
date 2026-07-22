package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestScannerRespectsIgnoreSecretsAndSize(t *testing.T) {
	root := t.TempDir()
	mustWrite := func(path, content string) {
		t.Helper()
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(".gitignore", "ignored.go\ncache/\n")
	mustWrite("main.go", "package main\n")
	mustWrite("ignored.go", "package ignored\n")
	mustWrite("cache/data.go", "package cache\n")
	mustWrite(".env", "TOKEN=secret\n")
	mustWrite("large.go", "01234567890123456789")
	config := DefaultScanConfig()
	config.MaximumFileSizeBytes = 16
	files, skipped, err := (Scanner{Config: config}).Scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "main.go" {
		t.Fatalf("unexpected indexed files: %#v", files)
	}
	reasons := map[string]string{}
	for _, item := range skipped {
		reasons[item.Path] = item.Reason
	}
	if reasons[".env"] != "sensitive" {
		t.Fatalf("expected sensitive .env, got %#v", reasons)
	}
	if reasons["large.go"] != "too_large" {
		t.Fatalf("expected large file skip, got %#v", reasons)
	}
}

func TestScannerRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte("package outside"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked.go")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	files, skipped, err := (Scanner{Config: DefaultScanConfig()}).Scan(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("symlink escape indexed: %#v", files)
	}
	if len(skipped) == 0 || skipped[0].Reason != "outside_workspace" {
		t.Fatalf("expected outside_workspace skip: %#v", skipped)
	}
}
