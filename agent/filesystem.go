package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	workspacefs "github.com/entreya/nativestudio/workspace"
)

// safeJoin validates and joins a requested path to the workspace root.
// It blocks any path that resolves outside the workspace.
func safeJoin(workspaceRoot, requestedPath string) (string, error) {
	return (workspacefs.Guard{Root: workspaceRoot}).Resolve(requestedPath)
}

// registerFilesystemTools adds list_files, read_file, read_file_range to the registry.
func registerFilesystemTools(r *Registry, workspaceRoot string) {
	r.Register(&Tool{
		Name:        "list_files",
		Description: "List files and directories in the workspace. Returns a recursive tree structure.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Relative path within workspace to list. Defaults to root.",
				},
				"depth": map[string]any{
					"type":        "integer",
					"description": "Maximum recursion depth. Defaults to 3.",
				},
			},
		},
		Safety: Safe,
		Execute: func(ctx context.Context, input ToolInput, meta ToolMeta) (ToolResult, error) {
			ws := meta.WorkspaceRoot
			path, _ := input["path"].(string)
			depthVal, _ := input["depth"].(float64)
			depth := int(depthVal)
			if depth <= 0 {
				depth = 3
			}

			root, err := safeJoin(ws, path)
			if err != nil {
				return ToolResult{OK: false, Error: err.Error()}, nil
			}

			tree, err := buildTree(root, ws, depth, 0)
			if err != nil {
				return ToolResult{OK: false, Error: err.Error()}, nil
			}

			return ToolResult{OK: true, Content: tree}, nil
		},
	})

	r.Register(&Tool{
		Name:        "read_file",
		Description: "Read the entire contents of a file in the workspace. Project-relative paths may be written with or without a leading slash.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Project-relative path within the workspace, with or without a leading slash.",
				},
			},
			"required": []string{"path"},
		},
		Safety: Safe,
		Execute: func(ctx context.Context, input ToolInput, meta ToolMeta) (ToolResult, error) {
			ws := meta.WorkspaceRoot
			path, _ := input["path"].(string)
			if path == "" {
				return ToolResult{OK: false, Error: "path is required"}, nil
			}

			fullPath, err := safeJoin(ws, path)
			if err != nil {
				return ToolResult{OK: false, Error: err.Error()}, nil
			}

			info, err := os.Stat(fullPath)
			if err != nil {
				return ToolResult{OK: false, Error: "file_not_found"}, nil
			}
			if info.IsDir() {
				return ToolResult{OK: false, Error: "path is a directory, not a file"}, nil
			}

			data, err := os.ReadFile(fullPath)
			if err != nil {
				return ToolResult{OK: false, Error: fmt.Sprintf("read error: %v", err)}, nil
			}

			content := string(data)
			lines := strings.Count(content, "\n") + 1

			return ToolResult{OK: true, Content: map[string]any{
				"content": content,
				"lines":   lines,
			}}, nil
		},
	})

	r.Register(&Tool{
		Name:        "read_file_range",
		Description: "Read a specific range of lines from a file in the workspace.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Project-relative path within the workspace, with or without a leading slash.",
				},
				"start_line": map[string]any{
					"type":        "integer",
					"description": "First line to read (1-indexed).",
				},
				"end_line": map[string]any{
					"type":        "integer",
					"description": "Last line to read (1-indexed, inclusive).",
				},
			},
			"required": []string{"path", "start_line", "end_line"},
		},
		Safety: Safe,
		Execute: func(ctx context.Context, input ToolInput, meta ToolMeta) (ToolResult, error) {
			ws := meta.WorkspaceRoot
			path, _ := input["path"].(string)
			// Arguments come from model-generated JSON, which may omit "required"
			// fields or send the wrong type — assert with comma-ok instead of a
			// direct type assertion so a malformed call returns an error instead
			// of panicking the request.
			startLineVal, startOK := input["start_line"].(float64)
			endLineVal, endOK := input["end_line"].(float64)

			if path == "" {
				return ToolResult{OK: false, Error: "path is required"}, nil
			}
			if !startOK || !endOK {
				return ToolResult{OK: false, Error: "start_line and end_line must be numbers"}, nil
			}
			startLine := int(startLineVal)
			endLine := int(endLineVal)
			if startLine < 1 || endLine < startLine {
				return ToolResult{OK: false, Error: "invalid line range"}, nil
			}

			fullPath, err := safeJoin(ws, path)
			if err != nil {
				return ToolResult{OK: false, Error: err.Error()}, nil
			}

			f, err := os.Open(fullPath)
			if err != nil {
				return ToolResult{OK: false, Error: "file_not_found"}, nil
			}
			defer f.Close()

			var lines []string
			scanner := bufio.NewScanner(f)
			lineNum := 0
			for scanner.Scan() {
				lineNum++
				if lineNum >= startLine && lineNum <= endLine {
					lines = append(lines, scanner.Text())
				}
				if lineNum > endLine {
					break
				}
			}

			if len(lines) == 0 {
				return ToolResult{OK: false, Error: "line range out of bounds"}, nil
			}

			return ToolResult{OK: true, Content: map[string]any{
				"content":    strings.Join(lines, "\n"),
				"start_line": startLine,
				"end_line":   startLine + len(lines) - 1,
			}}, nil
		},
	})
}

// fileNode represents a single entry in the file tree.
type fileNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	Type     string      `json:"type"`
	Children []*fileNode `json:"children,omitempty"`
}

// buildTree recursively builds a file tree up to maxDepth.
func buildTree(dir, wsRoot string, maxDepth, currentDepth int) ([]*fileNode, error) {
	if currentDepth >= maxDepth {
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var nodes []*fileNode
	for _, entry := range entries {
		name := entry.Name()
		// Skip hidden files/dirs and common noise
		if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "__pycache__" {
			continue
		}

		absPath := filepath.Join(dir, name)
		absRoot, _ := filepath.Abs(wsRoot)
		relPath, _ := filepath.Rel(absRoot, absPath)

		node := &fileNode{
			Name: name,
			Path: relPath,
		}

		if entry.IsDir() {
			node.Type = "dir"
			children, err := buildTree(absPath, wsRoot, maxDepth, currentDepth+1)
			if err == nil {
				node.Children = children
			}
		} else {
			node.Type = "file"
		}

		nodes = append(nodes, node)
	}

	// Sort: directories first, then alphabetical
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Type != nodes[j].Type {
			return nodes[i].Type == "dir"
		}
		return nodes[i].Name < nodes[j].Name
	})

	return nodes, nil
}
