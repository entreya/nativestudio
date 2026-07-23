package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Guard resolves project-relative paths and rejects traversal and symlink escapes.
type Guard struct{ Root string }

// WorkspaceGuard is the central path-security contract: every tool that
// resolves a user- or model-supplied path against a workspace root should
// depend on this instead of re-implementing containment checks, so path
// safety logic (traversal, symlink escape, sibling-prefix collisions) lives
// in exactly one place. Guard implements it below.
type WorkspaceGuard interface {
	ResolveDirectory(workspaceRoot, requestedPath string) (string, error)
	ResolveExistingPath(workspaceRoot, requestedPath string) (string, error)
	IsInsideWorkspace(workspaceRoot, resolvedPath string) bool
}

var _ WorkspaceGuard = Guard{}

func (g Guard) Resolve(requested string) (string, error) {
	if strings.ContainsRune(requested, 0) {
		return "", fmt.Errorf("invalid path: contains a null byte")
	}
	originalRoot, err := filepath.Abs(g.Root)
	if err != nil {
		return "", fmt.Errorf("invalid workspace root: %w", err)
	}
	canonicalRoot := originalRoot
	if resolved, evalErr := filepath.EvalSymlinks(originalRoot); evalErr == nil {
		canonicalRoot = resolved
	}
	cleaned := filepath.Clean(requested)
	target := filepath.Join(originalRoot, strings.TrimLeft(cleaned, `/\`))
	if filepath.IsAbs(cleaned) {
		if within(originalRoot, cleaned) || within(canonicalRoot, cleaned) {
			target = cleaned
		}
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	// Resolve the existing parent even when the final file does not yet exist.
	existing := target
	for {
		if _, statErr := os.Lstat(existing); statErr == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		existing = parent
	}
	canonicalTarget := target
	if resolved, evalErr := filepath.EvalSymlinks(existing); evalErr == nil {
		canonicalTarget = filepath.Join(resolved, strings.TrimPrefix(strings.TrimPrefix(target, existing), string(filepath.Separator)))
	}
	if !within(canonicalRoot, canonicalTarget) {
		return "", fmt.Errorf("outside_workspace")
	}
	return target, nil
}

func (g Guard) Relative(path string) (string, error) {
	full, err := g.Resolve(path)
	if err != nil {
		return "", err
	}
	root, _ := filepath.Abs(g.Root)
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func within(root, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ResolveExistingPath resolves requestedPath against workspaceRoot (ignoring
// any Root already set on the receiver, so a zero-value Guard{} works too)
// and additionally requires the result to already exist on disk.
func (g Guard) ResolveExistingPath(workspaceRoot, requestedPath string) (string, error) {
	resolved, err := (Guard{Root: workspaceRoot}).Resolve(requestedPath)
	if err != nil {
		return "", err
	}
	if _, statErr := os.Lstat(resolved); statErr != nil {
		if os.IsNotExist(statErr) {
			return "", fmt.Errorf("path_not_found")
		}
		return "", statErr
	}
	return resolved, nil
}

// ResolveDirectory resolves requestedPath against workspaceRoot and requires
// the result to be an existing directory (symlinked directories are followed
// and re-checked for containment by Resolve before this is ever reached).
func (g Guard) ResolveDirectory(workspaceRoot, requestedPath string) (string, error) {
	resolved, err := g.ResolveExistingPath(workspaceRoot, requestedPath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not_a_directory")
	}
	return resolved, nil
}

// IsInsideWorkspace reports whether resolvedPath (an already-resolved,
// absolute path) is contained within workspaceRoot.
func (g Guard) IsInsideWorkspace(workspaceRoot, resolvedPath string) bool {
	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return false
	}
	return within(root, resolvedPath)
}

// ResolveWalkEntry classifies a directory entry encountered while walking a
// workspace (e.g. os.ReadDir/filepath.WalkDir output): it follows a symlink
// to find its real type, but refuses to treat a symlink whose target escapes
// the workspace as usable — so directory walkers built on this never need
// their own symlink-escape logic. ok is false for a broken symlink or one
// that escapes the workspace; callers should skip such entries entirely.
func ResolveWalkEntry(workspaceRoot string, entry os.DirEntry, fullPath string) (isDir bool, ok bool) {
	if entry.Type()&os.ModeSymlink == 0 {
		return entry.IsDir(), true
	}
	target, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		return false, false
	}
	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return false, false
	}
	canonicalRoot := root
	if resolved, evalErr := filepath.EvalSymlinks(root); evalErr == nil {
		canonicalRoot = resolved
	}
	if !within(canonicalRoot, target) {
		return false, false
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		return false, false
	}
	return info.IsDir(), true
}
