package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/entreya/nativestudio/db"
)

type commandTestEnvironment struct {
	t       *testing.T
	db      *db.DB
	root    string
	mux     *http.ServeMux
	session string
}

func newCommandTestEnvironment(t *testing.T) *commandTestEnvironment {
	t.Helper()
	root := t.TempDir()
	database, err := db.Open(filepath.Join(t.TempDir(), "command-handler.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateProject("project", "Project", root, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateSession("session", "project", "model", "Test"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	NewCommandHandler(database, nil).RegisterRoutes(mux)
	environment := &commandTestEnvironment{t: t, db: database, root: root, mux: mux, session: "session"}
	t.Cleanup(func() { database.Close() })
	return environment
}

func (environment *commandTestEnvironment) stage(command, cwd string) db.CommandRun {
	environment.t.Helper()
	run := db.CommandRun{ID: db.NewID(), SessionID: environment.session, AgentRunID: "run", Command: command, Cwd: cwd}
	if err := environment.db.CreateCommandRun(context.Background(), run); err != nil {
		environment.t.Fatal(err)
	}
	return run
}

func (environment *commandTestEnvironment) post(path string, runIDs ...string) (*http.Response, map[string]any) {
	environment.t.Helper()
	body, _ := json.Marshal(map[string]any{"run_ids": runIDs})
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	environment.mux.ServeHTTP(recorder, request)
	response := recorder.Result()
	var payload map[string]any
	_ = json.NewDecoder(response.Body).Decode(&payload)
	response.Body.Close()
	return response, payload
}

func TestCommandApproveRunsAndRecordsOutput(t *testing.T) {
	environment := newCommandTestEnvironment(t)
	run := environment.stage("echo hello-from-command", ".")

	response, payload := environment.post("/api/sessions/session/commands/approve", run.ID)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d payload=%#v", response.StatusCode, payload)
	}
	failed, _ := payload["failed"].([]any)
	if len(failed) != 0 {
		t.Fatalf("expected no failures, got %#v", payload)
	}
	completed, _ := payload["completed"].([]any)
	if len(completed) != 1 {
		t.Fatalf("expected one completed command, got %#v", payload)
	}
	entry := completed[0].(map[string]any)
	if entry["status"] != "completed" {
		t.Fatalf("expected status completed, got %#v", entry)
	}
	if output, _ := entry["output"].(string); output == "" || !bytes.Contains([]byte(output), []byte("hello-from-command")) {
		t.Fatalf("expected captured output to contain the echoed text, got %q", output)
	}

	stored, err := environment.db.GetCommandRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "completed" || stored.FinishedAt == nil || stored.StartedAt == nil {
		t.Fatalf("expected the run to be recorded as completed with timestamps, got %#v", stored)
	}
}

func TestCommandApproveRecordsNonZeroExit(t *testing.T) {
	environment := newCommandTestEnvironment(t)
	run := environment.stage("exit 3", ".")

	_, payload := environment.post("/api/sessions/session/commands/approve", run.ID)
	completed := payload["completed"].([]any)[0].(map[string]any)
	if completed["status"] != "failed" {
		t.Fatalf("expected a non-zero exit to be recorded as failed, got %#v", completed)
	}
	if code, ok := completed["exit_code"].(float64); !ok || int(code) != 3 {
		t.Fatalf("expected exit_code 3, got %#v", completed["exit_code"])
	}
}

func TestCommandApproveCannotRunTwice(t *testing.T) {
	environment := newCommandTestEnvironment(t)
	run := environment.stage("echo once", ".")

	environment.post("/api/sessions/session/commands/approve", run.ID)
	_, payload := environment.post("/api/sessions/session/commands/approve", run.ID)
	failed, _ := payload["failed"].([]any)
	if len(failed) != 1 {
		t.Fatalf("expected the second approval of an already-run command to fail, got %#v", payload)
	}
}

func TestCommandRejectNeverExecutes(t *testing.T) {
	environment := newCommandTestEnvironment(t)
	run := environment.stage("echo should-not-run > "+filepath.Join(environment.root, "marker.txt"), ".")

	response, payload := environment.post("/api/sessions/session/commands/reject", run.ID)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d payload=%#v", response.StatusCode, payload)
	}
	stored, err := environment.db.GetCommandRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "rejected" {
		t.Fatalf("expected status rejected, got %q", stored.Status)
	}
	if _, statErr := os.Stat(filepath.Join(environment.root, "marker.txt")); statErr == nil {
		t.Fatal("rejected command must never actually execute")
	}
}

func TestCommandCannotEscapeWorkspaceViaCwd(t *testing.T) {
	environment := newCommandTestEnvironment(t)
	run := environment.stage("pwd", "../../etc")

	_, payload := environment.post("/api/sessions/session/commands/approve", run.ID)
	failed, _ := payload["failed"].([]any)
	if len(failed) != 1 {
		t.Fatalf("expected a cwd escape attempt to fail, got %#v", payload)
	}
}

func TestCommandListFiltersByStatus(t *testing.T) {
	environment := newCommandTestEnvironment(t)
	environment.stage("echo a", ".")

	request := httptest.NewRequest(http.MethodGet, "/api/sessions/session/commands?status=pending", nil)
	recorder := httptest.NewRecorder()
	environment.mux.ServeHTTP(recorder, request)
	var payload map[string]any
	json.NewDecoder(recorder.Body).Decode(&payload)
	commands, _ := payload["commands"].([]any)
	if len(commands) != 1 {
		t.Fatalf("expected 1 pending command, got %#v", payload)
	}
}
