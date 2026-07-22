package resolver

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"

	"github.com/entreya/nativestudio/db"
	"github.com/entreya/nativestudio/editor"
	"github.com/entreya/nativestudio/knowledge"
)

// ReferenceResolver resolves which file/symbol a user prompt is referring to.
type ReferenceResolver struct {
	mu             sync.RWMutex
	WorkspaceRoot  string
	DB             *db.DB
	WorkspaceID    string
	Embedder       knowledge.EmbeddingProvider
	EmbeddingModel string
}

func (r *ReferenceResolver) SetKnowledgeWorkspace(workspaceID string, embedder knowledge.EmbeddingProvider, embeddingModel string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.WorkspaceID = workspaceID
	r.Embedder = embedder
	r.EmbeddingModel = embeddingModel
}

// RetrieveKnowledge augments deterministic editor resolution with persisted
// exact/summary matches and a secondary semantic signal.
func (r *ReferenceResolver) RetrieveKnowledge(ctx context.Context, prompt string, state editor.EditorState, resolved []Candidate) ([]knowledge.Candidate, error) {
	r.mu.RLock()
	workspaceID, embedder, model := r.WorkspaceID, r.Embedder, r.EmbeddingModel
	r.mu.RUnlock()
	if workspaceID == "" {
		return nil, nil
	}
	exact, err := r.DB.SearchKnowledge(ctx, workspaceID, prompt, 12)
	if err != nil {
		return nil, err
	}
	if embedder != nil && hasSemanticIntent(prompt) {
		vectors, embedErr := embedder.Embed(ctx, []string{prompt})
		if embedErr == nil && len(vectors) == 1 {
			semantic, searchErr := r.DB.SearchEmbeddings(ctx, workspaceID, model, vectors[0], 6)
			if searchErr == nil {
				exact = append(exact, semantic...)
			}
		}
	}
	addPath := func(path string, score float64, reason string) {
		if path == "" {
			return
		}
		candidate, pathErr := r.DB.KnowledgeForPath(ctx, workspaceID, path)
		if pathErr == nil {
			candidate.Score = score
			candidate.Reasons = []string{reason}
			exact = append(exact, candidate)
		}
	}
	if state.ActiveFileContent == "" {
		addPath(state.ActiveFile, 90, "active_file")
	}
	for _, path := range state.OpenFiles {
		addPath(path, 70, "open_file")
	}
	for _, path := range state.RecentFiles {
		addPath(path, 65, "recent_file")
	}
	for _, candidate := range resolved {
		addPath(candidate.Path, candidate.Score*100, "resolved_reference")
	}
	result := deduplicateKnowledge(exact)
	if len(result) > 8 {
		result = result[:8]
	}
	return result, nil
}

func hasSemanticIntent(prompt string) bool {
	meaningful := 0
	for _, word := range strings.Fields(prompt) {
		word = strings.Trim(strings.ToLower(word), ".,!?;:()[]{}\"'`")
		if len([]rune(word)) >= 3 && word != "the" && word != "this" && word != "that" && word != "you" && word != "please" && word != "hello" && word != "hey" {
			meaningful++
		}
	}
	return meaningful > 0
}

func deduplicateKnowledge(items []knowledge.Candidate) []knowledge.Candidate {
	best := map[string]knowledge.Candidate{}
	for _, item := range items {
		key := fmt.Sprintf("%s:%s:%d:%d", item.Kind, item.Path, item.StartLine, item.EndLine)
		if item.Kind == "code" {
			key = item.Kind + ":" + item.Path
		}
		if current, ok := best[key]; !ok || item.Score > current.Score {
			best[key] = item
		}
	}
	result := make([]knowledge.Candidate, 0, len(best))
	for _, item := range best {
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Score > result[j].Score })
	return result
}

func (r *ReferenceResolver) SetWorkspaceRoot(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.WorkspaceRoot = path
}

// NewReferenceResolver creates a new ReferenceResolver.
func NewReferenceResolver(workspaceRoot string, database *db.DB) *ReferenceResolver {
	return &ReferenceResolver{
		WorkspaceRoot: workspaceRoot,
		DB:            database,
	}
}

// Resolve runs the full resolution pipeline:
//  1. Extract refs from the prompt
//  2. Apply deterministic rules
//  3. Handle "same X" by searching recent history
//  4. If no candidates yet, do a PascalCase ripgrep scan
//  5. Score all candidates against editor state + history
//  6. Sort by score
//  7. Deduplicate by path (keep highest score)
//  8. Return
func (r *ReferenceResolver) Resolve(
	ctx context.Context,
	prompt string,
	state editor.EditorState,
	history []db.Message,
) ([]Candidate, error) {
	r.mu.RLock()
	workspaceRoot := r.WorkspaceRoot
	r.mu.RUnlock()

	refs := ExtractRefs(prompt)
	candidates := ApplyRules(ctx, refs, state, workspaceRoot)

	// ── Handle "same X": search the last 5 messages for a matching file ───────
	if refs.HasSame && len(history) > 0 {
		last5 := history
		if len(last5) > 5 {
			last5 = last5[len(last5)-5:]
		}
		for _, msg := range last5 {
			for _, fileRef := range findFileRefInText(ctx, msg.Content) {
				candidates = append(candidates, Candidate{
					Path:    fileRef,
					Score:   0.65,
					Reasons: []string{"same_referenced_in_history"},
				})
			}
		}
	}

	// ── Fallback: if no candidates and PascalCase names exist, do a broad scan ─
	if len(candidates) == 0 && len(refs.ExplicitNames) > 0 {
		for _, name := range refs.ExplicitNames {
			matches := findSymbolByName(ctx, name, workspaceRoot)
			for _, m := range matches {
				candidates = append(candidates, Candidate{
					Path:    m,
					Score:   0.50,
					Reasons: []string{fmt.Sprintf("fallback_scan:%s", name)},
				})
			}
		}
	}

	// ── Score all candidates ──────────────────────────────────────────────────
	for i := range candidates {
		ScoreCandidate(&candidates[i], state, history)
	}

	// ── Sort by score descending ──────────────────────────────────────────────
	SortCandidates(candidates)

	// ── Deduplicate by path (keep highest score for each path) ───────────────
	seen := make(map[string]bool)
	var deduped []Candidate
	for _, c := range candidates {
		if !seen[c.Path] {
			seen[c.Path] = true
			deduped = append(deduped, c)
		}
	}

	return deduped, nil
}

// findFileRefInText extracts file path references from arbitrary text.
// Used to trace file references from conversation history.
func findFileRefInText(ctx context.Context, text string) []string {
	// Try ripgrep if available, otherwise manual scan
	rgPath, err := exec.LookPath("rg")
	if err == nil {
		rgCtx, cancel := context.WithTimeout(ctx, execTimeout)
		defer cancel()
		cmd := exec.CommandContext(rgCtx, rgPath, "-o", `[\w./\-]+\.(php|go|ts|js|py|md|jsx|tsx|json|yaml|yml)`, "--no-filename")
		cmd.Stdin = strings.NewReader(text)
		out, err := cmd.Output()
		if err == nil && len(out) > 0 {
			var refs []string
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				line = strings.TrimSpace(line)
				if line != "" {
					refs = append(refs, line)
				}
			}
			return refs
		}
	}

	// Manual: split on whitespace and check for file-like tokens
	var refs []string
	extensions := []string{".php", ".go", ".ts", ".js", ".py", ".md", ".jsx", ".tsx", ".json", ".yaml", ".yml"}
	for _, word := range strings.Fields(text) {
		word = strings.Trim(word, ".,;:()[]\"'`")
		for _, ext := range extensions {
			if strings.HasSuffix(word, ext) {
				refs = append(refs, word)
				break
			}
		}
	}
	return refs
}
