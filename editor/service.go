package editor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/entreya/nativestudio/db"
)

// EditorContextService persists and retrieves editor state per session.
type EditorContextService struct {
	db *db.DB
}

// NewEditorContextService creates a new EditorContextService.
func NewEditorContextService(database *db.DB) *EditorContextService {
	return &EditorContextService{db: database}
}

// Save persists the latest editor state for a session.
// Uses INSERT OR REPLACE so each session has exactly one current state row.
func (s *EditorContextService) Save(ctx context.Context, sessionID, messageID string, state EditorState) error {
	openFilesJSON, err := json.Marshal(state.OpenFiles)
	if err != nil {
		return fmt.Errorf("marshal open_files: %w", err)
	}
	recentFilesJSON, err := json.Marshal(state.RecentFiles)
	if err != nil {
		return fmt.Errorf("marshal recent_files: %w", err)
	}
	recentEditsJSON, err := json.Marshal(state.RecentEdits)
	if err != nil {
		return fmt.Errorf("marshal recent_edits: %w", err)
	}
	selectedSymbolJSON, err := json.Marshal(state.SelectedSymbol)
	if err != nil {
		return fmt.Errorf("marshal selected_symbol: %w", err)
	}

	var selStart, selEnd *int
	var selText *string
	if state.Selection != nil {
		selStart = &state.Selection.StartLine
		selEnd = &state.Selection.EndLine
		selText = &state.Selection.Text
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO editor_states
			(id, session_id, message_id, active_file, open_files,
			 cursor_line, cursor_column,
			 selection_start, selection_end, selection_text,
			 selected_symbol, recent_files, recent_edits)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		sessionID, // use session_id as the primary key so INSERT OR REPLACE always updates the same row
		sessionID,
		messageID,
		state.ActiveFile,
		string(openFilesJSON),
		state.Cursor.Line,
		state.Cursor.Column,
		selStart,
		selEnd,
		selText,
		string(selectedSymbolJSON),
		string(recentFilesJSON),
		string(recentEditsJSON),
	)
	if err != nil {
		return fmt.Errorf("save editor state: %w", err)
	}
	return nil
}

// Get retrieves the most recent editor state for a session.
func (s *EditorContextService) Get(ctx context.Context, sessionID string) (EditorState, error) {
	var (
		activeFile         string
		openFilesJSON      string
		cursorLine         int
		cursorCol          int
		selStart           *int
		selEnd             *int
		selText            *string
		selectedSymbolJSON string
		recentFilesJSON    string
		recentEditsJSON    string
	)

	err := s.db.QueryRowContext(ctx, `
		SELECT active_file, open_files, cursor_line, cursor_column,
		       selection_start, selection_end, selection_text,
		       selected_symbol, recent_files, recent_edits
		FROM editor_states WHERE session_id = ?
		ORDER BY created_at DESC LIMIT 1
	`, sessionID).Scan(
		&activeFile, &openFilesJSON, &cursorLine, &cursorCol,
		&selStart, &selEnd, &selText,
		&selectedSymbolJSON, &recentFilesJSON, &recentEditsJSON,
	)
	if err != nil {
		return EditorState{}, fmt.Errorf("get editor state: %w", err)
	}

	var state EditorState
	state.ActiveFile = activeFile
	state.Cursor = Cursor{Line: cursorLine, Column: cursorCol}

	if err := json.Unmarshal([]byte(openFilesJSON), &state.OpenFiles); err != nil {
		state.OpenFiles = nil
	}
	if err := json.Unmarshal([]byte(recentFilesJSON), &state.RecentFiles); err != nil {
		state.RecentFiles = nil
	}
	if err := json.Unmarshal([]byte(recentEditsJSON), &state.RecentEdits); err != nil {
		state.RecentEdits = nil
	}

	if selStart != nil && selEnd != nil && selText != nil {
		state.Selection = &Selection{
			StartLine: *selStart,
			EndLine:   *selEnd,
			Text:      *selText,
		}
	}

	if selectedSymbolJSON != "" && selectedSymbolJSON != "null" {
		var sym Symbol
		if err := json.Unmarshal([]byte(selectedSymbolJSON), &sym); err == nil {
			state.SelectedSymbol = &sym
		}
	}

	return state, nil
}
