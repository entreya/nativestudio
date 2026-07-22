package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entreya/nativestudio/db"
)

func TestUnifiedDiffRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		original string
		updated  string
	}{
		{name: "modify", original: "one\ntwo\nthree\n", updated: "one\nsecond\nthree\n"},
		{name: "create", original: "", updated: "first\nsecond\n"},
		{name: "delete", original: "first\nsecond\n", updated: ""},
		{name: "multiple hunks", original: "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\n", updated: "A\nb\nc\nd\ne\nf\ng\nh\ni\nJ\n"},
		{name: "no trailing newline", original: "one\ntwo", updated: "one\nsecond"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			diff := computeUnifiedDiff("example.txt", test.original, test.updated)
			if !strings.HasPrefix(diff, "--- a/example.txt\n+++ b/example.txt\n") {
				t.Fatalf("unexpected diff headers:\n%s", diff)
			}
			actual, err := applyUnifiedDiff(test.original, diff)
			if err != nil {
				t.Fatalf("apply generated diff: %v\n%s", err, diff)
			}
			if actual != test.updated {
				t.Fatalf("round trip mismatch\nwant: %q\n got: %q\ndiff:\n%s", test.updated, actual, diff)
			}
		})
	}
	if diff := computeUnifiedDiff("same.txt", "same\n", "same\n"); diff != "" {
		t.Fatalf("identical content produced a diff: %q", diff)
	}
}

func TestMutationToolsStageWithoutWriting(t *testing.T) {
	root := t.TempDir()
	database, err := db.Open(filepath.Join(t.TempDir(), "patch-tools.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if _, err := database.CreateProject("project", "Project", root, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateSession("session", "project", "model", "Test"); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, "sample.txt")
	original := "alpha\nbeta\ngamma\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	meta := ToolMeta{SessionID: "session", RunID: "run", WorkspaceRoot: root, DB: database}
	result, err := replaceInFile(context.Background(), ToolInput{
		"path": "sample.txt", "old_text": "beta", "new_text": "second",
	}, meta)
	if err != nil || !result.OK {
		t.Fatalf("stage replacement: result=%#v err=%v", result, err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != original {
		t.Fatalf("tool changed the file before approval: %q, %v", content, err)
	}
	patches, err := database.ListPatches(context.Background(), "session", "pending")
	if err != nil || len(patches) != 1 {
		t.Fatalf("pending patches: %#v, %v", patches, err)
	}
	if patches[0].NewContent != "alpha\nsecond\ngamma\n" || patches[0].OriginalContent != original {
		t.Fatalf("unexpected staged patch: %#v", patches[0])
	}
}

func TestMutationToolValidation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sample.txt")
	if err := os.WriteFile(path, []byte("repeat repeat"), 0644); err != nil {
		t.Fatal(err)
	}
	meta := ToolMeta{WorkspaceRoot: root}
	tests := []struct {
		name  string
		input ToolInput
		want  string
	}{
		{name: "missing text", input: ToolInput{"path": "sample.txt", "old_text": "absent", "new_text": "x"}, want: "text_not_found"},
		{name: "ambiguous text", input: ToolInput{"path": "sample.txt", "old_text": "repeat", "new_text": "x"}, want: "text_ambiguous"},
		{name: "empty old text", input: ToolInput{"path": "sample.txt", "old_text": "", "new_text": "x"}, want: "old_text is required"},
		{name: "outside workspace", input: ToolInput{"path": "../outside.txt", "old_text": "x", "new_text": "y"}, want: "outside_workspace"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := replaceInFile(context.Background(), test.input, meta)
			if err != nil || result.OK || result.Error != test.want {
				t.Fatalf("result=%#v err=%v, want error %q", result, err, test.want)
			}
		})
	}

	invalid, err := applyPatchTool(context.Background(), ToolInput{"path": "sample.txt", "diff": "not a diff"}, meta)
	if err != nil || invalid.OK || invalid.Error != "patch_failed" {
		t.Fatalf("invalid patch result=%#v err=%v", invalid, err)
	}
}
