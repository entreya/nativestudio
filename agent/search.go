package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// registerSearchTools adds the search_text tool to the registry.
func registerSearchTools(r *Registry, workspaceRoot string) {
	r.Register(&Tool{
		Name:        "search_text",
		Description: "Search for text patterns across files in the workspace. Uses ripgrep if available, falls back to manual search.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The text pattern to search for.",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Relative path within workspace to limit search scope. Defaults to workspace root.",
				},
				"max_results": map[string]any{
					"type":        "integer",
					"description": "Maximum number of results to return. Defaults to 20.",
				},
			},
			"required": []string{"query"},
		},
		Safety: Safe,
		Execute: func(ctx context.Context, input ToolInput, meta ToolMeta) (ToolResult, error) {
			ws := meta.WorkspaceRoot
			query, _ := input["query"].(string)
			if query == "" {
				return ToolResult{OK: false, Error: "query is required"}, nil
			}

			searchPath, _ := input["path"].(string)
			maxResultsVal, _ := input["max_results"].(float64)
			maxResults := int(maxResultsVal)
			if maxResults <= 0 {
				maxResults = 20
			}

			searchRoot, err := safeJoin(ws, searchPath)
			if err != nil {
				return ToolResult{OK: false, Error: err.Error()}, nil
			}

			// Try ripgrep first
			results, err := searchWithRipgrep(ctx, query, searchRoot, ws, maxResults)
			if err != nil {
				// Fallback to manual search
				results, err = searchManual(query, searchRoot, ws, maxResults)
				if err != nil {
					return ToolResult{OK: false, Error: fmt.Sprintf("search failed: %v", err)}, nil
				}
			}

			return ToolResult{OK: true, Content: map[string]any{
				"results": results,
				"count":   len(results),
			}}, nil
		},
	})
}

type searchMatch struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

// searchWithRipgrep uses rg --json to find matches.
func searchWithRipgrep(ctx context.Context, query, searchRoot, wsRoot string, maxResults int) ([]searchMatch, error) {
	rgPath, err := exec.LookPath("rg")
	if err != nil {
		return nil, fmt.Errorf("ripgrep not found: %w", err)
	}

	cmd := exec.CommandContext(ctx, rgPath, "--json", "-n", "--max-count", "50", query, searchRoot)
	out, err := cmd.Output()
	if err != nil {
		// rg returns exit code 1 if no matches found — that's fine
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return []searchMatch{}, nil
		}
		return nil, fmt.Errorf("ripgrep error: %w", err)
	}

	absRoot, _ := filepath.Abs(wsRoot)
	var results []searchMatch

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()

		var entry struct {
			Type string `json:"type"`
			Data struct {
				Path struct {
					Text string `json:"text"`
				} `json:"path"`
				LineNumber int `json:"line_number"`
				Lines      struct {
					Text string `json:"text"`
				} `json:"lines"`
			} `json:"data"`
		}

		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		if entry.Type != "match" {
			continue
		}

		absPath := entry.Data.Path.Text
		relPath, err := filepath.Rel(absRoot, absPath)
		if err != nil {
			relPath = absPath
		}

		results = append(results, searchMatch{
			Path:    relPath,
			Line:    entry.Data.LineNumber,
			Content: strings.TrimRight(entry.Data.Lines.Text, "\n"),
		})

		if len(results) >= maxResults {
			break
		}
	}

	return results, nil
}

// searchManual is a fallback that walks the file tree and searches line-by-line.
func searchManual(query, searchRoot, wsRoot string, maxResults int) ([]searchMatch, error) {
	absRoot, _ := filepath.Abs(wsRoot)
	var results []searchMatch

	err := filepath.Walk(searchRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		// Skip binary-looking files and hidden files
		name := info.Name()
		if strings.HasPrefix(name, ".") || info.Size() > 1<<20 { // skip files > 1MB
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if strings.Contains(line, query) {
				relPath, err := filepath.Rel(absRoot, path)
				if err != nil {
					relPath = path
				}

				results = append(results, searchMatch{
					Path:    relPath,
					Line:    lineNum,
					Content: line,
				})

				if len(results) >= maxResults {
					return filepath.SkipAll
				}
			}
		}

		return nil
	})

	return results, err
}
