// Package terminal manages persistent, PTY-backed shell sessions — one per
// project — so the integrated terminal behaves like a real terminal: cd
// persists, a long-running process like `npm run dev` keeps running across
// page refreshes, and Ctrl+C/colors/interactive prompts all work, because
// the shell is a real process with a real pseudo-terminal, not a one-shot
// exec per command (that's what agent/terminal.go's run_terminal tool is
// for instead — a separate, isolated, one-shot mechanism for the AI agent).
package terminal

import (
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

// scrollbackLimit bounds the recent-output buffer replayed to a new
// subscriber (a reconnect, a page refresh, re-showing a hidden panel) so it
// doesn't open onto a blank screen even though the shell itself is still
// alive and unchanged — without this, only output produced after the new
// connection attaches would ever be seen.
const scrollbackLimit = 64 << 10

// Session wraps one persistent shell process and its pseudo-terminal. Output
// is fanned out to any number of subscribers (normally just the one browser
// tab currently viewing it) via Subscribe/unsubscribe, so a reconnect (e.g.
// a page refresh) can re-attach to the same still-running shell instead of
// losing it.
type Session struct {
	mu          sync.Mutex
	pty         *os.File
	cmd         *exec.Cmd
	subscribers map[chan []byte]struct{}
	scrollback  []byte
	closed      bool
}

// shellCommand picks the user's own shell (matching what a real terminal on
// their machine would launch) rather than hardcoding one.
func shellCommand() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	return "/bin/bash"
}

// newSession starts a fresh shell process with a pty, rooted at workDir.
func newSession(workDir string) (*Session, error) {
	cmd := exec.Command(shellCommand())
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptyFile, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	s := &Session{
		pty:         ptyFile,
		cmd:         cmd,
		subscribers: make(map[chan []byte]struct{}),
	}
	go s.pump()
	return s, nil
}

// pump reads the pty's output continuously and fans it out to every current
// subscriber. Runs for the lifetime of the session, independent of any
// particular websocket connection.
func (s *Session) pump() {
	buf := make([]byte, 4096)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			s.mu.Lock()
			s.scrollback = append(s.scrollback, chunk...)
			if len(s.scrollback) > scrollbackLimit {
				s.scrollback = s.scrollback[len(s.scrollback)-scrollbackLimit:]
			}
			for ch := range s.subscribers {
				select {
				case ch <- chunk:
				default:
					// A slow/stuck subscriber shouldn't block the shell's
					// own output pump for everyone else.
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			s.mu.Lock()
			s.closed = true
			for ch := range s.subscribers {
				close(ch)
			}
			s.subscribers = map[chan []byte]struct{}{}
			s.mu.Unlock()
			return
		}
	}
}

// Subscribe registers a new output listener — pre-loaded with the recent
// scrollback so a reconnect doesn't open onto a blank screen — and returns
// it along with an unsubscribe function the caller must call when done
// (e.g. the websocket connection closed).
func (s *Session) Subscribe() (<-chan []byte, func()) {
	// Buffered generously enough to hold the scrollback replay (chunked back
	// into <=4096-byte pieces, matching pump's read size) plus a little
	// headroom for genuinely new output arriving in the same instant,
	// without the pump's non-blocking send (see the `default:` case above)
	// silently dropping the very backlog this exists to deliver.
	ch := make(chan []byte, 64)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	for offset := 0; offset < len(s.scrollback); offset += 4096 {
		end := offset + 4096
		if end > len(s.scrollback) {
			end = len(s.scrollback)
		}
		replay := make([]byte, end-offset)
		copy(replay, s.scrollback[offset:end])
		ch <- replay
	}
	s.subscribers[ch] = struct{}{}
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.subscribers[ch]; ok {
			delete(s.subscribers, ch)
			close(ch)
		}
	}
}

// Write sends input to the shell (keystrokes, pasted text, control bytes
// like Ctrl+C). Whichever connection last wrote "wins" — fine for a
// single-user local tool where at most one tab is realistically typing at
// once.
func (s *Session) Write(data []byte) error {
	_, err := s.pty.Write(data)
	return err
}

// Resize tells the pty (and therefore the shell and anything running in it)
// the new terminal dimensions, so full-screen programs (vim, htop, a dev
// server's TUI) render correctly.
func (s *Session) Resize(cols, rows int) error {
	return pty.Setsize(s.pty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// Close terminates the shell process and its pty. Used when a project is
// removed, not on an ordinary websocket disconnect (the whole point of a
// persistent session is surviving those).
func (s *Session) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	_ = s.pty.Close()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	return nil
}

// Manager holds one Session per project, created lazily on first use.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

func NewManager() *Manager {
	return &Manager{sessions: make(map[string]*Session)}
}

// GetOrCreate returns the existing session for a project, or starts one
// rooted at workDir if none exists yet (or the previous one's shell exited).
func (m *Manager) GetOrCreate(projectID, workDir string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.sessions[projectID]; ok {
		existing.mu.Lock()
		closed := existing.closed
		existing.mu.Unlock()
		if !closed {
			return existing, nil
		}
	}
	session, err := newSession(workDir)
	if err != nil {
		return nil, err
	}
	m.sessions[projectID] = session
	return session, nil
}

// Close terminates and forgets a project's session, if one exists.
func (m *Manager) Close(projectID string) {
	m.mu.Lock()
	session, ok := m.sessions[projectID]
	if ok {
		delete(m.sessions, projectID)
	}
	m.mu.Unlock()
	if ok {
		_ = session.Close()
	}
}
