package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/entreya/nativestudio/db"
)

func registerPatchTools(r *Registry) {
	r.Register(&Tool{
		Name:        "replace_in_file",
		Description: "Replace one exact text string in a file. The match must include exact whitespace and indentation. The change is staged for review and is not written until approved.",
		Parameters: objectParameters(map[string]any{
			"path":     map[string]any{"type": "string", "description": "File path relative to the workspace root."},
			"old_text": map[string]any{"type": "string", "description": "Exact text to replace; it must occur exactly once."},
			"new_text": map[string]any{"type": "string", "description": "Replacement text."},
		}, "path", "old_text", "new_text"),
		Safety:  RequiresApproval,
		Execute: replaceInFile,
	})
	r.Register(&Tool{
		Name:        "apply_patch",
		Description: "Apply a standard unified diff to one file. Use for multiple edits in the same file. The result is staged for review and is not written until approved.",
		Parameters: objectParameters(map[string]any{
			"path": map[string]any{"type": "string", "description": "File path relative to the workspace root."},
			"diff": map[string]any{"type": "string", "description": "Unified diff in standard ---/+++/@@ format."},
		}, "path", "diff"),
		Safety:  RequiresApproval,
		Execute: applyPatchTool,
	})
	r.Register(&Tool{
		Name:        "create_file",
		Description: "Create a new file with the supplied complete content. The creation is staged for review and is not written until approved.",
		Parameters: objectParameters(map[string]any{
			"path":    map[string]any{"type": "string", "description": "New file path relative to the workspace root."},
			"content": map[string]any{"type": "string", "description": "Complete content for the new file."},
		}, "path", "content"),
		Safety:  RequiresApproval,
		Execute: createFileTool,
	})
	r.Register(&Tool{
		Name:        "delete_file",
		Description: "Delete one file. The deletion is staged for explicit review and the file remains unchanged until approved.",
		Parameters: objectParameters(map[string]any{
			"path": map[string]any{"type": "string", "description": "File path relative to the workspace root."},
		}, "path"),
		Safety:  Dangerous,
		Execute: deleteFileTool,
	})
}

func objectParameters(properties map[string]any, required ...string) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "required": required}
}

func replaceInFile(ctx context.Context, input ToolInput, meta ToolMeta) (ToolResult, error) {
	path, _ := input["path"].(string)
	oldText, _ := input["old_text"].(string)
	newText, _ := input["new_text"].(string)
	if oldText == "" {
		return ToolResult{OK: false, Error: "old_text is required"}, nil
	}
	full, relative, result := resolvePatchPath(meta, path)
	if !result.OK {
		return result, nil
	}
	current, err := os.ReadFile(full)
	if os.IsNotExist(err) {
		return ToolResult{OK: false, Error: "file_not_found"}, nil
	}
	if err != nil {
		return ToolResult{OK: false, Error: err.Error()}, nil
	}
	count := strings.Count(string(current), oldText)
	if count == 0 {
		return ToolResult{OK: false, Error: "text_not_found"}, nil
	}
	if count > 1 {
		return ToolResult{OK: false, Error: "text_ambiguous", Content: map[string]any{"count": count}}, nil
	}
	updated := strings.Replace(string(current), oldText, newText, 1)
	return stagePatch(ctx, meta, relative, "modify", computeUnifiedDiff(relative, string(current), updated), updated, string(current))
}

func applyPatchTool(ctx context.Context, input ToolInput, meta ToolMeta) (ToolResult, error) {
	path, _ := input["path"].(string)
	diff, _ := input["diff"].(string)
	full, relative, result := resolvePatchPath(meta, path)
	if !result.OK {
		return result, nil
	}
	current, err := os.ReadFile(full)
	if os.IsNotExist(err) {
		return ToolResult{OK: false, Error: "file_not_found"}, nil
	}
	if err != nil {
		return ToolResult{OK: false, Error: err.Error()}, nil
	}
	updated, err := applyUnifiedDiff(string(current), diff)
	if err != nil {
		return ToolResult{OK: false, Error: "patch_failed", Content: map[string]any{"reason": err.Error()}}, nil
	}
	return stagePatch(ctx, meta, relative, "modify", diff, updated, string(current))
}

func createFileTool(ctx context.Context, input ToolInput, meta ToolMeta) (ToolResult, error) {
	path, _ := input["path"].(string)
	content, _ := input["content"].(string)
	full, relative, result := resolvePatchPath(meta, path)
	if !result.OK {
		return result, nil
	}
	if _, err := os.Lstat(full); err == nil {
		return ToolResult{OK: false, Error: "already_exists"}, nil
	} else if !os.IsNotExist(err) {
		return ToolResult{OK: false, Error: err.Error()}, nil
	}
	return stagePatch(ctx, meta, relative, "create", computeUnifiedDiff(relative, "", content), content, "")
}

func deleteFileTool(ctx context.Context, input ToolInput, meta ToolMeta) (ToolResult, error) {
	path, _ := input["path"].(string)
	full, relative, result := resolvePatchPath(meta, path)
	if !result.OK {
		return result, nil
	}
	current, err := os.ReadFile(full)
	if os.IsNotExist(err) {
		return ToolResult{OK: false, Error: "file_not_found"}, nil
	}
	if err != nil {
		return ToolResult{OK: false, Error: err.Error()}, nil
	}
	return stagePatch(ctx, meta, relative, "delete", computeUnifiedDiff(relative, string(current), ""), "", string(current))
}

func resolvePatchPath(meta ToolMeta, requested string) (string, string, ToolResult) {
	if strings.TrimSpace(requested) == "" {
		return "", "", ToolResult{OK: false, Error: "path is required"}
	}
	full, err := safeJoin(meta.WorkspaceRoot, requested)
	if err != nil {
		return "", "", ToolResult{OK: false, Error: "outside_workspace"}
	}
	relative, err := filepath.Rel(meta.WorkspaceRoot, full)
	if err != nil {
		return "", "", ToolResult{OK: false, Error: "outside_workspace"}
	}
	return full, filepath.ToSlash(relative), ToolResult{OK: true}
}

func stagePatch(ctx context.Context, meta ToolMeta, path, operation, diff, updated, original string) (ToolResult, error) {
	if meta.DB == nil || meta.SessionID == "" || meta.RunID == "" {
		return ToolResult{OK: false, Error: "patch_metadata_unavailable"}, nil
	}
	if operation == "modify" && diff == "" {
		return ToolResult{OK: false, Error: "no_changes"}, nil
	}
	patch := db.Patch{ID: db.NewID(), SessionID: meta.SessionID, RunID: meta.RunID, FilePath: path, Operation: operation, Diff: diff, NewContent: updated, OriginalContent: original, Status: "pending"}
	if err := meta.DB.CreatePatch(ctx, patch); err != nil {
		return ToolResult{OK: false, Error: err.Error()}, nil
	}
	return ToolResult{OK: true, Content: map[string]any{"ok": true, "staged": true, "patch_id": patch.ID, "file_path": path, "operation": operation, "diff": diff}}, nil
}

type diffOp struct {
	kind byte
	text string
}

type linePair struct{ a, b int }

func splitTextLines(text string) ([]string, bool) {
	if text == "" {
		return nil, false
	}
	trailing := strings.HasSuffix(text, "\n")
	if trailing {
		text = strings.TrimSuffix(text, "\n")
	}
	return strings.Split(text, "\n"), trailing
}

func lcsRow(a, b []string) []int {
	previous := make([]int, len(b)+1)
	for _, av := range a {
		current := make([]int, len(b)+1)
		for j, bv := range b {
			if av == bv {
				current[j+1] = previous[j] + 1
			} else if current[j] > previous[j+1] {
				current[j+1] = current[j]
			} else {
				current[j+1] = previous[j+1]
			}
		}
		previous = current
	}
	return previous
}

func reversed(lines []string) []string {
	result := make([]string, len(lines))
	for i := range lines {
		result[len(lines)-1-i] = lines[i]
	}
	return result
}

func lcsPairs(a, b []string, aOffset, bOffset int) []linePair {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	if len(a) == 1 {
		for j, value := range b {
			if a[0] == value {
				return []linePair{{a: aOffset, b: bOffset + j}}
			}
		}
		return nil
	}
	mid := len(a) / 2
	left := lcsRow(a[:mid], b)
	right := lcsRow(reversed(a[mid:]), reversed(b))
	split := 0
	best := -1
	for j := 0; j <= len(b); j++ {
		if score := left[j] + right[len(b)-j]; score > best {
			best, split = score, j
		}
	}
	pairs := lcsPairs(a[:mid], b[:split], aOffset, bOffset)
	return append(pairs, lcsPairs(a[mid:], b[split:], aOffset+mid, bOffset+split)...)
}

func diffOperations(original, updated []string) []diffOp {
	matches := lcsPairs(original, updated, 0, 0)
	var operations []diffOp
	ai, bi := 0, 0
	for _, match := range matches {
		for ai < match.a {
			operations = append(operations, diffOp{kind: '-', text: original[ai]})
			ai++
		}
		for bi < match.b {
			operations = append(operations, diffOp{kind: '+', text: updated[bi]})
			bi++
		}
		operations = append(operations, diffOp{kind: ' ', text: original[ai]})
		ai++
		bi++
	}
	for ai < len(original) {
		operations = append(operations, diffOp{kind: '-', text: original[ai]})
		ai++
	}
	for bi < len(updated) {
		operations = append(operations, diffOp{kind: '+', text: updated[bi]})
		bi++
	}
	return operations
}

func computeUnifiedDiff(filename, original, updated string) string {
	if original == updated {
		return ""
	}
	oldLines, oldTrailing := splitTextLines(original)
	newLines, newTrailing := splitTextLines(updated)
	operations := diffOperations(oldLines, newLines)
	var changes []int
	for i, operation := range operations {
		if operation.kind != ' ' {
			changes = append(changes, i)
		}
	}
	type span struct{ start, end int }
	var spans []span
	for _, change := range changes {
		start, end := change-3, change+4
		if start < 0 {
			start = 0
		}
		if end > len(operations) {
			end = len(operations)
		}
		if len(spans) > 0 && start <= spans[len(spans)-1].end {
			if end > spans[len(spans)-1].end {
				spans[len(spans)-1].end = end
			}
		} else {
			spans = append(spans, span{start: start, end: end})
		}
	}
	clean := filepath.ToSlash(strings.TrimLeft(filename, `/\`))
	var out strings.Builder
	out.WriteString("--- a/" + clean + "\n+++ b/" + clean + "\n")
	oldBefore, newBefore := make([]int, len(operations)+1), make([]int, len(operations)+1)
	for i, operation := range operations {
		oldBefore[i+1], newBefore[i+1] = oldBefore[i], newBefore[i]
		if operation.kind != '+' {
			oldBefore[i+1]++
		}
		if operation.kind != '-' {
			newBefore[i+1]++
		}
	}
	for _, hunk := range spans {
		oldCount := oldBefore[hunk.end] - oldBefore[hunk.start]
		newCount := newBefore[hunk.end] - newBefore[hunk.start]
		oldStart, newStart := oldBefore[hunk.start]+1, newBefore[hunk.start]+1
		if oldCount == 0 {
			oldStart--
		}
		if newCount == 0 {
			newStart--
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)
		for index := hunk.start; index < hunk.end; index++ {
			operation := operations[index]
			out.WriteByte(operation.kind)
			out.WriteString(operation.text)
			out.WriteByte('\n')
			oldAtEnd := operation.kind != '+' && oldBefore[index+1] == len(oldLines)
			newAtEnd := operation.kind != '-' && newBefore[index+1] == len(newLines)
			if (oldAtEnd && !oldTrailing) || (newAtEnd && !newTrailing) {
				out.WriteString("\\ No newline at end of file\n")
			}
		}
	}
	return out.String()
}

var hunkHeaderPattern = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func applyUnifiedDiff(original, patch string) (string, error) {
	originalLines, originalTrailing := splitTextLines(original)
	patchLines := strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n")
	var output []string
	cursor := 0
	foundHunk := false
	newTrailing := originalTrailing
	lastPrefix := byte(0)
	for index := 0; index < len(patchLines); {
		line := patchLines[index]
		if !strings.HasPrefix(line, "@@ ") {
			index++
			continue
		}
		match := hunkHeaderPattern.FindStringSubmatch(line)
		if match == nil {
			return "", fmt.Errorf("invalid hunk header: %s", line)
		}
		foundHunk = true
		oldStart, _ := strconv.Atoi(match[1])
		oldCount := 1
		if match[2] != "" {
			oldCount, _ = strconv.Atoi(match[2])
		}
		newCount := 1
		if match[4] != "" {
			newCount, _ = strconv.Atoi(match[4])
		}
		target := oldStart - 1
		if oldCount == 0 {
			target = oldStart
		}
		if target < cursor || target > len(originalLines) {
			return "", fmt.Errorf("hunk starts outside the current file")
		}
		output = append(output, originalLines[cursor:target]...)
		cursor = target
		seenOld, seenNew := 0, 0
		index++
		for index < len(patchLines) && !strings.HasPrefix(patchLines[index], "@@ ") {
			body := patchLines[index]
			if body == "" && index == len(patchLines)-1 {
				index++
				break
			}
			if body == `\ No newline at end of file` {
				if lastPrefix == '+' || lastPrefix == ' ' {
					newTrailing = false
				}
				index++
				continue
			}
			if body == "---" || strings.HasPrefix(body, "--- ") || body == "+++" || strings.HasPrefix(body, "+++ ") {
				break
			}
			if len(body) == 0 || (body[0] != ' ' && body[0] != '-' && body[0] != '+') {
				return "", fmt.Errorf("invalid hunk line: %s", body)
			}
			prefix, text := body[0], body[1:]
			lastPrefix = prefix
			switch prefix {
			case ' ':
				if cursor >= len(originalLines) || originalLines[cursor] != text {
					return "", fmt.Errorf("context mismatch at source line %d", cursor+1)
				}
				output = append(output, text)
				cursor++
				seenOld++
				seenNew++
			case '-':
				if cursor >= len(originalLines) || originalLines[cursor] != text {
					return "", fmt.Errorf("deletion mismatch at source line %d", cursor+1)
				}
				cursor++
				seenOld++
			case '+':
				output = append(output, text)
				seenNew++
				newTrailing = true
			}
			index++
		}
		if seenOld != oldCount || seenNew != newCount {
			return "", fmt.Errorf("hunk count mismatch: expected -%d +%d, got -%d +%d", oldCount, newCount, seenOld, seenNew)
		}
	}
	if !foundHunk {
		return "", fmt.Errorf("no unified diff hunks found")
	}
	output = append(output, originalLines[cursor:]...)
	if len(output) == 0 {
		return "", nil
	}
	result := strings.Join(output, "\n")
	if newTrailing {
		result += "\n"
	}
	return result, nil
}
