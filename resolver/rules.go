package resolver

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/entreya/nativestudio/editor"
)

// execTimeout bounds external command calls (ripgrep) so an unusual or very
// large repository can't hang a chat request indefinitely.
const execTimeout = 5 * time.Second

// skippedWalkDirs mirrors the indexer's default exclusions so the resolver's
// fallback filesystem walks — which run synchronously on the chat request
// path, not in the background like the indexer — don't burn time crawling
// dependency and build-output directories on every message.
var skippedWalkDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "dist": true, "build": true,
	"coverage": true, "target": true, "tmp": true, "cache": true, ".cache": true,
	".next": true, ".nuxt": true, "__pycache__": true,
}

// skipExcludedDir returns filepath.SkipDir for directories that should never
// be descended into by the resolver's ad-hoc walks.
func skipExcludedDir(info os.FileInfo) error {
	if info.IsDir() && skippedWalkDirs[info.Name()] {
		return filepath.SkipDir
	}
	return nil
}

// ApplyRules generates an initial set of candidates by applying deterministic
// rules based on extracted prompt references and the current editor state.
// Scores here are base values that ScoreCandidate will further adjust.
func ApplyRules(ctx context.Context, refs PromptRefs, state editor.EditorState, workspaceRoot string) []Candidate {
	var candidates []Candidate

	// ── Rule 1: "this X" / "here" ───────────────────────────────────────────
	if refs.HasThis || refs.ThisKind == "file" {
		if refs.ThisKind == "file" && state.ActiveFile != "" {
			// "this file" → the active file with very high confidence
			candidates = append(candidates, Candidate{
				Path:    state.ActiveFile,
				Score:   0.98,
				Reasons: []string{"this_file"},
			})
		} else if state.Selection != nil && state.Selection.Text != "" {
			// Selection takes highest priority for "this <kind>"
			candidates = append(candidates, Candidate{
				Path:    state.ActiveFile,
				Score:   0.95,
				Reasons: []string{"selected_text"},
			})
		} else if state.SelectedSymbol != nil && (refs.ThisKind == "" || strings.EqualFold(state.SelectedSymbol.Kind, refs.ThisKind)) {
			// Selected symbol matching the requested kind
			candidates = append(candidates, Candidate{
				Path:    state.SelectedSymbol.FilePath,
				Symbol:  state.SelectedSymbol,
				Score:   0.95,
				Reasons: []string{"selected_symbol"},
			})
		} else if state.ActiveFile != "" {
			// Fallback to active file
			candidates = append(candidates, Candidate{
				Path:    state.ActiveFile,
				Score:   0.80,
				Reasons: []string{"active_file_fallback"},
			})
		}
	}

	// ── Rule 2: Explicit file references ─────────────────────────────────────
	for _, fileRef := range refs.ExplicitFiles {
		matches := findFilesByNameSuffix(workspaceRoot, fileRef)
		for _, m := range matches {
			relPath, err := filepath.Rel(workspaceRoot, m)
			if err != nil {
				relPath = m
			}
			candidates = append(candidates, Candidate{
				Path:    relPath,
				Score:   0.90,
				Reasons: []string{"explicit_file_reference"},
			})
		}
	}

	// ── Rule 3: PascalCase name search via ripgrep ────────────────────────────
	for _, name := range refs.ExplicitNames {
		matches := findSymbolByName(ctx, name, workspaceRoot)
		for _, m := range matches {
			candidates = append(candidates, Candidate{
				Path:    m,
				Score:   0.70,
				Reasons: []string{fmt.Sprintf("pascal_case_match:%s", name)},
			})
		}
	}

	return candidates
}

// findFilesByNameSuffix walks the workspace and returns all files whose path
// ends with the given suffix (e.g. "auth.go", "services/auth.go").
func findFilesByNameSuffix(root, suffix string) []string {
	var results []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return skipExcludedDir(info)
		}
		// Match full suffix or basename
		if strings.HasSuffix(path, suffix) || info.Name() == filepath.Base(suffix) {
			results = append(results, path)
		}
		return nil
	})
	return results
}

// findSymbolByName uses ripgrep to locate declarations of a PascalCase name.
// Falls back to a manual walk if rg is not available.
func findSymbolByName(ctx context.Context, name, workspaceRoot string) []string {
	pattern := fmt.Sprintf(`(class|function|func|interface|trait|type)\s+%s\b`, name)

	rgPath, err := exec.LookPath("rg")
	if err == nil {
		rgCtx, cancel := context.WithTimeout(ctx, execTimeout)
		defer cancel()
		cmd := exec.CommandContext(rgCtx, rgPath, "-l", "--max-count", "1", "-e", pattern, workspaceRoot)
		out, err := cmd.Output()
		if err == nil {
			var results []string
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				rel, err := filepath.Rel(workspaceRoot, line)
				if err != nil {
					rel = line
				}
				results = append(results, rel)
			}
			return results
		}
	}

	// Manual fallback: walk and grep
	import_pattern := strings.ToLower(name)
	var results []string
	_ = filepath.Walk(workspaceRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return skipExcludedDir(info)
		}
		if info.Size() > 1<<20 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(strings.ToLower(string(data)), import_pattern) {
			rel, err := filepath.Rel(workspaceRoot, path)
			if err != nil {
				rel = path
			}
			results = append(results, rel)
		}
		return nil
	})
	return results
}
