package context

import (
	"sync"
	"time"
)

type Message struct {
	Role         string    `json:"role"`
	Content      string    `json:"content"`
	Timestamp    time.Time `json:"timestamp"`
	IsCheckpoint bool      `json:"is_checkpoint"`
}

type Checkpoint struct {
	CreatedAt    time.Time `json:"created_at"`
	TokensBefore int       `json:"tokens_before"`
	TokensAfter  int       `json:"tokens_after"`
	Summary      string    `json:"summary"`
}

type Session struct {
	ID          string       `json:"id"`
	Messages    []Message    `json:"messages"`
	Checkpoints []Checkpoint `json:"checkpoints"`
	ActiveFile  string       `json:"active_file"`
	Model       string       `json:"model"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*Session),
	}
}

var Store = NewSessionStore()

func (s *SessionStore) GetOrCreate(sessionID string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	if session, exists := s.sessions[sessionID]; exists {
		return session
	}

	session := &Session{
		ID:          sessionID,
		Messages:    make([]Message, 0),
		Checkpoints: make([]Checkpoint, 0),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	s.sessions[sessionID] = session
	return session
}

func (s *SessionStore) AppendMessage(sessionID string, msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if session, exists := s.sessions[sessionID]; exists {
		if msg.Timestamp.IsZero() {
			msg.Timestamp = time.Now()
		}
		session.Messages = append(session.Messages, msg)
		session.UpdatedAt = time.Now()
	}
}

func (s *SessionStore) GetMessages(sessionID string) []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if session, exists := s.sessions[sessionID]; exists {
		// Return a copy to avoid data races
		messages := make([]Message, len(session.Messages))
		copy(messages, session.Messages)
		return messages
	}
	return nil
}

func (s *SessionStore) GetTokenUsage(sessionID, model string) (used int, total int, pct float64) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total = GetModelContextWindow(model)

	if session, exists := s.sessions[sessionID]; exists {
		used = EstimateMessagesTokens(session.Messages)
		pct = float64(used) / float64(total)
		return used, total, pct
	}

	return 0, total, 0.0
}

func (s *SessionStore) Reset(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}

func (s *SessionStore) UpdateMessagesAndCheckpoints(sessionID string, messages []Message, checkpoint Checkpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if session, exists := s.sessions[sessionID]; exists {
		session.Messages = messages
		session.Checkpoints = append(session.Checkpoints, checkpoint)
		session.UpdatedAt = time.Now()
	}
}
