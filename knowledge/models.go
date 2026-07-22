package knowledge

import "time"

type RepositoryFile struct {
	ID           string    `json:"id"`
	WorkspaceID  string    `json:"workspace_id"`
	Path         string    `json:"path"`
	Language     string    `json:"language"`
	SizeBytes    int64     `json:"size_bytes"`
	ModifiedAtNS int64     `json:"modified_at_ns,omitempty"`
	ContentHash  string    `json:"content_hash"`
	Status       string    `json:"status"`
	SkipReason   string    `json:"skip_reason,omitempty"`
	GitStatus    string    `json:"git_status,omitempty"`
	IndexedAt    time.Time `json:"indexed_at"`
}

type FileManifest struct {
	Path         string
	ContentHash  string
	GitStatus    string
	SizeBytes    int64
	ModifiedAtNS int64
}

type IndexJob struct {
	ID          string
	WorkspaceID string
	RunID       string
	Path        string
	ContentHash string
	JobType     string
	Priority    int
	Attempts    int
}

type Symbol struct {
	ID            string `json:"id"`
	FileID        string `json:"file_id"`
	Path          string `json:"path"`
	Name          string `json:"symbol_name"`
	QualifiedName string `json:"qualified_name"`
	Type          string `json:"symbol_type"`
	Signature     string `json:"signature"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
	ContentHash   string `json:"content_hash"`
	Summary       string `json:"summary,omitempty"`
}

type Chunk struct {
	ID            string    `json:"id"`
	FileID        string    `json:"file_id"`
	SymbolID      string    `json:"symbol_id,omitempty"`
	Path          string    `json:"path"`
	StartLine     int       `json:"start_line"`
	EndLine       int       `json:"end_line"`
	Content       string    `json:"content"`
	ContentHash   string    `json:"content_hash"`
	TokenEstimate int       `json:"token_estimate"`
	Embedding     []float32 `json:"-"`
}

type FileSummary struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	Summary     string `json:"summary"`
	ContentHash string `json:"content_hash"`
	Version     int    `json:"version"`
	Model       string `json:"model"`
	Status      string `json:"status"`
}

type ProjectSummary struct {
	ID         string `json:"id"`
	Summary    string `json:"summary"`
	SourceHash string `json:"source_hash"`
	Version    int    `json:"version"`
	Model      string `json:"model"`
	Status     string `json:"status"`
}

type ModuleSummary struct {
	ID         string `json:"id"`
	ModulePath string `json:"module_path"`
	Summary    string `json:"summary"`
	SourceHash string `json:"source_hash"`
	Version    int    `json:"version"`
	Model      string `json:"model"`
	Status     string `json:"status"`
}

type FactSource struct {
	Path        string `json:"path"`
	SymbolName  string `json:"symbol_name,omitempty"`
	StartLine   int    `json:"start_line,omitempty"`
	EndLine     int    `json:"end_line,omitempty"`
	ContentHash string `json:"content_hash"`
}

type Fact struct {
	ID             string       `json:"id"`
	Category       string       `json:"category"`
	Fact           string       `json:"fact"`
	Confidence     float64      `json:"confidence"`
	Status         string       `json:"status"`
	Pinned         bool         `json:"pinned"`
	IsRule         bool         `json:"is_rule"`
	Sources        []FactSource `json:"sources"`
	LastVerifiedAt *time.Time   `json:"last_verified_at,omitempty"`
}

type Candidate struct {
	Kind       string   `json:"kind"`
	Path       string   `json:"path,omitempty"`
	Symbol     string   `json:"symbol,omitempty"`
	StartLine  int      `json:"start_line,omitempty"`
	EndLine    int      `json:"end_line,omitempty"`
	Content    string   `json:"content"`
	Score      float64  `json:"score"`
	Reasons    []string `json:"reasons"`
	TokenCount int      `json:"token_count"`
}

type IndexStatus struct {
	RunID               string     `json:"run_id,omitempty"`
	WorkspaceID         string     `json:"workspace_id"`
	Status              string     `json:"status"`
	Processed           int        `json:"processed"`
	Total               int        `json:"total"`
	Skipped             int        `json:"skipped"`
	Errors              int        `json:"errors"`
	EnrichmentStatus    string     `json:"enrichment_status,omitempty"`
	EnrichmentRemaining int        `json:"enrichment_remaining,omitempty"`
	StartedAt           *time.Time `json:"started_at,omitempty"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
	Error               string     `json:"error,omitempty"`
}

type Decision struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id,omitempty"`
	Decision  string    `json:"decision"`
	Reason    string    `json:"reason"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}
type Learning struct {
	ID          string    `json:"id"`
	SessionID   string    `json:"session_id,omitempty"`
	RunID       string    `json:"run_id"`
	Learning    string    `json:"learning"`
	PatchStatus string    `json:"patch_status"`
	CreatedAt   time.Time `json:"created_at"`
}

type IndexSettings struct {
	IncludePaths         []string `json:"include_paths"`
	ExcludePaths         []string `json:"exclude_paths"`
	SensitivePatterns    []string `json:"sensitive_patterns"`
	MaximumFileSizeBytes int64    `json:"maximum_file_size_bytes"`
}
type IndexError struct {
	Path      string    `json:"path"`
	Stage     string    `json:"stage"`
	Error     string    `json:"error"`
	CreatedAt time.Time `json:"created_at"`
}
type IndexSkip struct {
	Path      string    `json:"path"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

type Overview struct {
	WorkspaceID   string          `json:"workspace_id"`
	IndexedFiles  int             `json:"indexed_files"`
	IndexedChunks int             `json:"indexed_chunks"`
	SymbolCount   int             `json:"symbol_count"`
	StaleCount    int             `json:"stale_count"`
	Languages     map[string]int  `json:"languages"`
	Project       *ProjectSummary `json:"project_summary,omitempty"`
	Modules       []ModuleSummary `json:"module_summaries"`
	Files         []FileSummary   `json:"file_summaries"`
	Facts         []Fact          `json:"facts"`
	Symbols       []Symbol        `json:"important_symbols"`
	Decisions     []Decision      `json:"decisions"`
	Learnings     []Learning      `json:"recent_learnings"`
	Status        IndexStatus     `json:"index_status"`
	Errors        []IndexError    `json:"indexing_errors"`
	SkippedFiles  []IndexSkip     `json:"skipped_files"`
}
