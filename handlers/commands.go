package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"time"

	"github.com/entreya/nativestudio/db"
	"github.com/entreya/nativestudio/indexer"
	workspacefs "github.com/entreya/nativestudio/workspace"
)

// commandTimeout bounds a single approved command (package installs can
// legitimately take a while; this still needs a ceiling so an approved
// command can't hang the approval request forever).
const commandTimeout = 5 * time.Minute

// commandOutputLimit caps how much combined stdout/stderr is kept — enough
// to show a real install log without unbounded growth.
const commandOutputLimit = 200 << 10

type CommandHandler struct {
	db          *db.DB
	coordinator *indexer.Coordinator
}

func NewCommandHandler(database *db.DB, coordinator *indexer.Coordinator) *CommandHandler {
	return &CommandHandler{db: database, coordinator: coordinator}
}

func (h *CommandHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/sessions/{sessionId}/commands", h.List)
	mux.HandleFunc("POST /api/sessions/{sessionId}/commands/approve", h.Approve)
	mux.HandleFunc("POST /api/sessions/{sessionId}/commands/reject", h.Reject)
}

type commandRunIDsRequest struct {
	RunIDs []string `json:"run_ids"`
}

func (h *CommandHandler) List(w http.ResponseWriter, r *http.Request) {
	if _, err := h.db.GetSession(r.PathValue("sessionId")); err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	runs, err := h.db.ListCommandRuns(r.Context(), r.PathValue("sessionId"), r.URL.Query().Get("status"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if runs == nil {
		runs = []db.CommandRun{}
	}
	writeJSON(w, map[string]any{"commands": runs})
}

// Approve actually executes the command — this is the one and only place a
// staged run_command call becomes a real process, and only after a human
// explicitly asked for exactly this command to run.
func (h *CommandHandler) Approve(w http.ResponseWriter, r *http.Request) {
	var request commandRunIDsRequest
	if json.NewDecoder(r.Body).Decode(&request) != nil || len(request.RunIDs) == 0 {
		http.Error(w, "run_ids are required", http.StatusBadRequest)
		return
	}
	sessionID := r.PathValue("sessionId")
	completed := make([]map[string]any, 0, len(request.RunIDs))
	failed := make([]map[string]string, 0)

	for _, id := range request.RunIDs {
		run, projectRoot, err := h.resolveCommand(r.Context(), sessionID, id)
		if err == nil && run.Status != "pending" {
			err = fmt.Errorf("command is no longer pending")
		}
		if err != nil {
			failed = append(failed, map[string]string{"run_id": id, "error": err.Error()})
			continue
		}
		if claimed, claimErr := h.db.SetCommandRunStatus(r.Context(), id, "pending", "running"); claimErr != nil || !claimed {
			failed = append(failed, map[string]string{"run_id": id, "error": "command is no longer pending"})
			continue
		}

		workDir, dirErr := (workspacefs.Guard{Root: projectRoot}).ResolveDirectory(projectRoot, run.Cwd)
		if dirErr != nil {
			_ = h.db.FinishCommandRun(r.Context(), id, "failed", dirErr.Error(), -1)
			failed = append(failed, map[string]string{"run_id": id, "error": dirErr.Error()})
			continue
		}

		output, exitCode, runErr := runShellCommand(r.Context(), run.Command, workDir)
		status := "completed"
		if runErr != nil || exitCode != 0 {
			status = "failed"
		}
		if err := h.db.FinishCommandRun(r.Context(), id, status, output, exitCode); err != nil {
			failed = append(failed, map[string]string{"run_id": id, "error": err.Error()})
			continue
		}
		completed = append(completed, map[string]any{"run_id": id, "status": status, "output": output, "exit_code": exitCode})

		if h.coordinator != nil && status == "completed" {
			if session, sessErr := h.db.GetSession(sessionID); sessErr == nil {
				// A package install/scaffold command can add hundreds of
				// files at once — force a fresh scan rather than waiting on
				// the file watcher to notice them one at a time.
				go h.coordinator.Restart(session.ProjectID, projectRoot)
			}
		}
	}
	writeJSON(w, map[string]any{"completed": completed, "failed": failed})
}

func (h *CommandHandler) Reject(w http.ResponseWriter, r *http.Request) {
	var request commandRunIDsRequest
	if json.NewDecoder(r.Body).Decode(&request) != nil || len(request.RunIDs) == 0 {
		http.Error(w, "run_ids are required", http.StatusBadRequest)
		return
	}
	sessionID := r.PathValue("sessionId")
	rejected := []string{}
	failed := []map[string]string{}
	for _, id := range request.RunIDs {
		run, err := h.db.GetCommandRun(r.Context(), id)
		if err != nil || run.SessionID != sessionID {
			failed = append(failed, map[string]string{"run_id": id, "error": "command not found"})
			continue
		}
		updated, err := h.db.SetCommandRunStatus(r.Context(), id, "pending", "rejected")
		if err != nil || !updated {
			message := "command is no longer pending"
			if err != nil {
				message = err.Error()
			}
			failed = append(failed, map[string]string{"run_id": id, "error": message})
			continue
		}
		rejected = append(rejected, id)
	}
	writeJSON(w, map[string]any{"ok": len(failed) == 0, "rejected": rejected, "failed": failed})
}

func (h *CommandHandler) resolveCommand(ctx context.Context, sessionID, runID string) (db.CommandRun, string, error) {
	run, err := h.db.GetCommandRun(ctx, runID)
	if err != nil || run.SessionID != sessionID {
		return run, "", fmt.Errorf("command not found")
	}
	session, err := h.db.GetSession(run.SessionID)
	if err != nil {
		return run, "", fmt.Errorf("session not found")
	}
	project, err := h.db.GetProject(session.ProjectID)
	if err != nil {
		return run, "", fmt.Errorf("workspace not found")
	}
	return run, project.Path, nil
}

// runShellCommand is the only place this application ever executes a shell
// command. It is only ever reached via CommandHandler.Approve, i.e. after a
// human has read the exact command text and explicitly approved it — the
// workspace-contained cwd is a courtesy, not a sandbox; the command itself
// can do anything a shell command run by this user could do, same as typing
// it into their own terminal.
func runShellCommand(ctx context.Context, command, workDir string) (string, int, error) {
	runCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "sh", "-c", command)
	cmd.Dir = workDir
	var buf bytes.Buffer
	cmd.Stdout = &limitedWriter{buf: &buf, limit: commandOutputLimit}
	cmd.Stderr = cmd.Stdout

	err := cmd.Run()
	output := buf.String()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
			if runCtx.Err() == context.DeadlineExceeded {
				output += fmt.Sprintf("\n[timed out after %s]", commandTimeout)
			}
		}
	}
	return output, exitCode, err
}

// limitedWriter caps how much of a command's output is retained in memory
// and in the database, without failing the command itself if it's chatty.
type limitedWriter struct {
	buf   *bytes.Buffer
	limit int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
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
