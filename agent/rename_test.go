package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func renameTestEnvironment(t *testing.T) (*Agent, string, ToolMeta) {
	t.Helper()
	agentInstance, root, sessionID := newAgentTestEnvironment(t)
	meta := ToolMeta{SessionID: sessionID, RunID: "run-1", WorkspaceRoot: root, DB: agentInstance.DB}
	return agentInstance, root, meta
}

func writeRenameFile(t *testing.T, root, relative, content string) {
	t.Helper()
	full := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRenameSymbolAlsoRenamesTheFile is the regression test for the live bug:
// the class was renamed but its file was not, which breaks PSR-4 autoloading
// outright — the new class can't be found at the old path, and the old route
// dies with it.
func TestRenameSymbolAlsoRenamesTheFile(t *testing.T) {
	agentInstance, root, meta := renameTestEnvironment(t)
	writeRenameFile(t, root, "controllers/SiteController.php",
		"<?php\nnamespace app\\controllers;\nclass SiteController extends Controller\n{\n}\n")

	result, err := executeRenameSymbol(context.Background(), ToolInput{
		"old_name": "SiteController", "new_name": "KlopController",
	}, meta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.OK {
		t.Fatalf("rename failed: %+v", result)
	}

	content := result.Content.(map[string]any)
	renamed, _ := content["file_renamed"].(string)
	if !strings.Contains(renamed, "KlopController.php") {
		t.Fatalf("expected the file to be renamed too, got %q", renamed)
	}

	patches, err := agentInstance.DB.ListPatches(context.Background(), "session", "pending")
	if err != nil {
		t.Fatal(err)
	}
	var created, deleted bool
	for _, patch := range patches {
		if patch.Operation == "create" && strings.HasSuffix(patch.FilePath, "KlopController.php") {
			created = true
			if !strings.Contains(patch.NewContent, "class KlopController") {
				t.Errorf("new file should declare the new class:\n%s", patch.NewContent)
			}
		}
		if patch.Operation == "delete" && strings.HasSuffix(patch.FilePath, "SiteController.php") {
			deleted = true
		}
	}
	if !created || !deleted {
		t.Fatalf("expected the rename to be staged as create+delete (created=%v deleted=%v)", created, deleted)
	}
}

// TestRenameSymbolUpdatesReferencesInOtherFiles covers the second half the
// model talked itself out of doing: references elsewhere must move too, or
// they point at a symbol that no longer exists.
func TestRenameSymbolUpdatesReferencesInOtherFiles(t *testing.T) {
	agentInstance, root, meta := renameTestEnvironment(t)
	writeRenameFile(t, root, "controllers/SiteController.php", "<?php\nclass SiteController {}\n")
	writeRenameFile(t, root, "config/routes.php", "<?php\nreturn ['site' => SiteController::class];\n")
	writeRenameFile(t, root, "tests/SiteControllerTest.php", "<?php\n$c = new SiteController();\n")

	result, err := executeRenameSymbol(context.Background(), ToolInput{
		"old_name": "SiteController", "new_name": "KlopController",
	}, meta)
	if err != nil || !result.OK {
		t.Fatalf("rename failed: %+v err=%v", result, err)
	}

	patches, err := agentInstance.DB.ListPatches(context.Background(), "session", "pending")
	if err != nil {
		t.Fatal(err)
	}
	touched := map[string]bool{}
	for _, patch := range patches {
		touched[patch.FilePath] = true
		if patch.Operation == "modify" && strings.Contains(patch.NewContent, "SiteController") &&
			!strings.Contains(patch.NewContent, "KlopController") {
			t.Errorf("%s still references the old symbol:\n%s", patch.FilePath, patch.NewContent)
		}
	}
	for _, expected := range []string{"config/routes.php", "tests/SiteControllerTest.php"} {
		if !touched[expected] {
			t.Errorf("expected %s to be updated, staged: %v", expected, touched)
		}
	}
}

func TestRenameSymbolRejectsNonSymbolInput(t *testing.T) {
	_, root, meta := renameTestEnvironment(t)
	writeRenameFile(t, root, "a.php", "<?php class A {}\n")

	for _, bad := range []ToolInput{
		{"old_name": "class SiteController", "new_name": "KlopController"},
		{"old_name": "SiteController", "new_name": "Klop Controller"},
		{"old_name": "A", "new_name": "A"},
		{"old_name": "", "new_name": "B"},
	} {
		result, err := executeRenameSymbol(context.Background(), bad, meta)
		if err != nil {
			t.Fatalf("unexpected error for %+v: %v", bad, err)
		}
		if result.OK {
			t.Errorf("expected %+v to be rejected", bad)
		}
	}
}

func TestRenameSymbolOnlyMatchesWholeWords(t *testing.T) {
	agentInstance, root, meta := renameTestEnvironment(t)
	// SiteControllerHelper must NOT be renamed when renaming SiteController.
	writeRenameFile(t, root, "app.php", "<?php\nclass SiteController {}\nclass SiteControllerHelper {}\n")

	result, err := executeRenameSymbol(context.Background(), ToolInput{
		"old_name": "SiteController", "new_name": "KlopController",
	}, meta)
	if err != nil || !result.OK {
		t.Fatalf("rename failed: %+v err=%v", result, err)
	}

	patches, err := agentInstance.DB.ListPatches(context.Background(), "session", "pending")
	if err != nil {
		t.Fatal(err)
	}
	for _, patch := range patches {
		if strings.Contains(patch.NewContent, "KlopControllerHelper") {
			t.Fatalf("renamed a longer symbol that merely starts with the old name:\n%s", patch.NewContent)
		}
		if !strings.Contains(patch.NewContent, "SiteControllerHelper") {
			t.Fatalf("SiteControllerHelper should have been left alone:\n%s", patch.NewContent)
		}
	}
}

func TestRenameSymbolReportsWhenSymbolIsAbsent(t *testing.T) {
	_, root, meta := renameTestEnvironment(t)
	writeRenameFile(t, root, "a.php", "<?php class Other {}\n")

	result, err := executeRenameSymbol(context.Background(), ToolInput{
		"old_name": "SiteController", "new_name": "KlopController",
	}, meta)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.Error != "symbol_not_found" {
		t.Fatalf("expected a clear symbol_not_found result, got %+v", result)
	}
}

// TestRenameSymbolFinishesAnAlreadyHalfDoneRename is the regression test for
// a live bug: a file (controllers/SiteController.php) already declared
// "class KlopController" — renamed by hand at some point without renaming
// its file, the exact PSR-4 mismatch this tool exists to prevent, just
// reached from the other direction. Asking to rename SiteController to
// KlopController again correctly found no SiteController symbol and
// returned a bare symbol_not_found, which gave the small model in
// NativeStudio nothing to work with — it spent several rounds of
// increasingly confused reasoning trying to figure out what must have
// happened instead of being told directly. rename_symbol should recognize
// this specific state and finish the job (rename the file) in one shot.
func TestRenameSymbolFinishesAnAlreadyHalfDoneRename(t *testing.T) {
	agentInstance, root, meta := renameTestEnvironment(t)
	writeRenameFile(t, root, "controllers/SiteController.php",
		"<?php\nnamespace app\\controllers;\nclass KlopController extends Controller\n{\n}\n")

	result, err := executeRenameSymbol(context.Background(), ToolInput{
		"old_name": "SiteController", "new_name": "KlopController",
	}, meta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.OK {
		t.Fatalf("expected the tool to finish the half-done rename instead of erroring, got %+v", result)
	}

	content := result.Content.(map[string]any)
	if renamed, _ := content["already_renamed"].(bool); !renamed {
		t.Errorf("expected already_renamed: true so the model can tell this apart from a fresh rename, got %+v", content)
	}
	renamedFile, _ := content["file_renamed"].(string)
	if !strings.Contains(renamedFile, "KlopController.php") {
		t.Fatalf("expected the file to be renamed to KlopController.php, got %q", renamedFile)
	}

	patches, err := agentInstance.DB.ListPatches(context.Background(), "session", "pending")
	if err != nil {
		t.Fatal(err)
	}
	var created, deleted bool
	for _, patch := range patches {
		if patch.Operation == "create" && strings.HasSuffix(patch.FilePath, "KlopController.php") {
			created = true
			if !strings.Contains(patch.NewContent, "class KlopController") {
				t.Errorf("new file should keep the already-renamed class declaration:\n%s", patch.NewContent)
			}
		}
		if patch.Operation == "delete" && strings.HasSuffix(patch.FilePath, "SiteController.php") {
			deleted = true
		}
	}
	if !created || !deleted {
		t.Fatalf("expected a create+delete pair for the file rename (created=%v deleted=%v)", created, deleted)
	}
}

// TestRenameSymbolDoesNotConfuseAnUnrelatedFileForAHalfDoneRename makes sure
// the half-done-rename detection only fires when the filename itself matches
// oldName — a file that happens to mention newName for some unrelated reason
// must not be mistaken for a completed rename.
func TestRenameSymbolDoesNotConfuseAnUnrelatedFileForAHalfDoneRename(t *testing.T) {
	_, root, meta := renameTestEnvironment(t)
	writeRenameFile(t, root, "notes.php", "<?php // see KlopController for reference\n")

	result, err := executeRenameSymbol(context.Background(), ToolInput{
		"old_name": "SiteController", "new_name": "KlopController",
	}, meta)
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || result.Error != "symbol_not_found" {
		t.Fatalf("expected a plain symbol_not_found (no file is named SiteController), got %+v", result)
	}
}
