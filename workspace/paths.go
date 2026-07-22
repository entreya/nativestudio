package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Guard resolves project-relative paths and rejects traversal and symlink escapes.
type Guard struct{ Root string }

func (g Guard) Resolve(requested string) (string, error) {
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
