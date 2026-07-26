package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/entreya/nativestudio/db"
)

// registerRenameSymbolTool adds rename_symbol — a single deterministic
// operation for "rename this class/function everywhere".
//
// It exists because renaming was the worst case for a small model working
// through replace_in_file. Two things went wrong repeatedly and neither was a
// knowledge gap:
//
//  1. replace_in_file needs an exact-match string, so the model spent most of
//     its reasoning re-deriving whether to send "class SiteController" or
//     "SiteController", whether there was a space after "class", and so on —
//     observed re-deriving the same answer nine times in one run.
//  2. It renamed the class but not the file. For PSR-4 autoloading (Yii2,
//     Laravel, most modern PHP) a class must live in a file of the same name,
//     so "controllers/SiteController.php" containing "class KlopController"
//     fails to autoload and takes the old route down with it.
//
// Both are structural problems, so they get a structural fix: the caller
// names the symbol, not a byte-exact string, and the file rename happens as
// part of the same operation rather than being something to remember.
func registerRenameSymbolTool(r *Registry) {
	r.Register(&Tool{
		Name: "rename_symbol",
		Description: "Rename a class, interface, trait, function or constant across the workspace. Handles everything the rename needs: the declaration, all references in other files, and the file itself when its name matches the symbol " +
			"(required for PSR-4 autoloading — renaming a class without renaming its file breaks it). Prefer this over replace_in_file for any rename: you name the symbol, not an exact string. All changes are staged for review.",
		Parameters: objectParameters(map[string]any{
			"old_name": map[string]any{"type": "string", "description": "The current symbol name, e.g. \"SiteController\"."},
			"new_name": map[string]any{"type": "string", "description": "The new symbol name, e.g. \"KlopController\"."},
			"path":     map[string]any{"type": "string", "description": "Optional. The file declaring the symbol, if known. Found automatically when omitted."},
		}, "old_name", "new_name"),
		Safety:  RequiresApproval,
		Execute: executeRenameSymbol,
	})
}

// symbolNamePattern guards against a "symbol" that is really a code fragment,
// which would turn a rename into an arbitrary find-and-replace.
var symbolNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func executeRenameSymbol(ctx context.Context, input ToolInput, meta ToolMeta) (ToolResult, error) {
	oldName := strings.TrimSpace(stringInput(input, "old_name"))
	newName := strings.TrimSpace(stringInput(input, "new_name"))

	if !symbolNamePattern.MatchString(oldName) {
		return ToolResult{OK: false, Error: "old_name must be a plain symbol name (letters, digits, underscore)"}, nil
	}
	if !symbolNamePattern.MatchString(newName) {
		return ToolResult{OK: false, Error: "new_name must be a plain symbol name (letters, digits, underscore)"}, nil
	}
	if oldName == newName {
		return ToolResult{OK: false, Error: "old_name and new_name are the same"}, nil
	}
	if meta.DB == nil || meta.SessionID == "" || meta.RunID == "" {
		return ToolResult{OK: false, Error: "patch_metadata_unavailable"}, nil
	}

	matches, err := findSymbolReferences(meta.WorkspaceRoot, oldName)
	if err != nil {
		return ToolResult{OK: false, Error: err.Error()}, nil
	}
	if len(matches) == 0 {
		return ToolResult{OK: false, Error: "symbol_not_found", Content: map[string]any{
			"message": fmt.Sprintf("No file in the workspace mentions %q.", oldName),
		}}, nil
	}

	staged := make([]map[string]any, 0, len(matches)+1)
	boundary := regexp.MustCompile(`\b` + regexp.QuoteMeta(oldName) + `\b`)

	// A file named after the symbol goes through the create+delete path below
	// instead of the ordinary modify path: staging a "modify" for it too would
	// rewrite its content in place at the OLD path right before deleting that
	// same path, which is redundant work and — worse — a real hazard if it's
	// approved without the delete, since it recreates the exact PSR-4
	// mismatch (new class name, old filename) this tool exists to prevent.
	renameMatchIndex := -1
	for i, match := range matches {
		base := filepath.Base(match.RelativePath)
		extension := filepath.Ext(base)
		if strings.TrimSuffix(base, extension) == oldName {
			renameMatchIndex = i
			break
		}
	}

	for i, match := range matches {
		if i == renameMatchIndex {
			continue
		}
		updated := boundary.ReplaceAllString(match.Content, newName)
		if updated == match.Content {
			continue
		}
		result, err := stagePatch(ctx, meta, match.RelativePath, "modify",
			computeUnifiedDiff(match.RelativePath, match.Content, updated), updated, match.Content)
		if err != nil || !result.OK {
			return result, err
		}
		if output, ok := result.Content.(map[string]any); ok {
			output["references_renamed"] = len(boundary.FindAllString(match.Content, -1))
			staged = append(staged, output)
		}
	}

	// The file rename. Staged as a create+delete pair because the patch model
	// has no rename operation — approving both is what actually moves it.
	renamedFile := ""
	if renameMatchIndex >= 0 {
		match := matches[renameMatchIndex]
		base := filepath.Base(match.RelativePath)
		extension := filepath.Ext(base)
		newRelative := filepath.ToSlash(filepath.Join(filepath.Dir(match.RelativePath), newName+extension))
		newContent := boundary.ReplaceAllString(match.Content, newName)

		createResult, err := stagePatch(ctx, meta, newRelative, "create",
			computeUnifiedDiff(newRelative, "", newContent), newContent, "")
		if err != nil || !createResult.OK {
			return createResult, err
		}
		deleteResult, err := stagePatch(ctx, meta, match.RelativePath, "delete",
			computeUnifiedDiff(match.RelativePath, match.Content, ""), "", match.Content)
		if err != nil || !deleteResult.OK {
			return deleteResult, err
		}
		if output, ok := createResult.Content.(map[string]any); ok {
			staged = append(staged, output)
		}
		if output, ok := deleteResult.Content.(map[string]any); ok {
			staged = append(staged, output)
		}
		renamedFile = match.RelativePath + " → " + newRelative
	}

	summary := fmt.Sprintf("Renamed %s to %s across %d file(s)", oldName, newName, len(matches))
	if renamedFile != "" {
		summary += "; file renamed " + renamedFile
	} else {
		summary += "; no file needed renaming (no file is named after the symbol)"
	}

	return ToolResult{OK: true, Content: map[string]any{
		"staged":       true,
		"summary":      summary,
		"files":        len(matches),
		"file_renamed": renamedFile,
		"patches":      staged,
	}}, nil
}

type symbolMatch struct {
	RelativePath string
	Content      string
}

// findSymbolReferences returns every text file in the workspace containing
// oldName as a whole word. Deliberately a whole-workspace scan: the reason
// renames broke before was that only the obvious file got updated, leaving
// references elsewhere pointing at a symbol that no longer exists.
func findSymbolReferences(workspaceRoot, oldName string) ([]symbolMatch, error) {
	boundary := regexp.MustCompile(`\b` + regexp.QuoteMeta(oldName) + `\b`)
	var matches []symbolMatch

	err := filepath.WalkDir(workspaceRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if path != workspaceRoot && (strings.HasPrefix(name, ".") || skippedRenameDirs[name]) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}
		info, statErr := entry.Info()
		if statErr != nil || info.Size() > renameMaxFileSize {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil || looksBinary(data) {
			return nil
		}
		if !boundary.Match(data) {
			return nil
		}
		relative, relErr := filepath.Rel(workspaceRoot, path)
		if relErr != nil {
			return nil
		}
		matches = append(matches, symbolMatch{RelativePath: filepath.ToSlash(relative), Content: string(data)})
		return nil
	})
	return matches, err
}

const renameMaxFileSize = 2 << 20

var skippedRenameDirs = map[string]bool{
	"node_modules": true, "vendor": true, "__pycache__": true,
	"dist": true, "build": true, ".venv": true, "venv": true,
}

func stringInput(input ToolInput, key string) string {
	value, _ := input[key].(string)
	return value
}

// ensure db stays referenced if stagePatch's signature changes shape.
var _ = db.Patch{}
