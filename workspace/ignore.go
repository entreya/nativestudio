package workspace

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// IgnoreMatcher decides whether a workspace-relative path should be hidden
// from file-search tools — dependency/build directories, VCS metadata, and
// (via the root .gitignore) anything the project itself excludes. Both
// find_files and list_directory share one matcher instance per call instead
// of hardcoding exclusion checks separately.
type IgnoreMatcher interface {
	ShouldIgnore(relativePath string, isDirectory bool) bool
}

// DefaultExcludedDirectories are always skipped regardless of .gitignore —
// dependency and build-output directories that are rarely what a "find this
// file" request is looking for, and can be enormous.
var DefaultExcludedDirectories = []string{
	".git", ".hg", ".svn", "node_modules", "vendor", "dist", "build", "out",
	"target", "coverage", ".cache", ".next", ".nuxt", "tmp", "temp", "logs",
	"storage/logs", ".idea", ".vscode", "__pycache__", ".venv", "venv",
}

type gitignoreMatcher struct {
	excludedDirs []string
	patterns     []string
}

// NewIgnoreMatcher builds an IgnoreMatcher for workspaceRoot: the default
// excluded-directory list plus the root-level .gitignore, if present.
//
// Nested .gitignore files are not consulted in this version — only the
// workspace root's .gitignore is parsed, per the mandatory first-version
// scope. A directory-level default list still covers the overwhelming
// majority of what nested .gitignore files exist to exclude (node_modules,
// build output, caches), so this is a reasonable initial approximation.
func NewIgnoreMatcher(workspaceRoot string) IgnoreMatcher {
	m := &gitignoreMatcher{excludedDirs: append([]string(nil), DefaultExcludedDirectories...)}
	if data, err := os.ReadFile(filepath.Join(workspaceRoot, ".gitignore")); err == nil {
		m.patterns = parseGitignoreLines(data)
	}
	return m
}

func (m *gitignoreMatcher) ShouldIgnore(relativePath string, isDirectory bool) bool {
	normalized := strings.TrimPrefix(filepath.ToSlash(relativePath), "./")
	if normalized == "" || normalized == "." {
		return false
	}
	for _, part := range strings.Split(normalized, "/") {
		for _, excluded := range m.excludedDirs {
			if part == excluded {
				return true
			}
		}
	}
	return matchesGitignorePatterns(normalized, isDirectory, m.patterns)
}

func parseGitignoreLines(data []byte) []string {
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

// matchesGitignorePatterns applies a minimal but correct subset of gitignore
// semantics: later patterns override earlier ones, "!" negates, a trailing
// "/" restricts a pattern to directories, and a pattern containing "/" is
// matched against the full relative path while a bare pattern is matched
// against any path segment.
func matchesGitignorePatterns(path string, isDir bool, patterns []string) bool {
	ignored := false
	for _, raw := range patterns {
		negated := strings.HasPrefix(raw, "!")
		pattern := strings.TrimPrefix(raw, "!")
		pattern = strings.TrimPrefix(pattern, "/")
		directoryOnly := strings.HasSuffix(pattern, "/")
		pattern = strings.TrimSuffix(pattern, "/")
		if pattern == "" {
			continue
		}
		if directoryOnly && !isDir && !strings.HasPrefix(path, pattern+"/") {
			continue
		}
		matched := false
		if strings.Contains(pattern, "/") {
			matched, _ = filepath.Match(pattern, path)
			matched = matched || path == pattern || strings.HasPrefix(path, pattern+"/")
		} else {
			for _, part := range strings.Split(path, "/") {
				if ok, _ := filepath.Match(pattern, part); ok {
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

// SensitivePathPolicy decides whether a workspace-relative path looks like it
// holds credentials or secret material and should be hidden from the model
// by default, returning the reason so callers can log/report it.
type SensitivePathPolicy interface {
	IsSensitive(relativePath string) (bool, string)
}

type sensitivePattern struct{ pattern, reason string }

// defaultSensitivePatterns match basenames only — never a directory name or
// full path — so a legitimately named source file such as
// "SecretService.php" is never blocked just because "secret" appears in it;
// only filenames actually shaped like credential/secret material are.
var defaultSensitivePatterns = []sensitivePattern{
	{".env", "environment file"},
	{".env.*", "environment file"},
	{"*.pem", "private key material"},
	{"*.key", "private key material"},
	{"*.p12", "private key material"},
	{"*.pfx", "private key material"},
	{"id_rsa", "SSH private key"},
	{"id_ed25519", "SSH private key"},
	{"credentials.json", "credential file"},
	{"service-account*.json", "cloud service-account credential"},
	{"secrets.*", "secret file"},
	{".npmrc", "package registry credential"},
	{".pypirc", "package registry credential"},
	{".netrc", "network credential"},
	{"*.sql", "database dump"},
	{"*.dump", "database dump"},
	{"*.sqlite", "database file"},
	{"*.sqlite3", "database file"},
}

type defaultSensitivePolicy struct{}

// NewSensitivePathPolicy returns the default credential/secret filename policy.
func NewSensitivePathPolicy() SensitivePathPolicy { return defaultSensitivePolicy{} }

func (defaultSensitivePolicy) IsSensitive(relativePath string) (bool, string) {
	base := filepath.Base(filepath.ToSlash(relativePath))
	for _, entry := range defaultSensitivePatterns {
		if ok, _ := filepath.Match(entry.pattern, base); ok {
			return true, entry.reason
		}
	}
	return false, ""
}
