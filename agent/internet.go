package agent

import (
	"context"
	"encoding/xml"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type internetSearchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

// registerInternetSearchTool gives the local model a read-only research path
// when the answer is not present in its knowledge or the workspace.
func registerInternetSearchTool(r *Registry) {
	r.Register(&Tool{
		Name:        "search_internet",
		Description: "Search the public internet for current or unfamiliar information. Use this before guessing when the user's request depends on facts not available in the workspace or your knowledge.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":       map[string]any{"type": "string", "description": "A focused search query."},
				"max_results": map[string]any{"type": "integer", "description": "Number of results, from 1 to 8. Defaults to 5."},
			},
			"required": []string{"query"},
		},
		Safety: Safe,
		Execute: func(ctx context.Context, input ToolInput, meta ToolMeta) (ToolResult, error) {
			query, _ := input["query"].(string)
			query = strings.TrimSpace(query)
			if query == "" {
				return ToolResult{OK: false, Error: "query is required"}, nil
			}
			limit := 5
			if raw, ok := input["max_results"].(float64); ok && raw >= 1 && raw <= 8 {
				limit = int(raw)
			}
			results, err := searchInternet(ctx, query, limit)
			if err != nil {
				return ToolResult{OK: false, Error: err.Error()}, nil
			}
			return ToolResult{OK: true, Content: map[string]any{"query": query, "results": results}}, nil
		},
	})
}

func searchInternet(ctx context.Context, query string, limit int) ([]internetSearchResult, error) {
	endpoint := "https://www.bing.com/search?format=rss&q=" + url.QueryEscape(query)
	return searchRSS(ctx, endpoint, query, limit)
}

func searchNews(ctx context.Context, query string, limit int) ([]internetSearchResult, error) {
	endpoint := "https://news.google.com/rss/search?q=" + url.QueryEscape(query) + "&hl=en&gl=US&ceid=US:en"
	return searchRSS(ctx, endpoint, query, limit)
}

func searchCurrentEvent(ctx context.Context, query string, limit int) ([]internetSearchResult, error) {
	newsResults, newsErr := searchNews(ctx, query, limit)
	if newsErr == nil && len(newsResults) > 0 {
		return newsResults, nil
	}
	webResults, webErr := searchInternet(ctx, query, limit)
	if webErr == nil {
		return webResults, nil
	}
	if newsErr != nil {
		return nil, fmt.Errorf("news search failed: %v; web fallback failed: %w", newsErr, webErr)
	}
	return nil, webErr
}

func searchRSS(ctx context.Context, endpoint, query string, limit int) ([]internetSearchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "NativeStudio/1.0")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("internet search unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("internet search returned status %d", resp.StatusCode)
	}
	var feed struct {
		Channel struct {
			Items []struct {
				Title       string `xml:"title"`
				Link        string `xml:"link"`
				Description string `xml:"description"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("decode internet search: %w", err)
	}
	if limit > len(feed.Channel.Items) {
		limit = len(feed.Channel.Items)
	}
	candidates := make([]internetSearchResult, 0, len(feed.Channel.Items))
	for _, item := range feed.Channel.Items {
		candidates = append(candidates, internetSearchResult{
			Title:       strings.TrimSpace(item.Title),
			URL:         strings.TrimSpace(item.Link),
			Description: cleanSearchDescription(item.Description),
		})
	}
	results := filterRelevantResults(query, candidates)
	if limit > len(results) {
		limit = len(results)
	}
	return results[:limit], nil
}

var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)

func cleanSearchDescription(value string) string {
	return strings.TrimSpace(html.UnescapeString(htmlTagPattern.ReplaceAllString(value, " ")))
}

func filterRelevantResults(query string, candidates []internetSearchResult) []internetSearchResult {
	tokens := significantSearchTokens(query)
	if len(tokens) == 0 {
		return candidates
	}
	minimumMatches := 2
	if len(tokens) == 1 {
		minimumMatches = 1
	}
	results := make([]internetSearchResult, 0, len(candidates))
	for _, candidate := range candidates {
		haystack := strings.ToLower(candidate.Title + " " + candidate.Description)
		matches := 0
		for _, token := range tokens {
			if fuzzyTokenMatch(token, haystack) {
				matches++
			}
		}
		if matches >= minimumMatches {
			results = append(results, candidate)
		}
	}
	return results
}

func fuzzyTokenMatch(token, text string) bool {
	if strings.Contains(text, token) {
		return true
	}
	if len(token) < 5 {
		return false
	}
	for _, word := range strings.FieldsFunc(text, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}) {
		if absInt(len(word)-len(token)) <= 1 && editDistanceAtMostOne(token, word) {
			return true
		}
	}
	return false
}

func editDistanceAtMostOne(a, b string) bool {
	if a == b {
		return true
	}
	if absInt(len(a)-len(b)) > 1 {
		return false
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	i, j, edits := 0, 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			i++
			j++
			continue
		}
		edits++
		if edits > 1 {
			return false
		}
		if len(a) == len(b) {
			i++
		}
		j++
	}
	if i < len(a) || j < len(b) {
		edits++
	}
	return edits <= 1
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func significantSearchTokens(query string) []string {
	stopwords := map[string]bool{
		"about": true, "and": true, "delhi": true, "india": true, "july": true,
		"news": true, "recent": true, "the": true, "this": true, "what": true,
	}
	fields := strings.Fields(strings.ToLower(strings.NewReplacer(`"`, "", "'", "").Replace(query)))
	seen := map[string]bool{}
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, " ,.!?:;()[]{}")
		if len(field) < 4 || stopwords[field] || seen[field] {
			continue
		}
		seen[field] = true
		result = append(result, field)
	}
	return result
}
