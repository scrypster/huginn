package conntools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/tools"
)

// notionDo performs an authenticated Notion API request.
func notionDo(ctx context.Context, method, apiURL, token string, body io.Reader) (string, error) {
	req, err := http.NewRequestWithContext(ctx, method, apiURL, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Notion-Version", "2022-06-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("network: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, data)
	}
	return string(data), nil
}

// notionCreds extracts the token from a connection.
func notionCreds(mgr *connections.Manager, conn connections.Connection) (string, error) {
	creds, err := mgr.GetCredentials(conn.ID)
	if err != nil {
		return "", err
	}
	return creds["token"], nil
}

// --- notion_search ---

type notionSearchTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *notionSearchTool) Name() string        { return "notion_search" }
func (t *notionSearchTool) Description() string { return "Search Notion pages and databases." }
func (t *notionSearchTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *notionSearchTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "notion_search",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"query": {Type: "string", Description: "Search query text"},
					"limit": {Type: "integer", Description: "Maximum number of results (default 10)"},
				},
			},
		},
	}
}
func (t *notionSearchTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := notionCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "notion_search: auth: " + err.Error()}
	}

	limit := int(floatArg(args, "limit"))
	if limit <= 0 {
		limit = 10
	}

	payload := map[string]any{
		"page_size": limit,
	}
	if q, ok := args["query"].(string); ok && q != "" {
		payload["query"] = q
	}

	bodyBytes, _ := json.Marshal(payload)
	out, err := notionDo(ctx, "POST", "https://api.notion.com/v1/search", token, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- notion_get_page ---

type notionGetPageTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *notionGetPageTool) Name() string        { return "notion_get_page" }
func (t *notionGetPageTool) Description() string { return "Get a Notion page by ID." }
func (t *notionGetPageTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *notionGetPageTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "notion_get_page",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"page_id"},
				Properties: map[string]backend.ToolProperty{
					"page_id": {Type: "string", Description: "The Notion page ID"},
				},
			},
		},
	}
}
func (t *notionGetPageTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := notionCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "notion_get_page: auth: " + err.Error()}
	}
	pageID, ok := args["page_id"].(string)
	if !ok || pageID == "" {
		return tools.ToolResult{IsError: true, Error: "notion_get_page: page_id is required"}
	}
	apiURL := fmt.Sprintf("https://api.notion.com/v1/pages/%s", pageID)
	out, err := notionDo(ctx, "GET", apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- notion_get_database ---

type notionGetDatabaseTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *notionGetDatabaseTool) Name() string        { return "notion_get_database" }
func (t *notionGetDatabaseTool) Description() string { return "Get a Notion database by ID." }
func (t *notionGetDatabaseTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *notionGetDatabaseTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "notion_get_database",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"database_id"},
				Properties: map[string]backend.ToolProperty{
					"database_id": {Type: "string", Description: "The Notion database ID"},
				},
			},
		},
	}
}
func (t *notionGetDatabaseTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := notionCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "notion_get_database: auth: " + err.Error()}
	}
	dbID, ok := args["database_id"].(string)
	if !ok || dbID == "" {
		return tools.ToolResult{IsError: true, Error: "notion_get_database: database_id is required"}
	}
	apiURL := fmt.Sprintf("https://api.notion.com/v1/databases/%s", dbID)
	out, err := notionDo(ctx, "GET", apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- notion_query_database ---

type notionQueryDatabaseTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *notionQueryDatabaseTool) Name() string        { return "notion_query_database" }
func (t *notionQueryDatabaseTool) Description() string { return "Query a Notion database with optional filters." }
func (t *notionQueryDatabaseTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *notionQueryDatabaseTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "notion_query_database",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"database_id"},
				Properties: map[string]backend.ToolProperty{
					"database_id": {Type: "string", Description: "The Notion database ID"},
					"filter":      {Type: "object", Description: "Notion filter object (optional)"},
					"limit":       {Type: "integer", Description: "Maximum number of results (default 10)"},
				},
			},
		},
	}
}
func (t *notionQueryDatabaseTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := notionCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "notion_query_database: auth: " + err.Error()}
	}
	dbID, ok := args["database_id"].(string)
	if !ok || dbID == "" {
		return tools.ToolResult{IsError: true, Error: "notion_query_database: database_id is required"}
	}

	limit := int(floatArg(args, "limit"))
	if limit <= 0 {
		limit = 10
	}

	payload := map[string]any{
		"page_size": limit,
	}
	if filter, ok := args["filter"]; ok && filter != nil {
		payload["filter"] = filter
	}

	bodyBytes, _ := json.Marshal(payload)
	apiURL := fmt.Sprintf("https://api.notion.com/v1/databases/%s/query", dbID)
	out, err := notionDo(ctx, "POST", apiURL, token, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- notion_create_page ---

type notionCreatePageTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *notionCreatePageTool) Name() string        { return "notion_create_page" }
func (t *notionCreatePageTool) Description() string { return "Create a new Notion page. Requires user approval." }
func (t *notionCreatePageTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *notionCreatePageTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "notion_create_page",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"parent_id", "title"},
				Properties: map[string]backend.ToolProperty{
					"parent_id": {Type: "string", Description: "Parent page or database ID"},
					"title":     {Type: "string", Description: "Page title"},
				},
			},
		},
	}
}
func (t *notionCreatePageTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := notionCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "notion_create_page: auth: " + err.Error()}
	}
	parentID, _ := args["parent_id"].(string)
	title, _ := args["title"].(string)
	if parentID == "" || title == "" {
		return tools.ToolResult{IsError: true, Error: "notion_create_page: parent_id and title are required"}
	}

	payload := map[string]any{
		"parent": map[string]any{
			"page_id": parentID,
		},
		"properties": map[string]any{
			"title": map[string]any{
				"title": []map[string]any{
					{"text": map[string]any{"content": title}},
				},
			},
		},
	}

	bodyBytes, _ := json.Marshal(payload)
	out, err := notionDo(ctx, "POST", "https://api.notion.com/v1/pages", token, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerNotionTools registers all 5 Notion tools.
func registerNotionTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{
		"notion_search", "notion_get_page", "notion_get_database",
		"notion_query_database", "notion_create_page",
	}
	for _, n := range names {
		reg.Unregister(n)
	}
	strictInject(reg, &notionSearchTool{mgr: mgr, conns: conns})
	strictInject(reg, &notionGetPageTool{mgr: mgr, conns: conns})
	strictInject(reg, &notionGetDatabaseTool{mgr: mgr, conns: conns})
	strictInject(reg, &notionQueryDatabaseTool{mgr: mgr, conns: conns})
	strictInject(reg, &notionCreatePageTool{mgr: mgr, conns: conns})
	return nil
}
