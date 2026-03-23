package conntools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/tools"
)

// splunkDo performs an authenticated Splunk API request.
func splunkDo(ctx context.Context, method, apiURL, token string, body io.Reader) (string, error) {
	req, err := http.NewRequestWithContext(ctx, method, apiURL, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	client := &http.Client{Timeout: 60 * time.Second}
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

// splunkCreds extracts base URL and token from a connection.
func splunkCreds(mgr *connections.Manager, conn connections.Connection) (baseURL, token string, err error) {
	creds, err := mgr.GetCredentials(conn.ID)
	if err != nil {
		return "", "", err
	}
	baseURL = conn.Metadata["url"]
	if baseURL == "" {
		return "", "", fmt.Errorf("splunk: url not configured")
	}
	// Normalize: strip trailing slash
	baseURL = strings.TrimRight(baseURL, "/")
	return baseURL, creds["token"], nil
}

// --- splunk_search ---

type splunkSearchTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *splunkSearchTool) Name() string        { return "splunk_search" }
func (t *splunkSearchTool) Description() string { return "Run a Splunk search query (SPL). Returns events matching the search." }
func (t *splunkSearchTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *splunkSearchTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "splunk_search",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"query"},
				Properties: map[string]backend.ToolProperty{
					"query":     {Type: "string", Description: `SPL query e.g. "index=main error | head 10"`},
					"earliest":  {Type: "string", Description: `Earliest time bound (e.g. "-1h", default "-24h")`},
					"latest":    {Type: "string", Description: `Latest time bound (default "now")`},
					"max_count": {Type: "integer", Description: "Maximum number of events to return (default 100, max 10000)"},
				},
			},
		},
	}
}
func (t *splunkSearchTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, token, err := splunkCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "splunk_search: auth: " + err.Error()}
	}
	query, _ := args["query"].(string)
	if !strings.HasPrefix(strings.TrimSpace(query), "search") {
		query = "search " + query
	}
	earliest, _ := args["earliest"].(string)
	if earliest == "" {
		earliest = "-24h"
	}
	latest, _ := args["latest"].(string)
	if latest == "" {
		latest = "now"
	}
	maxCount := int(floatArg(args, "max_count"))
	if maxCount <= 0 || maxCount > 10000 {
		maxCount = 100
	}
	params := url.Values{}
	params.Set("search", query)
	params.Set("earliest_time", earliest)
	params.Set("latest_time", latest)
	params.Set("max_count", fmt.Sprintf("%d", maxCount))
	params.Set("output_mode", "json")
	apiURL := fmt.Sprintf("%s/services/search/jobs/oneshot", baseURL)
	out, err := splunkDo(ctx, "POST", apiURL, token, strings.NewReader(params.Encode()))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- splunk_list_indexes ---

type splunkListIndexesTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *splunkListIndexesTool) Name() string        { return "splunk_list_indexes" }
func (t *splunkListIndexesTool) Description() string { return "List all Splunk indexes available to the current user." }
func (t *splunkListIndexesTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *splunkListIndexesTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "splunk_list_indexes",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:       "object",
				Properties: map[string]backend.ToolProperty{},
			},
		},
	}
}
func (t *splunkListIndexesTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, token, err := splunkCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "splunk_list_indexes: auth: " + err.Error()}
	}
	apiURL := fmt.Sprintf("%s/services/data/indexes?output_mode=json&count=0", baseURL)
	out, err := splunkDo(ctx, "GET", apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- splunk_list_saved_searches ---

type splunkListSavedSearchesTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *splunkListSavedSearchesTool) Name() string        { return "splunk_list_saved_searches" }
func (t *splunkListSavedSearchesTool) Description() string { return "List saved searches (reports and alerts) in Splunk." }
func (t *splunkListSavedSearchesTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *splunkListSavedSearchesTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "splunk_list_saved_searches",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"count":  {Type: "integer", Description: "Max saved searches to return (default 100)"},
					"search": {Type: "string", Description: "Filter saved searches by name"},
				},
			},
		},
	}
}
func (t *splunkListSavedSearchesTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, token, err := splunkCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "splunk_list_saved_searches: auth: " + err.Error()}
	}
	count := int(floatArg(args, "count"))
	if count <= 0 {
		count = 100
	}
	params := url.Values{}
	params.Set("output_mode", "json")
	params.Set("count", fmt.Sprintf("%d", count))
	if search, ok := args["search"].(string); ok && search != "" {
		params.Set("search", search)
	}
	apiURL := fmt.Sprintf("%s/services/saved/searches?%s", baseURL, params.Encode())
	out, err := splunkDo(ctx, "GET", apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- splunk_run_saved_search ---

type splunkRunSavedSearchTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *splunkRunSavedSearchTool) Name() string        { return "splunk_run_saved_search" }
func (t *splunkRunSavedSearchTool) Description() string { return "Run a saved Splunk search by name and return the results." }
func (t *splunkRunSavedSearchTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *splunkRunSavedSearchTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "splunk_run_saved_search",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"name"},
				Properties: map[string]backend.ToolProperty{
					"name": {Type: "string", Description: "Exact name of the saved search to run"},
				},
			},
		},
	}
}
func (t *splunkRunSavedSearchTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, token, err := splunkCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "splunk_run_saved_search: auth: " + err.Error()}
	}
	name, _ := args["name"].(string)
	apiURL := fmt.Sprintf("%s/services/saved/searches/%s/dispatch?output_mode=json", baseURL, url.PathEscape(name))
	out, err := splunkDo(ctx, "POST", apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- splunk_list_alerts ---

type splunkListAlertsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *splunkListAlertsTool) Name() string        { return "splunk_list_alerts" }
func (t *splunkListAlertsTool) Description() string { return "List fired alert actions (triggered alerts) in Splunk." }
func (t *splunkListAlertsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *splunkListAlertsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "splunk_list_alerts",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"count":    {Type: "integer", Description: "Max alerts to return (default 50)"},
					"earliest": {Type: "string", Description: `Time filter for alerts (e.g. "-24h")`},
				},
			},
		},
	}
}
func (t *splunkListAlertsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, token, err := splunkCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "splunk_list_alerts: auth: " + err.Error()}
	}
	count := int(floatArg(args, "count"))
	if count <= 0 {
		count = 50
	}
	params := url.Values{}
	params.Set("output_mode", "json")
	params.Set("count", fmt.Sprintf("%d", count))
	if earliest, ok := args["earliest"].(string); ok && earliest != "" {
		params.Set("earliest_time", earliest)
	}
	apiURL := fmt.Sprintf("%s/services/alerts/fired_alerts?%s", baseURL, params.Encode())
	out, err := splunkDo(ctx, "GET", apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- splunk_list_dashboards ---

type splunkListDashboardsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *splunkListDashboardsTool) Name() string        { return "splunk_list_dashboards" }
func (t *splunkListDashboardsTool) Description() string { return "List Splunk dashboards available to the current user." }
func (t *splunkListDashboardsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *splunkListDashboardsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "splunk_list_dashboards",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"count": {Type: "integer", Description: "Max dashboards to return (default 100)"},
				},
			},
		},
	}
}
func (t *splunkListDashboardsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, token, err := splunkCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "splunk_list_dashboards: auth: " + err.Error()}
	}
	count := int(floatArg(args, "count"))
	if count <= 0 {
		count = 100
	}
	apiURL := fmt.Sprintf("%s/services/data/ui/views?output_mode=json&count=%d", baseURL, count)
	out, err := splunkDo(ctx, "GET", apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerSplunkTools registers all 6 Splunk tools.
func registerSplunkTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{
		"splunk_search", "splunk_list_indexes", "splunk_list_saved_searches",
		"splunk_run_saved_search", "splunk_list_alerts", "splunk_list_dashboards",
	}
	for _, n := range names {
		reg.Unregister(n)
	}
	strictInject(reg, &splunkSearchTool{mgr: mgr, conns: conns})
	strictInject(reg, &splunkListIndexesTool{mgr: mgr, conns: conns})
	strictInject(reg, &splunkListSavedSearchesTool{mgr: mgr, conns: conns})
	strictInject(reg, &splunkRunSavedSearchTool{mgr: mgr, conns: conns})
	strictInject(reg, &splunkListAlertsTool{mgr: mgr, conns: conns})
	strictInject(reg, &splunkListDashboardsTool{mgr: mgr, conns: conns})
	return nil
}
