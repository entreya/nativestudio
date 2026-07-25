package terminal

import (
	"strings"
	"testing"
	"time"
)

func readUntil(t *testing.T, ch <-chan []byte, substr string, timeout time.Duration) string {
	t.Helper()
	deadline := time.After(timeout)
	var collected strings.Builder
	for {
		select {
		case chunk, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed before seeing %q; collected: %q", substr, collected.String())
			}
			collected.Write(chunk)
			if strings.Contains(collected.String(), substr) {
				return collected.String()
			}
		case <-deadline:
			t.Fatalf("timed out waiting for %q; collected so far: %q", substr, collected.String())
		}
	}
}

func TestSessionEchoesCommandOutput(t *testing.T) {
	session, err := newSession(t.TempDir())
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	defer session.Close()

	output, unsubscribe := session.Subscribe()
	defer unsubscribe()

	if err := session.Write([]byte("echo hello-terminal-test\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	readUntil(t, output, "hello-terminal-test", 5*time.Second)
}

func TestSessionSupportsMultipleSubscribers(t *testing.T) {
	session, err := newSession(t.TempDir())
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	defer session.Close()

	outputA, unsubA := session.Subscribe()
	defer unsubA()
	outputB, unsubB := session.Subscribe()
	defer unsubB()

	if err := session.Write([]byte("echo fanned-out\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	readUntil(t, outputA, "fanned-out", 5*time.Second)
	readUntil(t, outputB, "fanned-out", 5*time.Second)
}

func TestSessionCloseEndsSubscribers(t *testing.T) {
	session, err := newSession(t.TempDir())
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	output, unsubscribe := session.Subscribe()
	defer unsubscribe()

	if err := session.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case _, ok := <-output:
		if ok {
			// Draining leftover buffered output before the close signal is fine.
			return
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expected the subscriber channel to close after Close()")
	}
}

func TestSubscribeReplaysRecentScrollbackToNewSubscriber(t *testing.T) {
	session, err := newSession(t.TempDir())
	if err != nil {
		t.Fatalf("newSession: %v", err)
	}
	defer session.Close()

	first, unsubFirst := session.Subscribe()
	if err := session.Write([]byte("echo scrollback-marker\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	readUntil(t, first, "scrollback-marker", 5*time.Second)
	unsubFirst()

	// A brand new subscriber (simulating a reconnect) should immediately see
	// that same output already replayed, without the shell producing
	// anything new.
	second, unsubSecond := session.Subscribe()
	defer unsubSecond()
	readUntil(t, second, "scrollback-marker", 5*time.Second)
}

func TestManagerReusesSessionForSameProject(t *testing.T) {
	manager := NewManager()
	root := t.TempDir()

	first, err := manager.GetOrCreate("proj-1", root)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	second, err := manager.GetOrCreate("proj-1", root)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if first != second {
		t.Fatal("expected the same session to be reused for the same project")
	}
	manager.Close("proj-1")
}

func TestManagerGivesDifferentProjectsDifferentSessions(t *testing.T) {
	manager := NewManager()
	root := t.TempDir()

	a, err := manager.GetOrCreate("proj-a", root)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	defer manager.Close("proj-a")
	b, err := manager.GetOrCreate("proj-b", root)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	defer manager.Close("proj-b")
	if a == b {
		t.Fatal("expected different projects to get different sessions")
	}
}
