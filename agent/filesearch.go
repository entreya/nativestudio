package agent

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/entreya/nativestudio/editor"
	workspacefs "github.com/entreya/nativestudio/workspace"
)

// Safety defaults for the file-search tools. Limits are clamped, never
// trusted verbatim from model input.
const (
	defaultFindFilesLimit = 50
	maxFindFilesLimit     = 200
	defaultListDirLimit   = 200
	maxListDirLimit       = 500
	defaultListDirDepth   = 1
	maxListDirDepth       = 5
	findFilesTimeout      = 5 * time.Second
	listDirectoryTimeout  = 5 * time.Second
	maxRelativePathLength = 4096
	// maxFilesScanned bounds the number of eligible (non-ignored) entries a
	// single find_files call will examine, independent of how many actually
	// match — without this, a query with zero matches against a huge repo
	// would still walk the entire tree before returning.
	maxFilesScanned = 50000
)

// FindFilesInput is the typed shape of find_files' arguments, decoded from
// the raw ToolInput map.
type FindFilesInput struct {
	Query      string
	Path       string
	Extensions []string
	FileTypes  []string
	Limit      int
}

// ListDirectoryInput is the typed shape of list_directory's arguments,
// decoded from the raw ToolInput map.
type ListDirectoryInput struct {
	Path          string
	Depth         int
	Limit         int
	IncludeHidden bool
}

type fileMatch struct {
	Path      string   `json:"path"`
	Name      string   `json:"name"`
	Type      string   `json:"type"` // "file" or "directory"
	Extension string   `json:"extension,omitempty"`
	Score     int      `json:"score"`
	Reasons   []string `json:"reasons"`
}

type findFilesOutput struct {
	Matches      []fileMatch `json:"matches"`
	SearchedPath string      `json:"searched_path"`
	TotalMatches int         `json:"total_matches"`
	Truncated    bool        `json:"truncated"`
}

// registerFileSearchTools adds find_files and list_directory — structured,
// read-only repository discovery tools that let the model locate files
// without exact paths and without unrestricted filesystem or shell access.
func registerFileSearchTools(r *Registry) {
	r.Register(&Tool{
		Name: "find_files",
		Description: "Search filenames and relative paths inside the workspace to locate a file without knowing its exact path. " +
			"Matches basenames, filenames without extension, directory names, and normalized tokens (CamelCase/snake_case/kebab-case are " +
			"all treated as equivalent). Use this before guessing a path — never invent one.",
		Parameters: objectParameters(map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Filename, partial name, or natural-language term to search for, e.g. \"AuthService\", \"auth service\", or \"student eligibility\".",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Directory to search within, relative to the workspace root. Defaults to the whole workspace.",
			},
			"extensions": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Limit results to these file extensions, with or without a leading dot (e.g. \"php\" or \".php\").",
			},
			"file_types": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string", "enum": []string{"file", "directory"}},
				"description": "Limit results to files, directories, or both. Defaults to both.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("Maximum number of results to return (default %d, maximum %d).", defaultFindFilesLimit, maxFindFilesLimit),
			},
		}, "query"),
		Safety:  Safe,
		Execute: executeFindFiles,
	})

	r.Register(&Tool{
		Name: "list_directory",
		Description: "List the contents of a directory in the workspace (directories first, then files, alphabetically). " +
			"Use this when the user refers to a module or folder rather than a specific file. Does not read file content.",
		Parameters: objectParameters(map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Directory to list, relative to the workspace root. Defaults to the workspace root.",
			},
			"depth": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("How many directory levels to descend (default %d, maximum %d).", defaultListDirDepth, maxListDirDepth),
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("Maximum number of entries to return (default %d, maximum %d).", defaultListDirLimit, maxListDirLimit),
			},
			"include_hidden": map[string]any{
				"type":        "boolean",
				"description": "Include dotfiles/dot-directories. Defaults to false.",
			},
		}),
		Safety:  Safe,
		Execute: executeListDirectory,
	})
}

func decodeFindFilesInput(input ToolInput) FindFilesInput {
	query, _ := input["query"].(string)
	pathValue, _ := input["path"].(string)
	limitValue, _ := input["limit"].(float64)
	return FindFilesInput{
		Query: strings.TrimSpace(query),
		Path:  strings.TrimSpace(pathValue),
		// Not stringSlice: that helper silently drops empty-string entries,
		// which is right for things like follow-up option lists but wrong
		// here — an explicit but empty/malformed extensions or file_types
		// entry should surface as a validation error, not get silently
		// treated as "no filter".
		Extensions: rawStringSlice(input["extensions"]),
		FileTypes:  rawStringSlice(input["file_types"]),
		Limit:      int(limitValue),
	}
}

// rawStringSlice decodes a JSON array of strings without dropping empty
// entries, so validation (e.g. normalizeExtensions) can reject them.
func rawStringSlice(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func decodeListDirectoryInput(input ToolInput) ListDirectoryInput {
	pathValue, _ := input["path"].(string)
	depthValue, _ := input["depth"].(float64)
	limitValue, _ := input["limit"].(float64)
	includeHidden, _ := input["include_hidden"].(bool)
	return ListDirectoryInput{
		Path:          strings.TrimSpace(pathValue),
		Depth:         int(depthValue),
		Limit:         int(limitValue),
		IncludeHidden: includeHidden,
	}
}

func executeFindFiles(ctx context.Context, rawInput ToolInput, meta ToolMeta) (ToolResult, error) {
	input := decodeFindFilesInput(rawInput)
	if input.Query == "" {
		return ToolResult{OK: false, Error: "query is required"}, nil
	}

	searchPath := input.Path
	if searchPath == "" {
		searchPath = "."
	}
	guard := workspacefs.Guard{}
	searchRoot, err := guard.ResolveDirectory(meta.WorkspaceRoot, searchPath)
	if err != nil {
		return ToolResult{OK: false, Error: err.Error()}, nil
	}

	extensions, err := normalizeExtensions(input.Extensions)
	if err != nil {
		return ToolResult{OK: false, Error: err.Error()}, nil
	}
	fileTypes, err := normalizeFileTypes(input.FileTypes)
	if err != nil {
		return ToolResult{OK: false, Error: err.Error()}, nil
	}

	limit := clamp(input.Limit, defaultFindFilesLimit, 1, maxFindFilesLimit)

	matcher := workspacefs.NewIgnoreMatcher(meta.WorkspaceRoot)
	sensitive := workspacefs.NewSensitivePathPolicy()

	queryLower := strings.ToLower(input.Query)
	queryTokens := tokenize(input.Query)
	normalizedQuery := strings.Join(queryTokens, "")
	recentFiles, activeDir := editorRankingContext(meta.State)

	searchCtx, cancel := context.WithTimeout(ctx, findFilesTimeout)
	defer cancel()

	type scored struct {
		match fileMatch
		score int
	}
	var candidates []scored
	scanned := 0
	truncated := false

	walkErr := filepath.WalkDir(searchRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entry — skip it, don't abort the whole search
		}
		select {
		case <-searchCtx.Done():
			return searchCtx.Err()
		default:
		}
		if p == searchRoot {
			return nil
		}
		rel, relErr := filepath.Rel(meta.WorkspaceRoot, p)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		// d.IsDir() reflects the dirent's own type, which is never "dir" for
		// a symlink even when it points at one inside the workspace — and is
		// "true" only when the symlink itself is a broken/escaping one we
		// must not treat as usable. ResolveWalkEntry follows the link and
		// enforces both.
		isDir, ok := workspacefs.ResolveWalkEntry(meta.WorkspaceRoot, d, p)
		if !ok {
			// Only reachable for a symlink (broken, or escaping the
			// workspace) — WalkDir never auto-descends into symlinks anyway,
			// so simply not reporting it is enough; there is nothing to skip.
			return nil
		}
		if matcher.ShouldIgnore(rel, isDir) {
			if isDir {
				return filepath.SkipDir
			}
			return nil
		}
		if len(rel) > maxRelativePathLength {
			if isDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !isDir {
			if ok, _ := sensitive.IsSensitive(rel); ok {
				return nil
			}
		}

		scanned++
		if scanned > maxFilesScanned {
			truncated = true
			return filepath.SkipAll
		}

		entryType := "file"
		if isDir {
			entryType = "directory"
		}
		if !fileTypes[entryType] {
			return nil
		}

		name := d.Name()
		nameNoExt := name
		ext := ""
		if !isDir {
			ext = strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
			if ext != "" {
				nameNoExt = name[:len(name)-len(ext)-1]
			}
			if len(extensions) > 0 && !extensions[ext] {
				return nil
			}
		}

		score, reasons := scoreFileCandidate(rel, name, nameNoExt, queryLower, queryTokens, normalizedQuery, recentFiles, activeDir)
		if score <= 0 {
			return nil
		}
		candidates = append(candidates, scored{
			match: fileMatch{Path: rel, Name: name, Type: entryType, Extension: ext, Score: score, Reasons: reasons},
			score: score,
		})
		return nil
	})
	if walkErr != nil {
		// Timeout/cancellation/SkipAll all mean "stopped early" — return
		// whatever was found rather than failing the whole call.
		truncated = true
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if len(a.match.Path) != len(b.match.Path) {
			return len(a.match.Path) < len(b.match.Path)
		}
		if a.match.Name != b.match.Name {
			return a.match.Name < b.match.Name
		}
		return a.match.Path < b.match.Path
	})

	total := len(candidates)
	if total > limit {
		candidates = candidates[:limit]
		truncated = true
	}
	matches := make([]fileMatch, len(candidates))
	for i, c := range candidates {
		matches[i] = c.match
	}

	return ToolResult{OK: true, Content: findFilesOutput{
		Matches:      matches,
		SearchedPath: searchPath,
		TotalMatches: total,
		Truncated:    truncated,
	}}, nil
}

// editorRankingContext turns editor state into lookup structures for the
// "recently opened file" and "active directory" ranking bonuses. Both are
// optional signals — an empty EditorState (no session/editor context
// available) simply yields no bonuses.
func editorRankingContext(state editor.EditorState) (recentFiles map[string]bool, activeDir string) {
	recentFiles = map[string]bool{}
	normalize := func(p string) string {
		return strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(p)), "/")
	}
	if state.ActiveFile != "" {
		recentFiles[normalize(state.ActiveFile)] = true
		activeDir = path.Dir(normalize(state.ActiveFile))
	}
	for _, f := range state.RecentFiles {
		if f != "" {
			recentFiles[normalize(f)] = true
		}
	}
	for _, f := range state.OpenFiles {
		if f != "" {
			recentFiles[normalize(f)] = true
		}
	}
	return recentFiles, activeDir
}

func scoreFileCandidate(relPath, name, nameNoExt, queryLower string, queryTokens []string, normalizedQuery string, recentFiles map[string]bool, activeDir string) (int, []string) {
	nameLower := strings.ToLower(name)
	nameNoExtLower := strings.ToLower(nameNoExt)
	pathLower := strings.ToLower(relPath)
	nameTokens := tokenize(nameNoExt)
	pathTokens := tokenize(relPath)
	normalizedName := strings.Join(nameTokens, "")
	nameJoined := " " + strings.Join(nameTokens, " ") + " "
	pathJoined := " " + strings.Join(pathTokens, " ") + " "

	var base int
	var reason string
	switch {
	case pathLower == queryLower:
		base, reason = 120, "Exact relative path match"
	case nameLower == queryLower:
		base, reason = 110, "Exact basename match"
	case nameNoExtLower == queryLower:
		base, reason = 105, "Exact filename-without-extension match"
	case normalizedName != "" && normalizedQuery != "" && normalizedName == normalizedQuery:
		base, reason = 100, "Exact normalized-name match"
	case strings.HasPrefix(nameNoExtLower, queryLower) || strings.HasPrefix(nameLower, queryLower):
		base, reason = 90, "Filename starts with query"
	case strings.Contains(nameNoExtLower, queryLower) || strings.Contains(nameLower, queryLower):
		base, reason = 85, "Filename contains exact query"
	case allTokensPresent(queryTokens, nameTokens):
		base, reason = 80, "All query tokens occur in filename"
	case allTokensPresent(queryTokens, pathTokens):
		base, reason = 70, "All query tokens occur in relative path"
	case anyTokenSubstring(queryTokens, nameJoined):
		base, reason = 55, "Partial token match in filename"
	case anyTokenSubstring(queryTokens, pathJoined):
		base, reason = 40, "Partial token match in path"
	default:
		return 0, nil
	}

	score := base
	reasons := []string{reason}
	if recentFiles[strings.TrimPrefix(relPath, "/")] {
		score += 15
		reasons = append(reasons, "Recently opened file bonus")
	}
	if activeDir != "" && path.Dir(relPath) == activeDir {
		score += 10
		reasons = append(reasons, "Active directory bonus")
	}
	return score, reasons
}

func allTokensPresent(queryTokens, targetTokens []string) bool {
	if len(queryTokens) == 0 {
		return false
	}
	set := make(map[string]bool, len(targetTokens))
	for _, t := range targetTokens {
		set[t] = true
	}
	for _, qt := range queryTokens {
		if !set[qt] {
			return false
		}
	}
	return true
}

func anyTokenSubstring(queryTokens []string, spaceJoinedTarget string) bool {
	for _, qt := range queryTokens {
		if qt != "" && strings.Contains(spaceJoinedTarget, qt) {
			return true
		}
	}
	return false
}

// tokenize splits a name into lowercase tokens on CamelCase boundaries,
// snake_case/kebab-case separators, spaces, dots, and path separators — so
// "UserAuthService", "user-auth-service", "user_auth_service", and
// "user auth service" all normalize to the same ["user","auth","service"].
func tokenize(value string) []string {
	var tokens []string
	var current strings.Builder
	runes := []rune(value)
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, strings.ToLower(current.String()))
			current.Reset()
		}
	}
	for i, r := range runes {
		switch {
		case r == '_' || r == '-' || r == ' ' || r == '.' || r == '/' || r == '\\':
			flush()
		case unicode.IsUpper(r):
			if current.Len() > 0 {
				prev := runes[i-1]
				startsNewWord := unicode.IsLower(prev) || unicode.IsDigit(prev)
				endsAcronym := !startsNewWord && unicode.IsUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1])
				if startsNewWord || endsAcronym {
					flush()
				}
			}
			current.WriteRune(r)
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return tokens
}

var validExtensionPattern = regexp.MustCompile(`^[a-zA-Z0-9]+$`)

// normalizeExtensions accepts extensions with or without a leading dot and
// rejects anything that isn't a plain alphanumeric extension (e.g. path
// separators or glob characters smuggled in as an "extension").
func normalizeExtensions(raw []string) (map[string]bool, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	set := make(map[string]bool, len(raw))
	for _, entry := range raw {
		trimmed := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(entry)), ".")
		if !validExtensionPattern.MatchString(trimmed) {
			return nil, fmt.Errorf("invalid extension: %q", entry)
		}
		set[trimmed] = true
	}
	return set, nil
}

// normalizeFileTypes validates the file_types filter and defaults to both
// files and directories when omitted.
func normalizeFileTypes(raw []string) (map[string]bool, error) {
	if len(raw) == 0 {
		return map[string]bool{"file": true, "directory": true}, nil
	}
	set := make(map[string]bool, len(raw))
	for _, entry := range raw {
		normalized := strings.ToLower(strings.TrimSpace(entry))
		if normalized != "file" && normalized != "directory" {
			return nil, fmt.Errorf("invalid file_type: %q (expected \"file\" or \"directory\")", entry)
		}
		set[normalized] = true
	}
	return set, nil
}

func clamp(value, defaultValue, min, max int) int {
	if value <= 0 {
		value = defaultValue
	}
	if value < min {
		value = min
	}
	if value > max {
		value = max
	}
	return value
}

type directoryEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Type      string `json:"type"` // "file" or "directory"
	Extension string `json:"extension,omitempty"`
	Size      int64  `json:"size,omitempty"`
}

type listDirectoryOutput struct {
	Path      string           `json:"path"`
	Entries   []directoryEntry `json:"entries"`
	Truncated bool             `json:"truncated"`
}

func executeListDirectory(ctx context.Context, rawInput ToolInput, meta ToolMeta) (ToolResult, error) {
	input := decodeListDirectoryInput(rawInput)
	targetPath := input.Path
	if targetPath == "" {
		targetPath = "."
	}
	guard := workspacefs.Guard{}
	root, err := guard.ResolveDirectory(meta.WorkspaceRoot, targetPath)
	if err != nil {
		return ToolResult{OK: false, Error: err.Error()}, nil
	}

	depth := clamp(input.Depth, defaultListDirDepth, 1, maxListDirDepth)
	limit := clamp(input.Limit, defaultListDirLimit, 1, maxListDirLimit)

	matcher := workspacefs.NewIgnoreMatcher(meta.WorkspaceRoot)
	sensitive := workspacefs.NewSensitivePathPolicy()

	listCtx, cancel := context.WithTimeout(ctx, listDirectoryTimeout)
	defer cancel()

	var entries []directoryEntry
	truncated := false
	limitReached := errors.New("limit_reached")

	var walk func(dir string, currentDepth int) error
	walk = func(dir string, currentDepth int) error {
		select {
		case <-listCtx.Done():
			return listCtx.Err()
		default:
		}
		items, readErr := os.ReadDir(dir)
		if readErr != nil {
			return nil // unreadable directory — just contributes nothing
		}
		// Directories first, then files, alphabetically — sorting the raw
		// entries up front makes both this level's order and (for nested
		// levels) the overall listing order match that rule.
		sort.Slice(items, func(i, j int) bool {
			iDir, jDir := items[i].IsDir(), items[j].IsDir()
			if iDir != jDir {
				return iDir
			}
			return strings.ToLower(items[i].Name()) < strings.ToLower(items[j].Name())
		})
		for _, item := range items {
			name := item.Name()
			if !input.IncludeHidden && strings.HasPrefix(name, ".") {
				continue
			}
			full := filepath.Join(dir, name)
			rel, relErr := filepath.Rel(meta.WorkspaceRoot, full)
			if relErr != nil {
				continue
			}
			rel = filepath.ToSlash(rel)

			// os.ReadDir's entries satisfy fs.DirEntry, same as WalkDir's —
			// ResolveWalkEntry follows symlinks and refuses ones that escape
			// the workspace, exactly as find_files does.
			isDir, ok := workspacefs.ResolveWalkEntry(meta.WorkspaceRoot, item, full)
			if !ok {
				continue
			}
			if matcher.ShouldIgnore(rel, isDir) {
				continue
			}
			if !isDir {
				if hit, _ := sensitive.IsSensitive(rel); hit {
					continue
				}
			}

			if len(entries) >= limit {
				return limitReached
			}

			entry := directoryEntry{Name: name, Path: rel, Type: "file"}
			if isDir {
				entry.Type = "directory"
			} else {
				entry.Extension = strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
				if info, infoErr := item.Info(); infoErr == nil {
					entry.Size = info.Size()
				}
			}
			entries = append(entries, entry)

			if isDir && currentDepth < depth {
				if walkErr := walk(full, currentDepth+1); walkErr != nil {
					return walkErr
				}
			}
		}
		return nil
	}

	if walkErr := walk(root, 1); walkErr != nil {
		truncated = true
	}

	return ToolResult{OK: true, Content: listDirectoryOutput{
		Path:      targetPath,
		Entries:   entries,
		Truncated: truncated,
	}}, nil
}
