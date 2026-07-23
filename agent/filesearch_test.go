package agent

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newFileSearchWorkspace builds a fixture workspace covering: nested
// directories, similar/ambiguous filenames, CamelCase/snake_case/kebab-case
// names, an ignored (dependency) directory, a custom .gitignore entry, and
// sensitive filenames.
func newFileSearchWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"src/services/AuthService.php":                           "<?php class AuthService {}",
		"src/services/UserAuthService.php":                       "<?php class UserAuthService {}",
		"src/controllers/AuthController.go":                      "package controllers",
		"modules/evaluation/services/ResultHandler.php":          "<?php class ResultHandler {}",
		"modules/evaluation/services/AuthEligibilityService.php": "<?php class AuthEligibilityService {}",
		"modules/evaluation/StudentEligibilityController.ts":     "export class StudentEligibilityController {}",
		"modules/evaluation/student_eligibility_helper.py":       "def helper(): pass",
		"modules/evaluation/student-eligibility-widget.js":       "export const widget = {};",
		"node_modules/leftpad/index.js":                          "module.exports = {};",
		"vendor/composer/AuthServiceProvider.php":                "<?php class AuthServiceProvider {}",
		".git/config":                        "[core]",
		".env":                               "SECRET=1",
		"id_rsa":                             "-----BEGIN OPENSSH PRIVATE KEY-----",
		"ignored_dir/AuthServiceIgnored.php": "<?php // should be gitignored",
	}
	for relPath, content := range files {
		mustWriteFile(t, filepath.Join(root, relPath), content)
	}
	mustWriteFile(t, filepath.Join(root, ".gitignore"), "ignored_dir/\n")
	return root
}

func callFindFiles(t *testing.T, ws string, input ToolInput) findFilesOutput {
	t.Helper()
	result, err := executeFindFiles(context.Background(), input, ToolMeta{WorkspaceRoot: ws})
	if err != nil {
		t.Fatalf("executeFindFiles returned an error: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected an OK result, got error: %s", result.Error)
	}
	output, ok := result.Content.(findFilesOutput)
	if !ok {
		t.Fatalf("unexpected content type %T", result.Content)
	}
	return output
}

func callFindFilesExpectError(t *testing.T, ws string, input ToolInput) string {
	t.Helper()
	result, err := executeFindFiles(context.Background(), input, ToolMeta{WorkspaceRoot: ws})
	if err != nil {
		t.Fatalf("executeFindFiles returned an error: %v", err)
	}
	if result.OK {
		t.Fatalf("expected an error result, got matches: %#v", result.Content)
	}
	return result.Error
}

func callListDirectory(t *testing.T, ws string, input ToolInput) listDirectoryOutput {
	t.Helper()
	result, err := executeListDirectory(context.Background(), input, ToolMeta{WorkspaceRoot: ws})
	if err != nil {
		t.Fatalf("executeListDirectory returned an error: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected an OK result, got error: %s", result.Error)
	}
	output, ok := result.Content.(listDirectoryOutput)
	if !ok {
		t.Fatalf("unexpected content type %T", result.Content)
	}
	return output
}

func callListDirectoryExpectError(t *testing.T, ws string, input ToolInput) string {
	t.Helper()
	result, err := executeListDirectory(context.Background(), input, ToolMeta{WorkspaceRoot: ws})
	if err != nil {
		t.Fatalf("executeListDirectory returned an error: %v", err)
	}
	if result.OK {
		t.Fatalf("expected an error result, got entries: %#v", result.Content)
	}
	return result.Error
}

// ---- find_files ranking ----

func TestFindFilesExactBasenameRanksFirst(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	output := callFindFiles(t, ws, ToolInput{"query": "AuthService.php"})
	if len(output.Matches) == 0 || output.Matches[0].Path != "src/services/AuthService.php" {
		t.Fatalf("expected AuthService.php to rank first, got %#v", output.Matches)
	}
	if output.Matches[0].Reasons[0] != "Exact basename match" {
		t.Fatalf("expected exact basename match reason, got %v", output.Matches[0].Reasons)
	}
}

func TestFindFilesExactFilenameWithoutExtensionRanksCorrectly(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	output := callFindFiles(t, ws, ToolInput{"query": "AuthService"})
	if len(output.Matches) == 0 || output.Matches[0].Path != "src/services/AuthService.php" {
		t.Fatalf("expected AuthService.php to rank first, got %#v", output.Matches)
	}
	if output.Matches[0].Reasons[0] != "Exact filename-without-extension match" {
		t.Fatalf("expected exact-without-extension reason, got %v", output.Matches[0].Reasons)
	}
}

func TestFindFilesCamelCaseNormalization(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	output := callFindFiles(t, ws, ToolInput{"query": "user auth service"})
	if !containsPath(output.Matches, "src/services/UserAuthService.php") {
		t.Fatalf("expected a spaced query to match the CamelCase file via normalization, got %#v", output.Matches)
	}
}

func TestFindFilesSnakeCaseNormalization(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	output := callFindFiles(t, ws, ToolInput{"query": "student eligibility"})
	if !containsPath(output.Matches, "modules/evaluation/student_eligibility_helper.py") {
		t.Fatalf("expected query to match the snake_case file, got %#v", output.Matches)
	}
}

func TestFindFilesKebabCaseNormalization(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	output := callFindFiles(t, ws, ToolInput{"query": "student_eligibility"})
	if !containsPath(output.Matches, "modules/evaluation/student-eligibility-widget.js") {
		t.Fatalf("expected query to match the kebab-case file, got %#v", output.Matches)
	}
}

func TestFindFilesCaseInsensitiveMatching(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	output := callFindFiles(t, ws, ToolInput{"query": "authservice"})
	if len(output.Matches) == 0 || output.Matches[0].Path != "src/services/AuthService.php" {
		t.Fatalf("expected case-insensitive match to rank AuthService.php first, got %#v", output.Matches)
	}
}

func containsPath(matches []fileMatch, path string) bool {
	for _, m := range matches {
		if m.Path == path {
			return true
		}
	}
	return false
}

// ---- find_files filters ----

func TestFindFilesExtensionFilteringWorks(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	output := callFindFiles(t, ws, ToolInput{"query": "auth", "extensions": []any{"go"}})
	if len(output.Matches) == 0 {
		t.Fatal("expected at least one .go match")
	}
	for _, m := range output.Matches {
		if m.Extension != "go" {
			t.Fatalf("expected only .go matches, got %s", m.Path)
		}
	}
	// A leading dot must be accepted too.
	dotted := callFindFiles(t, ws, ToolInput{"query": "auth", "extensions": []any{".go"}})
	if !reflect.DeepEqual(output.Matches, dotted.Matches) {
		t.Fatalf("extension with/without leading dot should behave identically")
	}
}

func TestFindFilesDirectoryFilteringWorks(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	output := callFindFiles(t, ws, ToolInput{"query": "services", "file_types": []any{"directory"}})
	if len(output.Matches) == 0 {
		t.Fatal("expected at least one directory match")
	}
	for _, m := range output.Matches {
		if m.Type != "directory" {
			t.Fatalf("expected only directories, got %s (%s)", m.Path, m.Type)
		}
	}
}

func TestFindFilesPathFilteringWorks(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	scoped := callFindFiles(t, ws, ToolInput{"query": "auth", "path": "modules/evaluation"})
	if !containsPath(scoped.Matches, "modules/evaluation/services/AuthEligibilityService.php") {
		t.Fatalf("expected the scoped fixture to be found, got %#v", scoped.Matches)
	}
	for _, m := range scoped.Matches {
		if !strings.HasPrefix(m.Path, "modules/evaluation/") {
			t.Fatalf("expected only matches under modules/evaluation, got %s", m.Path)
		}
	}

	other := callFindFiles(t, ws, ToolInput{"query": "auth", "path": "src"})
	for _, m := range other.Matches {
		if strings.HasPrefix(m.Path, "modules/") {
			t.Fatalf("path filter leaked a result outside its scope: %s", m.Path)
		}
	}
}

func TestFindFilesResultsAreDeterministicallySorted(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	first := callFindFiles(t, ws, ToolInput{"query": "service"})
	second := callFindFiles(t, ws, ToolInput{"query": "service"})
	if len(first.Matches) == 0 {
		t.Fatal("expected matches")
	}
	if !reflect.DeepEqual(first.Matches, second.Matches) {
		t.Fatalf("expected deterministic ordering across identical calls:\n%#v\nvs\n%#v", first.Matches, second.Matches)
	}
}

func TestFindFilesLimitEnforcedAndTruncationReported(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	output := callFindFiles(t, ws, ToolInput{"query": "e", "limit": float64(1)})
	if len(output.Matches) != 1 {
		t.Fatalf("expected exactly 1 match due to the limit, got %d", len(output.Matches))
	}
	if !output.Truncated {
		t.Fatal("expected truncated=true when more matches exist than the limit")
	}
	if output.TotalMatches <= 1 {
		t.Fatalf("expected total_matches to reflect the full match count, got %d", output.TotalMatches)
	}
}

func TestFindFilesWorkspaceRootSearchWorks(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	output := callFindFiles(t, ws, ToolInput{"query": "AuthService.php"})
	if len(output.Matches) == 0 {
		t.Fatal("expected a match when searching the whole workspace (no path given)")
	}
}

// ---- find_files exclusions ----

func TestFindFilesIgnoredDirectoriesExcluded(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	output := callFindFiles(t, ws, ToolInput{"query": "index"})
	for _, m := range output.Matches {
		if strings.Contains(m.Path, "node_modules") {
			t.Fatalf("expected node_modules to be excluded, got %s", m.Path)
		}
	}
}

func TestFindFilesGitignoreEntriesExcluded(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	// The query's tokens ("auth", "service") legitimately partial-match other
	// real fixtures too, so the right assertion is "the gitignored file itself
	// never appears", not "there are no matches at all".
	output := callFindFiles(t, ws, ToolInput{"query": "AuthServiceIgnored"})
	if containsPath(output.Matches, "ignored_dir/AuthServiceIgnored.php") {
		t.Fatalf("expected the .gitignore'd file to be excluded, got %#v", output.Matches)
	}
}

func TestFindFilesSensitiveFilesExcluded(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	for _, query := range []string{".env", "env", "id_rsa", "rsa"} {
		output := callFindFiles(t, ws, ToolInput{"query": query})
		for _, m := range output.Matches {
			if m.Name == ".env" || m.Name == "id_rsa" {
				t.Fatalf("expected sensitive file %s to be excluded from query %q", m.Name, query)
			}
		}
	}
}

// ---- find_files path safety ----

func TestFindFilesRejectsPathTraversal(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	if msg := callFindFilesExpectError(t, ws, ToolInput{"query": "x", "path": "../../etc"}); msg == "" {
		t.Fatal("expected an error message")
	}
}

func TestFindFilesNeverEscapesWorkspaceForUnsafePaths(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	outside := t.TempDir()
	mustWriteFile(t, filepath.Join(outside, "OutsideSecret.php"), "<?php")
	sibling := ws + "-other"
	if err := os.Mkdir(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(sibling, "SiblingSecret.php"), "<?php")

	for _, p := range []string{"../../.ssh", "/etc", outside, sibling} {
		result, err := executeFindFiles(context.Background(), ToolInput{"query": "Secret", "path": p}, ToolMeta{WorkspaceRoot: ws})
		if err != nil {
			t.Fatal(err)
		}
		if result.OK {
			output := result.Content.(findFilesOutput)
			for _, m := range output.Matches {
				t.Errorf("path %q leaked a match outside the workspace: %s", p, m.Path)
			}
		}
	}
}

func TestFindFilesSymlinkEscapeNotFollowed(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	outside := t.TempDir()
	mustWriteFile(t, filepath.Join(outside, "EscapedAuthService.php"), "<?php")
	if err := os.Symlink(outside, filepath.Join(ws, "escaped-link")); err != nil {
		t.Skipf("symlinks unavailable in this environment: %v", err)
	}
	output := callFindFiles(t, ws, ToolInput{"query": "EscapedAuthService"})
	if containsPath(output.Matches, "escaped-link/EscapedAuthService.php") {
		t.Fatalf("expected a symlink pointing outside the workspace to never be searched, got %#v", output.Matches)
	}
}

func TestFindFilesContextCancellationStopsTraversal(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := executeFindFiles(ctx, ToolInput{"query": "e"}, ToolMeta{WorkspaceRoot: ws})
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("expected a graceful partial result even when canceled, got error: %s", result.Error)
	}
	output := result.Content.(findFilesOutput)
	if !output.Truncated {
		t.Fatal("expected truncated=true when the search context was already canceled")
	}
}

// ---- find_files input validation ----

func TestFindFilesInvalidExtensionsRejected(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	for _, bad := range []string{"../etc", "php/x", "*", ""} {
		if msg := callFindFilesExpectError(t, ws, ToolInput{"query": "auth", "extensions": []any{bad}}); msg == "" {
			t.Fatalf("expected extension %q to be rejected", bad)
		}
	}
}

func TestFindFilesInvalidFileTypesRejected(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	if msg := callFindFilesExpectError(t, ws, ToolInput{"query": "auth", "file_types": []any{"banana"}}); msg == "" {
		t.Fatal("expected an invalid file_type to be rejected")
	}
}

func TestFindFilesRequiresQuery(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	if msg := callFindFilesExpectError(t, ws, ToolInput{}); msg == "" {
		t.Fatal("expected a missing query to be rejected")
	}
}

// ---- list_directory ----

func TestListDirectoryDefaultsToWorkspaceRoot(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	output := callListDirectory(t, ws, ToolInput{})
	if output.Path != "." {
		t.Fatalf("expected default path \".\", got %q", output.Path)
	}
	if len(output.Entries) == 0 {
		t.Fatal("expected entries at the workspace root")
	}
}

func TestListDirectoryDepthEnforced(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	shallow := callListDirectory(t, ws, ToolInput{"path": "modules", "depth": float64(1)})
	for _, e := range shallow.Entries {
		if strings.Contains(e.Path, "modules/evaluation/services") {
			t.Fatalf("depth=1 should not descend into modules/evaluation/services, got %s", e.Path)
		}
	}
	deep := callListDirectory(t, ws, ToolInput{"path": "modules", "depth": float64(2)})
	found := false
	for _, e := range deep.Entries {
		if e.Path == "modules/evaluation/services" {
			found = true
		}
	}
	if !found {
		t.Fatalf("depth=2 should include the modules/evaluation/services directory entry, got %#v", deep.Entries)
	}
}

func TestListDirectoryDirectoriesBeforeFilesAlphabetical(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	output := callListDirectory(t, ws, ToolInput{"path": "modules/evaluation", "depth": float64(1)})
	sawFile := false
	var fileNames []string
	for _, e := range output.Entries {
		if e.Type == "file" {
			sawFile = true
			fileNames = append(fileNames, e.Name)
		} else if sawFile {
			t.Fatalf("expected all directories before files, got directory %q after a file", e.Path)
		}
	}
	if !sort.SliceIsSorted(fileNames, func(i, j int) bool { return strings.ToLower(fileNames[i]) < strings.ToLower(fileNames[j]) }) {
		t.Fatalf("expected files sorted alphabetically, got %v", fileNames)
	}
}

func TestListDirectoryLimitAndTruncation(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	output := callListDirectory(t, ws, ToolInput{"path": "modules/evaluation", "limit": float64(1)})
	if len(output.Entries) != 1 {
		t.Fatalf("expected exactly 1 entry due to the limit, got %d", len(output.Entries))
	}
	if !output.Truncated {
		t.Fatal("expected truncated=true")
	}
}

func TestListDirectoryIgnoredAndSensitiveExcluded(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	output := callListDirectory(t, ws, ToolInput{"path": "."})
	blocked := map[string]bool{"node_modules": true, "vendor": true, ".git": true, ".env": true, "id_rsa": true, "ignored_dir": true}
	for _, e := range output.Entries {
		if blocked[e.Name] {
			t.Fatalf("expected %q to be excluded from the listing", e.Name)
		}
	}
}

func TestListDirectoryRejectsUnsafePaths(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	if msg := callListDirectoryExpectError(t, ws, ToolInput{"path": "../../etc"}); msg == "" {
		t.Fatal("expected traversal to be rejected")
	}
	if msg := callListDirectoryExpectError(t, ws, ToolInput{"path": "src/services/AuthService.php"}); msg == "" {
		t.Fatal("expected a regular file to be rejected as a directory path")
	}
}

func TestListDirectoryFollowsInWorkspaceSymlink(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	if err := os.Symlink(filepath.Join(ws, "src", "services"), filepath.Join(ws, "src", "services-link")); err != nil {
		t.Skipf("symlinks unavailable in this environment: %v", err)
	}
	output := callListDirectory(t, ws, ToolInput{"path": "src", "depth": float64(2)})
	found := false
	for _, e := range output.Entries {
		if e.Path == "src/services-link/AuthService.php" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an in-workspace symlinked directory to be followed, got %#v", output.Entries)
	}
}

func TestListDirectorySymlinkEscapeNotFollowed(t *testing.T) {
	ws := newFileSearchWorkspace(t)
	outside := t.TempDir()
	mustWriteFile(t, filepath.Join(outside, "Secret.txt"), "x")
	if err := os.Symlink(outside, filepath.Join(ws, "escaped-link")); err != nil {
		t.Skipf("symlinks unavailable in this environment: %v", err)
	}
	output := callListDirectory(t, ws, ToolInput{"path": ".", "depth": float64(2)})
	for _, e := range output.Entries {
		if strings.Contains(e.Path, "Secret.txt") {
			t.Fatalf("expected the symlink escape to not be followed, got %s", e.Path)
		}
	}
}

// ---- registry ----

func TestRegistryRejectsUnknownTool(t *testing.T) {
	r := NewRegistry(t.TempDir())
	if _, found := r.Get("does_not_exist"); found {
		t.Fatal("expected an unknown tool lookup to report not found")
	}
}

func TestRegistryDetectsDuplicateToolNames(t *testing.T) {
	r := NewRegistry(t.TempDir())
	defer func() {
		if recover() == nil {
			t.Fatal("expected registering a duplicate tool name to panic")
		}
	}()
	r.Register(&Tool{Name: "find_files", Execute: executeFindFiles})
}
