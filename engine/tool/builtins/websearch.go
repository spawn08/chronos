package builtins

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spawn08/chronos/engine/tool"
)

// defaultDDGAPIURLTemplate is the DuckDuckGo JSON API URL with a single %s for the query-escaped search term.
const defaultDDGAPIURLTemplate = "https://api.duckduckgo.com/?q=%s&format=json&no_html=1&skip_disambig=1"

// NewWebSearchTool creates a tool that searches the web using DuckDuckGo's instant answer API.
// timeout controls max request time (0 = 30s default).
// maxResults limits the number of results returned (0 = 5 default).
func NewWebSearchTool(timeout time.Duration, maxResults int) *tool.Definition {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if maxResults <= 0 {
		maxResults = 5
	}

	client := &http.Client{Timeout: timeout}
	return webSearchTool(client, maxResults, defaultDDGAPIURLTemplate)
}

// webSearchTool builds the standard web search tool. apiURLTemplate must contain one %s for url.QueryEscape(query).
func webSearchTool(client *http.Client, maxResults int, apiURLTemplate string) *tool.Definition {
	return &tool.Definition{
		Name:        "web_search",
		Description: "Search the web using DuckDuckGo and return results with titles, URLs, and snippets.",
		Permission:  tool.PermRequireApproval,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The search query",
				},
			},
			"required": []string{"query"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			query, ok := args["query"].(string)
			if !ok || query == "" {
				return nil, fmt.Errorf("web_search: 'query' argument is required")
			}

			apiURL := fmt.Sprintf(apiURLTemplate, url.QueryEscape(query))

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
			if err != nil {
				return nil, fmt.Errorf("web_search: %w", err)
			}
			req.Header.Set("User-Agent", "Chronos/1.0")

			resp, err := client.Do(req)
			if err != nil {
				return nil, fmt.Errorf("web_search: %w", err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			if err != nil {
				return nil, fmt.Errorf("web_search: reading response: %w", err)
			}

			var ddg struct {
				Abstract       string `json:"Abstract"`
				AbstractSource string `json:"AbstractSource"`
				AbstractURL    string `json:"AbstractURL"`
				Answer         string `json:"Answer"`
				RelatedTopics  []struct {
					Text     string `json:"Text"`
					FirstURL string `json:"FirstURL"`
				} `json:"RelatedTopics"`
			}
			if err := json.Unmarshal(body, &ddg); err != nil {
				return nil, fmt.Errorf("web_search: parsing response: %w", err)
			}

			var results []map[string]string

			if ddg.Abstract != "" {
				results = append(results, map[string]string{
					"title":   ddg.AbstractSource,
					"url":     ddg.AbstractURL,
					"snippet": ddg.Abstract,
				})
			}

			if ddg.Answer != "" {
				results = append(results, map[string]string{
					"title":   "Instant Answer",
					"url":     "",
					"snippet": ddg.Answer,
				})
			}

			for _, topic := range ddg.RelatedTopics {
				if len(results) >= maxResults {
					break
				}
				if topic.Text == "" {
					continue
				}
				title := topic.Text
				if len(title) > 100 {
					title = title[:100]
				}
				results = append(results, map[string]string{
					"title":   title,
					"url":     topic.FirstURL,
					"snippet": topic.Text,
				})
			}

			return map[string]any{
				"query":   query,
				"results": results,
				"count":   len(results),
			}, nil
		},
	}
}

// NewWebSearchToolWithEngine creates a web search tool that accepts a custom search
// URL template. The template must contain %s for the query placeholder.
func NewWebSearchToolWithEngine(engineURL string, timeout time.Duration) *tool.Definition {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if engineURL == "" {
		engineURL = "https://api.duckduckgo.com/?q=%s&format=json&no_html=1"
	}
	if !strings.Contains(engineURL, "%s") {
		engineURL += "?q=%s"
	}

	client := &http.Client{Timeout: timeout}

	return &tool.Definition{
		Name:        "web_search_custom",
		Description: "Search the web using a custom search engine endpoint.",
		Permission:  tool.PermRequireApproval,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The search query",
				},
			},
			"required": []string{"query"},
		},
		Handler: func(ctx context.Context, args map[string]any) (any, error) {
			query, ok := args["query"].(string)
			if !ok || query == "" {
				return nil, fmt.Errorf("web_search_custom: 'query' argument is required")
			}

			searchURL := fmt.Sprintf(engineURL, url.QueryEscape(query))
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, http.NoBody)
			if err != nil {
				return nil, fmt.Errorf("web_search_custom: %w", err)
			}
			req.Header.Set("User-Agent", "Chronos/1.0")

			resp, err := client.Do(req)
			if err != nil {
				return nil, fmt.Errorf("web_search_custom: %w", err)
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			if err != nil {
				return nil, fmt.Errorf("web_search_custom: reading response: %w", err)
			}

			return map[string]any{
				"query":       query,
				"status_code": resp.StatusCode,
				"body":        string(body),
			}, nil
		},
	}
}
