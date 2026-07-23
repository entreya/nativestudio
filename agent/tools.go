package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/entreya/nativestudio/db"
	"github.com/entreya/nativestudio/editor"
)

// SafetyLevel categorizes how dangerous a tool operation is.
type SafetyLevel int

const (
	Safe             SafetyLevel = iota // Read-only operations
	RequiresApproval                    // Writes that need user confirmation
	Dangerous                           // Destructive operations
)

// ToolInput is the parsed JSON arguments map for a tool call.
type ToolInput map[string]any

type ToolMeta struct {
	SessionID     string
	RunID         string
	WorkspaceRoot string
	DB            *db.DB
	// State carries the current editor context (active/open/recent files) so
	// tools like find_files can rank results the user is more likely to mean
	// (e.g. a file they already have open) above unrelated matches.
	State editor.EditorState
}

// ToolResult is the structured output from a tool execution.
type ToolResult struct {
	OK      bool   `json:"ok"`
	Content any    `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Tool defines a single callable tool with its metadata and executor.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
	Safety      SafetyLevel
	Execute     func(ctx context.Context, input ToolInput, meta ToolMeta) (ToolResult, error)
}

// Registry holds all registered tools and the workspace root they operate on.
type Registry struct {
	mu            sync.RWMutex
	tools         map[string]*Tool
	workspaceRoot string
}

// NewRegistry creates a Registry pre-loaded with the default filesystem, search,
// and editor context tools.
func NewRegistry(workspaceRoot string) *Registry {
	r := &Registry{
		tools:         make(map[string]*Tool),
		workspaceRoot: workspaceRoot,
	}
	registerFilesystemTools(r, workspaceRoot)
	registerFileSearchTools(r)
	registerSearchTools(r, workspaceRoot)
	registerInternetSearchTool(r)
	registerEditorContextTool(r)
	registerFollowUpTool(r)
	registerPatchTools(r)
	registerCommandTool(r)
	return r
}

// registerFollowUpTool gives the model a structured way to pause and ask the
// user for missing information. The loop intercepts this tool before Execute.
func registerFollowUpTool(r *Registry) {
	r.Register(&Tool{
		Name:        "ask_follow_up",
		Description: "Ask the user one concise clarifying or next-step question. Provide 2 to 5 short options when useful and use multiselect when more than one answer may apply.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question": map[string]any{"type": "string", "description": "The concise question to show the user."},
				"options": map[string]any{
					"type": "array", "items": map[string]any{"type": "string"},
					"description": "Two to five short suggested answers.",
				},
				"input_type": map[string]any{
					"type": "string", "enum": []string{"text", "select", "multiselect"},
					"description": "Use select for one fixed choice, multiselect when multiple options may be correct, and text for a free-form answer.",
				},
			},
			"required": []string{"question"},
		},
		Safety: Safe,
		Execute: func(ctx context.Context, input ToolInput, meta ToolMeta) (ToolResult, error) {
			return ToolResult{OK: true}, nil
		},
	})
}

// Register adds a tool to the registry. Tool names must be unique: all tools
// are registered once at startup from NewRegistry, so a collision is a
// programming error, not a runtime condition — it panics immediately rather
// than silently overwriting the earlier tool (which previously happened
// silently via a plain map assignment) or requiring every call site to
// check an error for something that should never happen once the binary
// has started successfully once.
func (r *Registry) Register(t *Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[t.Name]; exists {
		panic(fmt.Sprintf("agent: duplicate tool registration: %q", t.Name))
	}
	r.tools[t.Name] = t
}

// Get retrieves a tool by name.
func (r *Registry) Get(name string) (*Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// WorkspaceRoot returns the workspace root path.
func (r *Registry) WorkspaceRoot() string {
	return r.workspaceRoot
}

// SetWorkspaceRoot updates the workspace root for all tools.
func (r *Registry) SetWorkspaceRoot(ws string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workspaceRoot = ws
}

// OllamaDefinitions returns tool definitions in the format Ollama expects.
func (r *Registry) OllamaDefinitions() []map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var defs []map[string]any
	for _, t := range r.tools {
		defs = append(defs, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Parameters,
			},
		})
	}
	return defs
}

// registerEditorContextTool adds the get_editor_context tool.
// The tool's Execute closure captures a pointer to the run's State field
// at call time. Because AgentRun embeds the state by value, each run's
// get_editor_context tool is registered separately via RegisterEditorContextForRun.
func registerEditorContextTool(r *Registry) {
	// Register a stub; the real implementation is injected per-run via closure below.
	// We need to register it here so OllamaDefinitions() includes it in the schema.
	r.Register(&Tool{
		Name:        "get_editor_context",
		Description: "Returns the current editor state: active file, cursor position, selected code or symbol, and recently opened/edited files.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Safety: Safe,
		// Default execute returns an empty state; overridden per-run below.
		Execute: func(ctx context.Context, input ToolInput, meta ToolMeta) (ToolResult, error) {
			return ToolResult{OK: true, Content: editor.EditorState{}}, nil
		},
	})
}

// InjectEditorState replaces the get_editor_context executor with a closure
// that returns the specific EditorState for this run. Called by agent.Run()
// after creating the AgentRun struct.
func (r *Registry) InjectEditorState(state editor.EditorState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tools["get_editor_context"]; ok {
		t.Execute = func(ctx context.Context, input ToolInput, meta ToolMeta) (ToolResult, error) {
			return ToolResult{OK: true, Content: state}, nil
		}
	}
}
