package agent

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/entreya/nativestudio/db"
	workspacefs "github.com/entreya/nativestudio/workspace"
)

// terminalTimeout bounds a run_terminal call. Unlike run_command (staged,
// human-approved, and fine taking a while for a real install), run_terminal
// executes immediately with no review step at all, so it's scoped to
// genuinely quick, basic operations — a short ceiling turns a stuck command
// into a clear error instead of a long silent hang.
const terminalTimeout = 20 * time.Second

// terminalOutputLimit caps how much combined stdout/stderr comes back in a
// single tool result.
const terminalOutputLimit = 32 << 10

// destructiveCommandPatterns blocks command shapes that would be a real
// safety regression if they ran with no human review at all — these must
// go through run_command instead, where a person reads the exact command
// before it executes. This is a denylist, not a sandbox: a command that
// doesn't match one of these can still do anything a shell command run by
// this user could do, the same trust model run_command already documents.
var destructiveCommandPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\brm\s+(?:\S+\s+)*-[a-z]*r[a-z]*\b`), // rm with any recursive flag (-r, -rf, -fr, -R...)
	regexp.MustCompile(`(?i)\brm\s+(?:\S+\s+)*--recursive\b`),
	regexp.MustCompile(`(?i)\bsudo\b`),
	regexp.MustCompile(`(?i)\bmkfs\b`),
	regexp.MustCompile(`(?i)\bdd\s+if=`),
	regexp.MustCompile(`(?i)>\s*/dev/(sd|nvme|disk|hd)`),
	regexp.MustCompile(`(?i)\bchmod\s+(?:-r\s+)?777\b`),
	regexp.MustCompile(`(?i)\bchown\s+-r\b`),
	regexp.MustCompile(`(?i)\bshutdown\b`),
	regexp.MustCompile(`(?i)\breboot\b`),
	regexp.MustCompile(`(?i)\bkill\s+-9\s+1\b`),
	regexp.MustCompile(`(?i)\bgit\s+push\b.*--force`),
	regexp.MustCompile(`(?i)\bgit\s+reset\s+--hard\b`),
	regexp.MustCompile(`(?i)\|\s*(?:sudo\s+)?(?:sh|bash)\b`),
	regexp.MustCompile(`:\(\)\s*\{`), // fork bomb
}

func isDestructiveCommand(command string) bool {
	for _, pattern := range destructiveCommandPatterns {
		if pattern.MatchString(command) {
			return true
		}
	}
	return false
}

// registerTerminalTool adds run_terminal — an instant-execution counterpart
// to run_command for basic, reversible one-off tasks (checking a file's
// size, a curl request, running a short script, a rename) that don't need a
// human to review them first. Anything matching destructiveCommandPatterns
// is not refused outright — it's staged through the same approval popup as
// run_command, so the user still gets a confirmation prompt before anything
// destructive actually runs, but the model doesn't need to pick the right
// tool up front to get there.
func registerTerminalTool(r *Registry) {
	r.Register(&Tool{
		Name: "run_terminal",
		Description: "Immediately run a basic shell command in the workspace and return its output — for quick, non-destructive one-off tasks " +
			"like checking a file's size, making a curl request, running a short script you just wrote, renaming/moving a file, or generating " +
			"sample data. Executes right away with no approval step for basic/reversible operations. A command that looks destructive or " +
			"irreversible (recursive delete, force-push, piping a download into a shell, etc.) is instead staged for the user to confirm in a " +
			"popup, same as run_command, rather than run instantly or refused outright.",
		Parameters: objectParameters(map[string]any{
			"command": map[string]any{"type": "string", "description": "The exact shell command to run."},
			"cwd":     map[string]any{"type": "string", "description": "Directory to run it in, relative to the workspace root. Defaults to the workspace root."},
		}, "command"),
		Safety:  Safe,
		Execute: executeRunTerminal,
	})
}

func executeRunTerminal(ctx context.Context, input ToolInput, meta ToolMeta) (ToolResult, error) {
	command, _ := input["command"].(string)
	command = strings.TrimSpace(command)
	if command == "" {
		return ToolResult{OK: false, Error: "command is required"}, nil
	}
	cwdInput, _ := input["cwd"].(string)
	if isDestructiveCommand(command) {
		return stageCommandForApproval(ctx, meta, command, cwdInput)
	}

	if cwdInput == "" {
		cwdInput = "."
	}
	guard := workspacefs.Guard{}
	resolvedCwd, err := guard.ResolveDirectory(meta.WorkspaceRoot, cwdInput)
	if err != nil {
		return ToolResult{OK: false, Error: err.Error()}, nil
	}

	before, truncated := snapshotTerminalFiles(resolvedCwd, meta.WorkspaceRoot)

	runCtx, cancel := context.WithTimeout(ctx, terminalTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "sh", "-c", command)
	cmd.Dir = resolvedCwd
	var buf bytes.Buffer
	cmd.Stdout = &limitedTerminalWriter{buf: &buf, limit: terminalOutputLimit}
	cmd.Stderr = cmd.Stdout

	runErr := cmd.Run()
	output := buf.String()
	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
			if runCtx.Err() == context.DeadlineExceeded {
				output += fmt.Sprintf("\n[timed out after %s]", terminalTimeout)
			}
		}
	}

	content := map[string]any{"command": command, "output": output, "exit_code": exitCode}
	if !truncated {
		after, afterTruncated := snapshotTerminalFiles(resolvedCwd, meta.WorkspaceRoot)
		if !afterTruncated {
			if changes := detectTerminalFileChanges(ctx, meta, before, after); len(changes) > 0 {
				content["file_changes"] = changes
			}
		}
	}

	return ToolResult{OK: true, Content: content}, nil
}

// terminalSnapshotMaxFiles/terminalSnapshotMaxFileSize/terminalSnapshotMaxTotalBytes
// bound how much a before/after snapshot reads so a quick command on a huge
// directory can't turn into a slow, memory-heavy scan — if the walk hits any
// limit, the snapshot is marked truncated and file-change detection is
// skipped entirely for that call (the command's real output is unaffected;
// only the "show me what changed" review is skipped on an oversized tree).
const (
	terminalSnapshotMaxFiles      = 400
	terminalSnapshotMaxFileSize   = 512 << 10
	terminalSnapshotMaxTotalBytes = 20 << 20
)

var terminalSnapshotSkipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "__pycache__": true, ".venv": true, "venv": true,
}

// snapshotTerminalFiles walks walkRoot and returns relative-to-relBase path
// -> content for every plain-text file found, so a run_terminal call can
// diff before vs. after and surface exactly what changed for review — the
// same way create_file/apply_patch already stage a reviewable diff, just
// after the fact since the command already ran. Returns truncated=true if
// any bound was hit, in which case the caller should skip diffing rather
// than risk a false create/delete from an incomplete walk.
func snapshotTerminalFiles(walkRoot, relBase string) (map[string]string, bool) {
	snapshot := make(map[string]string)
	totalBytes := 0
	truncated := false
	_ = filepath.WalkDir(walkRoot, func(path string, entry fs.DirEntry, err error) error {
		if truncated {
			return filepath.SkipAll
		}
		if err != nil {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if path != walkRoot && (terminalSnapshotSkipDirs[name] || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}
		info, statErr := entry.Info()
		if statErr != nil || info.Size() > terminalSnapshotMaxFileSize {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil || looksBinary(data) {
			return nil
		}
		rel, relErr := filepath.Rel(relBase, path)
		if relErr != nil {
			return nil
		}
		snapshot[filepath.ToSlash(rel)] = string(data)
		totalBytes += len(data)
		if len(snapshot) > terminalSnapshotMaxFiles || totalBytes > terminalSnapshotMaxTotalBytes {
			truncated = true
			return filepath.SkipAll
		}
		return nil
	})
	return snapshot, truncated
}

func looksBinary(data []byte) bool {
	sample := data
	if len(sample) > 8000 {
		sample = sample[:8000]
	}
	for _, b := range sample {
		if b == 0 {
			return true
		}
	}
	return false
}

// detectTerminalFileChanges diffs a before/after snapshot and, for every
// file that was created, modified, or deleted, stages an already-applied
// Patch (db.CreateAppliedPatch) so it shows up for review exactly like a
// create_file/apply_patch change — except reject-equivalent here means
// rolling back a change that's already on disk, via the existing
// PatchHandler.Rollback endpoint, rather than never having written it.
func detectTerminalFileChanges(ctx context.Context, meta ToolMeta, before, after map[string]string) []map[string]any {
	var changes []map[string]any
	for relPath, afterContent := range after {
		beforeContent, existed := before[relPath]
		if existed && beforeContent == afterContent {
			continue
		}
		operation := "modify"
		originalContent := beforeContent
		if !existed {
			operation = "create"
			originalContent = ""
		}
		changes = append(changes, stageAppliedTerminalPatch(ctx, meta, relPath, operation, originalContent, afterContent))
	}
	for relPath, beforeContent := range before {
		if _, stillExists := after[relPath]; stillExists {
			continue
		}
		changes = append(changes, stageAppliedTerminalPatch(ctx, meta, relPath, "delete", beforeContent, ""))
	}
	return changes
}

func stageAppliedTerminalPatch(ctx context.Context, meta ToolMeta, relPath, operation, original, updated string) map[string]any {
	diff := computeUnifiedDiff(relPath, original, updated)
	patch := db.Patch{
		ID: db.NewID(), SessionID: meta.SessionID, RunID: meta.RunID,
		FilePath: relPath, Operation: operation, Diff: diff,
		NewContent: updated, OriginalContent: original,
	}
	if meta.DB != nil && meta.SessionID != "" && meta.RunID != "" {
		_ = meta.DB.CreateAppliedPatch(ctx, patch)
	}
	return map[string]any{
		"patch_id": patch.ID, "file_path": relPath, "operation": operation, "diff": diff,
	}
}

// limitedTerminalWriter caps how much of a command's output is retained,
// without failing the command itself if it's chatty.
type limitedTerminalWriter struct {
	buf   *bytes.Buffer
	limit int
}

func (w *limitedTerminalWriter) Write(p []byte) (int, error) {
	remaining := w.limit - w.buf.Len()
	if remaining <= 0 {
		return len(p), nil
	}
	if len(p) > remaining {
		w.buf.Write(p[:remaining])
		w.buf.WriteString("\n[output truncated]")
		return len(p), nil
	}
	return w.buf.Write(p)
}
