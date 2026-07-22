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

type patchTestEnvironment struct {
	t       *testing.T
	db      *db.DB
	root    string
	mux     *http.ServeMux
	session string
}

func newPatchTestEnvironment(t *testing.T) *patchTestEnvironment {
	t.Helper()
	root := t.TempDir()
	database, err := db.Open(filepath.Join(t.TempDir(), "patch-handler.db"))
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
	NewPatchHandler(database, nil).RegisterRoutes(mux)
	environment := &patchTestEnvironment{t: t, db: database, root: root, mux: mux, session: "session"}
	t.Cleanup(func() {
		database.Close()
	})
	return environment
}

func (environment *patchTestEnvironment) addPatch(operation, path, original, updated string) db.Patch {
	environment.t.Helper()
	patch := db.Patch{
		ID: db.NewID(), SessionID: environment.session, RunID: "run", FilePath: path,
		Operation: operation, Diff: "diff", OriginalContent: original, NewContent: updated,
	}
	if err := environment.db.CreatePatch(context.Background(), patch); err != nil {
		environment.t.Fatal(err)
	}
	return patch
}

func (environment *patchTestEnvironment) post(path string, patchIDs ...string) (*http.Response, map[string]any) {
	environment.t.Helper()
	body, _ := json.Marshal(map[string]any{"patch_ids": patchIDs})
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

func TestPatchApproveAndRollbackModify(t *testing.T) {
	environment := newPatchTestEnvironment(t)
	target := filepath.Join(environment.root, "nested", "sample.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("before\n"), 0644); err != nil {
		t.Fatal(err)
	}
	patch := environment.addPatch("modify", "nested/sample.txt", "before\n", "after\n")

	response, payload := environment.post("/api/sessions/session/patches/approve", patch.ID)
	if response.StatusCode != http.StatusOK || len(payload["failed"].([]any)) != 0 {
		t.Fatalf("approve status=%d payload=%#v", response.StatusCode, payload)
	}
	content, _ := os.ReadFile(target)
	if string(content) != "after\n" {
		t.Fatalf("approved content = %q", content)
	}
	stored, _ := environment.db.GetPatch(context.Background(), patch.ID)
	if stored.Status != "applied" || stored.AppliedAt == nil {
		t.Fatalf("patch not marked applied: %#v", stored)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/patches/"+patch.ID+"/rollback", nil)
	recorder := httptest.NewRecorder()
	environment.mux.ServeHTTP(recorder, request)
	rollback := recorder.Result()
	rollback.Body.Close()
	if rollback.StatusCode != http.StatusOK {
		t.Fatalf("rollback status=%d", rollback.StatusCode)
	}
	content, _ = os.ReadFile(target)
	if string(content) != "before\n" {
		t.Fatalf("rolled back content = %q", content)
	}
	stored, _ = environment.db.GetPatch(context.Background(), patch.ID)
	if stored.Status != "rolled_back" {
		t.Fatalf("patch status after rollback = %q", stored.Status)
	}
}

func TestPatchRejectLeavesDiskUnchanged(t *testing.T) {
	environment := newPatchTestEnvironment(t)
	patch := environment.addPatch("create", "new.txt", "", "new content\n")
	response, _ := environment.post("/api/sessions/session/patches/reject", patch.ID)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("reject status=%d", response.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(environment.root, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("rejected create changed disk: %v", err)
	}
	stored, _ := environment.db.GetPatch(context.Background(), patch.ID)
	if stored.Status != "rejected" {
		t.Fatalf("patch status = %q", stored.Status)
	}
}

func TestPatchApproveAndRollbackCreateAndDelete(t *testing.T) {
	environment := newPatchTestEnvironment(t)
	deletedTarget := filepath.Join(environment.root, "old.txt")
	if err := os.WriteFile(deletedTarget, []byte("old content\n"), 0644); err != nil {
		t.Fatal(err)
	}
	created := environment.addPatch("create", "nested/new.txt", "", "new content\n")
	deleted := environment.addPatch("delete", "old.txt", "old content\n", "")

	response, payload := environment.post("/api/sessions/session/patches/approve", created.ID, deleted.ID)
	if response.StatusCode != http.StatusOK || len(payload["failed"].([]any)) != 0 {
		t.Fatalf("batch approve status=%d payload=%#v", response.StatusCode, payload)
	}
	if content, err := os.ReadFile(filepath.Join(environment.root, "nested", "new.txt")); err != nil || string(content) != "new content\n" {
		t.Fatalf("created file content=%q err=%v", content, err)
	}
	if _, err := os.Stat(deletedTarget); !os.IsNotExist(err) {
		t.Fatalf("delete was not applied: %v", err)
	}

	for _, patchID := range []string{created.ID, deleted.ID} {
		request := httptest.NewRequest(http.MethodPost, "/api/patches/"+patchID+"/rollback", nil)
		recorder := httptest.NewRecorder()
		environment.mux.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("rollback %s status=%d body=%s", patchID, recorder.Code, recorder.Body.String())
		}
	}
	if _, err := os.Stat(filepath.Join(environment.root, "nested", "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("created file remained after rollback: %v", err)
	}
	if content, err := os.ReadFile(deletedTarget); err != nil || string(content) != "old content\n" {
		t.Fatalf("deleted file was not restored: content=%q err=%v", content, err)
	}
}

func TestPatchApproveProtectsAgainstStaleContent(t *testing.T) {
	environment := newPatchTestEnvironment(t)
	target := filepath.Join(environment.root, "sample.txt")
	if err := os.WriteFile(target, []byte("changed elsewhere\n"), 0644); err != nil {
		t.Fatal(err)
	}
	patch := environment.addPatch("modify", "sample.txt", "before\n", "after\n")
	response, payload := environment.post("/api/sessions/session/patches/approve", patch.ID)
	if response.StatusCode != http.StatusOK || len(payload["failed"].([]any)) != 1 {
		t.Fatalf("stale approve status=%d payload=%#v", response.StatusCode, payload)
	}
	content, _ := os.ReadFile(target)
	if string(content) != "changed elsewhere\n" {
		t.Fatalf("stale patch overwrote content: %q", content)
	}
	stored, _ := environment.db.GetPatch(context.Background(), patch.ID)
	if stored.Status != "pending" {
		t.Fatalf("failed patch status = %q", stored.Status)
	}
}

func TestPatchListFiltersStatus(t *testing.T) {
	environment := newPatchTestEnvironment(t)
	pending := environment.addPatch("create", "pending.txt", "", "pending")
	rejected := environment.addPatch("create", "rejected.txt", "", "rejected")
	environment.post("/api/sessions/session/patches/reject", rejected.ID)
	request := httptest.NewRequest(http.MethodGet, "/api/sessions/session/patches?status=pending", nil)
	recorder := httptest.NewRecorder()
	environment.mux.ServeHTTP(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()
	var payload struct {
		Patches []db.Patch `json:"patches"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Patches) != 1 || payload.Patches[0].ID != pending.ID {
		t.Fatalf("pending list = %#v", payload.Patches)
	}
}
