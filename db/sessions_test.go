package db

import (
	"path/filepath"
	"testing"
)

func TestListConversationOverviews(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "overview.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	project, err := database.CreateProject("project-1", "Stale label", "/work/actual-folder", "")
	if err != nil {
		t.Fatal(err)
	}
	session, err := database.CreateSession("session-1", project.ID, "qwen2.5-coder:1.5b", "Refactor API")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.AppendMessage("message-1", session.ID, "user", "Please refactor this", false, 12); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`
		INSERT INTO checkpoints (id, session_id, summary, tokens_before, tokens_after, message_seq_start, message_seq_end)
		VALUES ('checkpoint-1', ?, 'Kept the API decision', 100, 20, 1, 1)
	`, session.ID); err != nil {
		t.Fatal(err)
	}

	items, err := database.ListConversationOverviews()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one conversation, got %d", len(items))
	}
	got := items[0]
	if got.ProjectPath != "/work/actual-folder" || got.MessageCount != 1 || got.TokensUsed != 12 || got.CheckpointCount != 1 || got.LatestMemory != "Kept the API decision" {
		t.Fatalf("unexpected overview: %#v", got)
	}

	if err := database.UpdateMessage("message-1", "Updated prompt", 4); err != nil {
		t.Fatal(err)
	}
	messages, err := database.GetMessages(session.ID)
	if err != nil || len(messages) != 1 || messages[0].Content != "Updated prompt" || messages[0].TokenEstimate != 4 {
		t.Fatalf("message edit was not persisted: %#v, %v", messages, err)
	}
	if err := database.UpdateSessionMetadata(session.ID, "API Refactor Plan", "The user is refactoring the API."); err != nil {
		t.Fatal(err)
	}
	updated, err := database.GetSession(session.ID)
	if err != nil || updated.Title != "API Refactor Plan" || updated.Summary != "The user is refactoring the API." {
		t.Fatalf("session metadata was not persisted: %#v, %v", updated, err)
	}
}

func TestPlaceholderConversationIsBackfilledOnReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backfill.db")
	database, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	project, err := database.CreateProject("project-backfill", "Project", "/work/project", "")
	if err != nil {
		t.Fatal(err)
	}
	session, err := database.CreateSession("session-backfill", project.ID, "model", "New Conversation")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.AppendMessage("message-backfill", session.ID, "user", "Explain this file", false, 4); err != nil {
		t.Fatal(err)
	}
	database.Close()

	database, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	updated, err := database.GetSession(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Explain this file" || updated.Summary == "" {
		t.Fatalf("placeholder was not backfilled: %#v", updated)
	}
}
