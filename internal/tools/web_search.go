package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/backend"
)

const (
	braveSearchURL   = "https://api.search.brave.com/res/v1/web/search"
	braveSearchCount = 5
	webSearchTimeout = 10 * time.Second
)

// WebSearchTool performs web searches via the Brave Search API.
// Not registered if APIKey is empty.
type WebSearchTool struct {
	APIKey string
	client *http.Client // injectable for testing
}

func (t *WebSearchTool) Name() string { return "web_search" }
func (t *WebSearchTool) Description() string {
	return "Search the web using Brave Search. Returns title, URL, and description for top results."
}
func (t *WebSearchTool) Permission() PermissionLevel { return PermRead }

func (t *WebSearchTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "web_search",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"query"},
				Properties: map[string]backend.ToolProperty{
					"query": {Type: "string", Description: "Search query"},
					"count": {Type: "integer", Description: "Number of results (default 5, max 10)"},
				},
			},
		},
	}
}

func (t *WebSearchTool) httpClient() *http.Client {
	if t.client != nil {
		return t.client
	}
	return &http.Client{Timeout: webSearchTimeout}
}

func (t *WebSearchTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	query, ok := args["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return ToolResult{IsError: true, Error: "web_search: 'query' argument required"}
	}

	count := braveSearchCount
	if c, ok := args["count"]; ok {
		switch v := c.(type) {
		case float64:
			count = int(v)
		case int:
			count = v
		}
		if count < 1 {
			count = 1
		}
		if count > 10 {
			count = 10
		}
	}

	reqURL := fmt.Sprintf("%s?q=%s&count=%d", braveSearchURL, url.QueryEscape(query), count)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("web_search: build request: %v", err)}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", t.APIKey)

	resp, err := t.httpClient().Do(req)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("web_search: http: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return ToolResult{
			IsError: true,
			Error:   fmt.Sprintf("web_search: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body))),
		}
	}

	var result braveSearchResponse
	limited := io.LimitReader(resp.Body, 1<<20) // 1MB cap on success response
	if err := json.NewDecoder(limited).Decode(&result); err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("web_search: decode: %v", err)}
	}

	var sb strings.Builder
	for i, r := range result.Web.Results {
		fmt.Fprintf(&sb, "%d. %s\n   URL: %s\n   %s\n\n", i+1, r.Title, r.URL, r.Description)
	}
	if sb.Len() == 0 {
		return ToolResult{Output: "No results found."}
	}
	return ToolResult{Output: strings.TrimRight(sb.String(), "\n")}
}

type braveSearchResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}
