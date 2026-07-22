package editor

// EditorState captures the current state of the user's editor at a point in time.
// It is transmitted from the frontend on every chat request.
type EditorState struct {
	ActiveFile        string       `json:"active_file"`
	ActiveFileContent string       `json:"active_file_content,omitempty"`
	OpenFiles         []string     `json:"open_files"`
	RecentFiles       []string     `json:"recent_files"`
	Cursor            Cursor       `json:"cursor"`
	Selection         *Selection   `json:"selection,omitempty"`
	SelectedSymbol    *Symbol      `json:"selected_symbol,omitempty"`
	RecentEdits       []RecentEdit `json:"recent_edits"`
}

// Cursor represents the position of the text cursor in the editor.
type Cursor struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// Selection represents a highlighted range of text in the editor.
type Selection struct {
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Text      string `json:"text"`
}

// Symbol represents a code symbol (function, class, etc.) at the cursor.
type Symbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Parent    string `json:"parent"`
	FilePath  string `json:"file_path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

// RecentEdit records the line range of a recent modification in a file.
type RecentEdit struct {
	File  string `json:"file"`
	Lines string `json:"lines"`
}
