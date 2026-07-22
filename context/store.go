package context

import (
	"crypto/rand"
	"fmt"
	"log"
	"time"

	"github.com/entreya/nativestudio/db"
)

type Message struct {
	Role         string    `json:"role"`
	Content      string    `json:"content"`
	Timestamp    time.Time `json:"timestamp"`
	IsCheckpoint bool      `json:"is_checkpoint"`
	Sequence     int       `json:"-"`
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

func newID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

type DBStore struct {
	database *db.DB
}

func NewDBStore(database *db.DB) *DBStore {
	return &DBStore{database: database}
}

var Store *DBStore

func (s *DBStore) GetOrCreate(sessionID string) *Session {
	// Try to get session from DB
	dbSess, err := s.database.GetSession(sessionID)
	if err != nil {
		// Session might not exist yet if called directly from chat before UI created it,
		// but with the new UI it should exist. Return a dummy view just in case.
		return &Session{
			ID:          sessionID,
			Messages:    []Message{},
			Checkpoints: []Checkpoint{},
			Model:       "qwen2.5-coder:1.5b", // Fallback
		}
	}

	msgs, _ := s.GetMessagesFromDB(sessionID)
	chks, _ := s.GetCheckpointsFromDB(sessionID)

	return &Session{
		ID:          dbSess.ID,
		Messages:    msgs,
		Checkpoints: chks,
		Model:       dbSess.Model,
		CreatedAt:   dbSess.CreatedAt,
		UpdatedAt:   dbSess.UpdatedAt,
	}
}

func (s *DBStore) AppendMessage(sessionID string, msg Message) *db.Message {
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}
	est := EstimateMessagesTokens([]Message{msg})
	saved, err := s.database.AppendMessage(newID(), sessionID, msg.Role, msg.Content, msg.IsCheckpoint, est)
	if err != nil {
		log.Printf("Failed to append message to DB: %v", err)
		return nil
	}
	s.database.TouchSession(sessionID)
	return saved
}

func (s *DBStore) GetMessagesFromDB(sessionID string) ([]Message, error) {
	dbMsgs, err := s.database.GetMessages(sessionID)
	if err != nil {
		return nil, err
	}
	var msgs []Message
	for _, m := range dbMsgs {
		msgs = append(msgs, Message{
			Role:         m.Role,
			Content:      m.Content,
			Timestamp:    m.CreatedAt,
			IsCheckpoint: m.IsCheckpoint,
			Sequence:     m.Sequence,
		})
	}
	return msgs, nil
}

func (s *DBStore) GetCheckpointsFromDB(sessionID string) ([]Checkpoint, error) {
	dbChks, err := s.database.GetCheckpoints(sessionID)
	if err != nil {
		return nil, err
	}
	var chks []Checkpoint
	for _, c := range dbChks {
		chks = append(chks, Checkpoint{
			CreatedAt:    c.CreatedAt,
			TokensBefore: c.TokensBefore,
			TokensAfter:  c.TokensAfter,
			Summary:      c.Summary,
		})
	}
	return chks, nil
}

func (s *DBStore) GetMessages(sessionID string) []Message {
	msgs, err := s.GetMessagesFromDB(sessionID)
	if err != nil {
		log.Printf("Failed to get messages: %v", err)
		return []Message{}
	}
	return msgs
}

func (s *DBStore) GetTokenUsage(sessionID, model string) (used int, total int, pct float64) {
	total = GetModelContextWindow(model)

	msgs := s.GetMessages(sessionID)
	used = EstimateMessagesTokens(msgs)
	if total > 0 {
		pct = float64(used) / float64(total)
	}
	return used, total, pct
}

func (s *DBStore) Reset(sessionID string) {
	err := s.database.DeleteMessages(sessionID)
	if err != nil {
		log.Printf("Failed to reset session: %v", err)
	}
	s.database.TouchSession(sessionID)
}

// ReplaceWithCheckpoint collapses every message with sequence < keepFromSeq
// into a single checkpoint message, keeping everything from keepFromSeq
// onward untouched. Using a sequence threshold (rather than wiping and
// re-inserting the whole table) means a chat turn that appends new messages
// concurrently with this call can never have those messages deleted — they
// all land at sequence >= keepFromSeq and are simply left alone.
func (s *DBStore) ReplaceWithCheckpoint(sessionID string, keepFromSeq int, checkpoint Checkpoint) error {
	if err := s.database.ReplaceMessagesWithCheckpoint(sessionID, newID(), newID(), keepFromSeq, checkpoint.Summary, checkpoint.TokensBefore, checkpoint.TokensAfter); err != nil {
		return err
	}
	s.database.TouchSession(sessionID)
	return nil
}
