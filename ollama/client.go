package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type statusError struct {
	status int
	body   string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("ollama returned status %d: %s", e.status, e.body)
}

type Client struct {
	BaseURL        string
	SummaryModel   string
	EmbeddingModel string
	HTTP           *http.Client
	semaphore      chan struct{}
}

func NewClient(baseURL, summaryModel, embeddingModel string, concurrency int) *Client {
	if concurrency <= 0 {
		concurrency = 2
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), SummaryModel: summaryModel, EmbeddingModel: embeddingModel, HTTP: &http.Client{Timeout: 2 * time.Minute}, semaphore: make(chan struct{}, concurrency)}
}
func (c *Client) do(ctx context.Context, path string, payload any, target any) error {
	select {
	case c.semaphore <- struct{}{}:
		defer func() { <-c.semaphore }()
	case <-ctx.Done():
		return ctx.Err()
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return &statusError{status: resp.StatusCode, body: strings.TrimSpace(string(data))}
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(target)
}

func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	var response struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	err := c.do(ctx, "/api/embed", map[string]any{"model": c.EmbeddingModel, "input": texts}, &response)
	if err == nil {
		if len(response.Embeddings) != len(texts) {
			return nil, fmt.Errorf("ollama returned %d embeddings for %d inputs", len(response.Embeddings), len(texts))
		}
		return response.Embeddings, nil
	}
	var httpErr *statusError
	if !errors.As(err, &httpErr) || httpErr.status != http.StatusNotFound {
		return nil, err
	}
	var vectors [][]float32
	for _, text := range texts {
		var legacy struct {
			Embedding []float32 `json:"embedding"`
		}
		if legacyErr := c.do(ctx, "/api/embeddings", map[string]any{"model": c.EmbeddingModel, "prompt": text}, &legacy); legacyErr != nil {
			return nil, legacyErr
		}
		vectors = append(vectors, legacy.Embedding)
	}
	return vectors, nil
}

type structuredResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

func (c *Client) structured(ctx context.Context, prompt string, schema map[string]any, target any) error {
	payload := map[string]any{"model": c.SummaryModel, "stream": false, "format": schema, "messages": []map[string]string{{"role": "system", "content": "Return only valid JSON matching the supplied schema. Describe concrete observed behavior; never invent facts."}, {"role": "user", "content": prompt}}, "options": map[string]any{"temperature": 0.1, "num_predict": 1200}}
	for attempt := 0; attempt < 2; attempt++ {
		var response structuredResponse
		if err := c.do(ctx, "/api/chat", payload, &response); err != nil {
			return err
		}
		raw := extractJSON(response.Message.Content)
		if err := json.Unmarshal([]byte(raw), target); err == nil {
			return nil
		}
		payload["messages"] = []map[string]string{{"role": "system", "content": "Repair the invalid result and return only valid JSON matching the schema."}, {"role": "user", "content": raw}}
	}
	return fmt.Errorf("summary model returned invalid JSON after repair")
}
func extractJSON(value string) string {
	value = strings.TrimSpace(value)
	start, end := strings.Index(value, "{"), strings.LastIndex(value, "}")
	if start >= 0 && end >= start {
		return value[start : end+1]
	}
	return value
}

func (c *Client) SummarizeFile(ctx context.Context, filePath, language string, symbols []string, chunks []string) (string, error) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"purpose": map[string]any{"type": "string"}, "important_symbols": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "dependencies": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "important_behaviour": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "side_effects": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "required": []string{"purpose", "important_symbols", "dependencies", "important_behaviour", "side_effects"}, "additionalProperties": false}
	var result map[string]any
	prompt := fmt.Sprintf("Summarize repository file %s (%s). Symbols: %s\nRelevant source excerpts:\n%s", filePath, language, strings.Join(symbols, ", "), strings.Join(chunks, "\n---\n"))
	if err := c.structured(ctx, prompt, schema, &result); err != nil {
		return "", err
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

func (c *Client) SummarizeSymbol(ctx context.Context, path, name, kind, source string) (string, error) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"purpose": map[string]any{"type": "string"}, "inputs": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "outputs": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "calls": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "side_effects": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "failure_cases": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "required": []string{"purpose", "inputs", "outputs", "calls", "side_effects", "failure_cases"}, "additionalProperties": false}
	var result map[string]any
	prompt := fmt.Sprintf("Summarize %s %s in %s from this exact source only:\n%s", kind, name, path, source)
	if err := c.structured(ctx, prompt, schema, &result); err != nil {
		return "", err
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

func (c *Client) SummarizeModule(ctx context.Context, module string, fileSummaries []string) (string, error) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"module": map[string]any{"type": "string"}, "purpose": map[string]any{"type": "string"}, "important_behaviour": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "dependencies": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "required": []string{"module", "purpose", "important_behaviour", "dependencies"}, "additionalProperties": false}
	var result map[string]any
	prompt := fmt.Sprintf("Summarize module %s only from these file summaries:\n%s", module, strings.Join(fileSummaries, "\n"))
	if err := c.structured(ctx, prompt, schema, &result); err != nil {
		return "", err
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}

func (c *Client) SummarizeProject(ctx context.Context, name string, fileSummaries []string) (string, error) {
	if len(fileSummaries) > 20 {
		partials := make([]string, 0, (len(fileSummaries)+19)/20)
		for start := 0; start < len(fileSummaries); start += 20 {
			end := start + 20
			if end > len(fileSummaries) {
				end = len(fileSummaries)
			}
			partial, err := c.SummarizeProject(ctx, name+" section", fileSummaries[start:end])
			if err != nil {
				return "", err
			}
			partials = append(partials, partial)
		}
		return c.SummarizeProject(ctx, name, partials)
	}
	schema := map[string]any{"type": "object", "properties": map[string]any{"project_name": map[string]any{"type": "string"}, "project_type": map[string]any{"type": "string"}, "languages": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "frameworks": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "architecture": map[string]any{"type": "string"}, "entry_points": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "build_commands": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "test_commands": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "known_risks": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "required": []string{"project_name", "project_type", "languages", "frameworks", "architecture", "entry_points", "build_commands", "test_commands", "known_risks"}, "additionalProperties": false}
	var result map[string]any
	prompt := fmt.Sprintf("Create a concrete project summary for %s only from these validated file summaries. Do not infer unsupported technologies.\n%s", name, strings.Join(fileSummaries, "\n"))
	if err := c.structured(ctx, prompt, schema, &result); err != nil {
		return "", err
	}
	data, _ := json.Marshal(result)
	return string(data), nil
}
