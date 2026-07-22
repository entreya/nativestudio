package indexer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	workspacefs "github.com/entreya/nativestudio/workspace"
)

type ScanConfig struct {
	MaximumFileSizeBytes int64
	IncludedPaths        []string
	ExcludedDirectories  []string
	SensitivePatterns    []string
}

type ScannedFile struct {
	Path, Language, Hash, GitStatus string
	Size                            int64
	ModifiedAtNS                    int64
	Content                         []byte
}
type FileMetadata struct {
	Path, Language, GitStatus string
	Size, ModifiedAtNS        int64
}
type SkippedFile struct{ Path, Reason string }

type Scanner struct{ Config ScanConfig }

func (s Scanner) ScanFile(ctx context.Context, root, path string) (*ScannedFile, *SkippedFile, error) {
	select {
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	default:
	}
	guard := workspacefs.Guard{Root: root}
	rel, err := guard.Relative(path)
	if err != nil {
		return nil, &SkippedFile{Path: path, Reason: err.Error()}, nil
	}
	if reason := s.exclusionReason(rel); reason != "" {
		return nil, &SkippedFile{Path: rel, Reason: reason}, nil
	}
	full, err := guard.Resolve(rel)
	if err != nil {
		return nil, &SkippedFile{Path: rel, Reason: err.Error()}, nil
	}
	info, err := os.Stat(full)
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, &SkippedFile{Path: rel, Reason: "not_regular"}, nil
	}
	limit := s.Config.MaximumFileSizeBytes
	if limit <= 0 {
		limit = 1 << 20
	}
	if info.Size() > limit {
		return nil, &SkippedFile{Path: rel, Reason: "too_large"}, nil
	}
	content, err := os.ReadFile(full)
	if err != nil {
		return nil, nil, err
	}
	if looksBinary(content) {
		return nil, &SkippedFile{Path: rel, Reason: "binary"}, nil
	}
	hash := sha256.Sum256(content)
	return &ScannedFile{Path: filepath.ToSlash(rel), Language: languageFor(rel), Hash: hex.EncodeToString(hash[:]), GitStatus: gitFileStatus(ctx, root, rel), Size: info.Size(), ModifiedAtNS: info.ModTime().UnixNano(), Content: content}, nil, nil
}

func DefaultScanConfig() ScanConfig {
	return ScanConfig{
		MaximumFileSizeBytes: 1 << 20,
		ExcludedDirectories:  []string{".git", "node_modules", "vendor", "dist", "build", "coverage", "target", "tmp", "cache", ".cache", ".next", ".nuxt", "__pycache__"},
		SensitivePatterns:    []string{".env", ".env.*", "*.pem", "*.key", "*.p12", "*.pfx", "id_rsa", "id_ed25519", "credentials*", "secrets*", "*.sql", "*.dump"},
	}
}

func (s Scanner) Scan(ctx context.Context, root string) ([]ScannedFile, []SkippedFile, error) {
	metadata, skipped, err := s.Discover(ctx, root)
	if err != nil {
		return nil, skipped, err
	}
	files := make([]ScannedFile, 0, len(metadata))
	for _, item := range metadata {
		file, readSkip, readErr := s.ReadDiscoveredFile(ctx, root, item)
		if readErr != nil {
			skipped = append(skipped, SkippedFile{Path: item.Path, Reason: "unreadable"})
			continue
		}
		if readSkip != nil {
			skipped = append(skipped, *readSkip)
			continue
		}
		files = append(files, *file)
	}
	return files, skipped, nil
}

// Discover performs the cheap directory and stat pass. File contents are read
// only later for entries whose persistent metadata indicates a possible change.
func (s Scanner) Discover(ctx context.Context, root string) ([]FileMetadata, []SkippedFile, error) {
	guard := workspacefs.Guard{Root: root}
	paths, err := gitFiles(ctx, root)
	if err != nil {
		paths, err = s.walkFiles(root)
	}
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(paths)
	statuses := gitStatuses(ctx, root)
	var files []FileMetadata
	var skipped []SkippedFile
	for _, path := range paths {
		select {
		case <-ctx.Done():
			return files, skipped, ctx.Err()
		default:
		}
		rel, err := guard.Relative(path)
		if err != nil {
			skipped = append(skipped, SkippedFile{path, "outside_workspace"})
			continue
		}
		if reason := s.exclusionReason(rel); reason != "" {
			skipped = append(skipped, SkippedFile{rel, reason})
			continue
		}
		full, err := guard.Resolve(rel)
		if err != nil {
			skipped = append(skipped, SkippedFile{rel, err.Error()})
			continue
		}
		info, err := os.Stat(full)
		if err != nil || !info.Mode().IsRegular() {
			skipped = append(skipped, SkippedFile{rel, "not_regular"})
			continue
		}
		limit := s.Config.MaximumFileSizeBytes
		if limit <= 0 {
			limit = 1 << 20
		}
		if info.Size() > limit {
			skipped = append(skipped, SkippedFile{rel, "too_large"})
			continue
		}
		normalized := filepath.ToSlash(rel)
		files = append(files, FileMetadata{Path: normalized, Language: languageFor(rel), GitStatus: statuses[normalized], Size: info.Size(), ModifiedAtNS: info.ModTime().UnixNano()})
	}
	return files, skipped, nil
}

func (s Scanner) ReadDiscoveredFile(ctx context.Context, root string, item FileMetadata) (*ScannedFile, *SkippedFile, error) {
	guard := workspacefs.Guard{Root: root}
	full, err := guard.Resolve(item.Path)
	if err != nil {
		return nil, &SkippedFile{Path: item.Path, Reason: err.Error()}, nil
	}
	content, err := os.ReadFile(full)
	if err != nil {
		return nil, nil, err
	}
	if looksBinary(content) {
		return nil, &SkippedFile{Path: item.Path, Reason: "binary"}, nil
	}
	hash := sha256.Sum256(content)
	return &ScannedFile{Path: item.Path, Language: item.Language, Hash: hex.EncodeToString(hash[:]), GitStatus: item.GitStatus, Size: item.Size, ModifiedAtNS: item.ModifiedAtNS, Content: content}, nil, nil
}

func (s Scanner) exclusionReason(path string) string {
	normalized := filepath.ToSlash(path)
	if len(s.Config.IncludedPaths) > 0 && !matchesIncluded(normalized, s.Config.IncludedPaths) {
		return "not_included"
	}
	parts := strings.Split(normalized, "/")
	for _, part := range parts {
		for _, excluded := range s.Config.ExcludedDirectories {
			matched, _ := filepath.Match(excluded, part)
			cleanExcluded := strings.Trim(filepath.ToSlash(excluded), "/")
			if part == excluded || matched || normalized == cleanExcluded || strings.HasPrefix(normalized, cleanExcluded+"/") {
				return "excluded_directory"
			}
		}
	}
	base := filepath.Base(path)
	for _, pattern := range s.Config.SensitivePatterns {
		if ok, _ := filepath.Match(pattern, base); ok {
			return "sensitive"
		}
	}
	return ""
}
func matchesIncluded(path string, patterns []string) bool {
	for _, raw := range patterns {
		pattern := strings.Trim(filepath.ToSlash(raw), "/")
		if pattern == "" {
			return true
		}
		matched, _ := filepath.Match(pattern, path)
		if matched || path == pattern || strings.HasPrefix(path, pattern+"/") || strings.HasPrefix(pattern, path+"/") {
			return true
		}
	}
	return false
}
func looksBinary(data []byte) bool {
	sample := data
	if len(sample) > 8000 {
		sample = sample[:8000]
	}
	return bytes.IndexByte(sample, 0) >= 0
}
func languageFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".js", ".jsx":
		return "javascript"
	case ".ts", ".tsx":
		return "typescript"
	case ".php":
		return "php"
	case ".py":
		return "python"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".md":
		return "markdown"
	default:
		return "text"
	}
}
func gitFiles(ctx context.Context, root string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	raw := bytes.Split(out, []byte{0})
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if len(item) > 0 {
			result = append(result, filepath.Join(root, string(item)))
		}
	}
	return result, nil
}
func gitStatuses(ctx context.Context, root string) map[string]string {
	result := map[string]string{}
	cmd := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain=v1", "-z")
	out, err := cmd.Output()
	if err != nil {
		return result
	}
	for _, entry := range bytes.Split(out, []byte{0}) {
		if len(entry) < 4 {
			continue
		}
		status := strings.TrimSpace(string(entry[:2]))
		path := filepath.ToSlash(string(entry[3:]))
		result[path] = status
	}
	return result
}
func gitFileStatus(ctx context.Context, root, path string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain=v1", "--", path)
	out, err := cmd.Output()
	if err != nil || len(out) < 2 {
		return ""
	}
	return strings.TrimSpace(string(out[:2]))
}
func (s Scanner) walkFiles(root string) ([]string, error) {
	var result []string
	var ignorePatterns []string
	if data, err := os.ReadFile(filepath.Join(root, ".gitignore")); err == nil {
		ignorePatterns = ParseGitIgnore(data)
	}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel != "." && (isGitIgnored(rel, info.IsDir(), ignorePatterns) || s.exclusionReason(rel) == "excluded_directory") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		result = append(result, path)
		return nil
	})
	return result, err
}

func isGitIgnored(path string, isDir bool, patterns []string) bool {
	ignored := false
	path = strings.TrimPrefix(filepath.ToSlash(path), "./")
	for _, raw := range patterns {
		negated := strings.HasPrefix(raw, "!")
		pattern := strings.TrimPrefix(raw, "!")
		pattern = strings.TrimPrefix(pattern, "/")
		directoryOnly := strings.HasSuffix(pattern, "/")
		pattern = strings.TrimSuffix(pattern, "/")
		if directoryOnly && !isDir && !strings.HasPrefix(path, pattern+"/") {
			continue
		}
		matched := false
		if strings.Contains(pattern, "/") {
			matched, _ = filepath.Match(pattern, path)
			matched = matched || path == pattern || strings.HasPrefix(path, pattern+"/")
		} else {
			for _, part := range strings.Split(path, "/") {
				ok, _ := filepath.Match(pattern, part)
				if ok {
					matched = true
					break
				}
			}
		}
		if matched {
			ignored = !negated
		}
	}
	return ignored
}

// ParseGitIgnore is retained for non-Git workspaces and supports the common
// line-oriented subset used by project ignore files.
func ParseGitIgnore(data []byte) []string {
	var out []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}
