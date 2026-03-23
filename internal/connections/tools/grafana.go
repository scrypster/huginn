package conntools

import (
	"bytes"
	"context"
	"encoding/json"
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

// grafanaDo performs an authenticated Grafana API request.
func grafanaDo(ctx context.Context, method, baseURL, token, path string, body []byte) (string, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, bodyReader)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
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

// grafanaCreds extracts base URL and token from a connection.
func grafanaCreds(mgr *connections.Manager, conn connections.Connection) (baseURL, token string, err error) {
	creds, err := mgr.GetCredentials(conn.ID)
	if err != nil {
		return "", "", err
	}
	baseURL = conn.Metadata["url"]
	if baseURL == "" {
		return "", "", fmt.Errorf("grafana: url not configured")
	}
	// Normalize: strip trailing slash
	baseURL = strings.TrimRight(baseURL, "/")
	return baseURL, creds["token"], nil
}

// --- grafana_list_dashboards ---

type grafanaListDashboardsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *grafanaListDashboardsTool) Name() string        { return "grafana_list_dashboards" }
func (t *grafanaListDashboardsTool) Description() string { return "List all Grafana dashboards." }
func (t *grafanaListDashboardsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *grafanaListDashboardsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "grafana_list_dashboards",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"query": {Type: "string", Description: "Search query to filter dashboards by title"},
					"limit": {Type: "integer", Description: "Maximum number of results (default 50)"},
				},
			},
		},
	}
}
func (t *grafanaListDashboardsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, token, err := grafanaCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "grafana_list_dashboards: auth: " + err.Error()}
	}
	path := "/api/search?type=dash-db"
	if q, ok := args["query"].(string); ok && q != "" {
		path += "&query=" + url.QueryEscape(q)
	}
	limit := 50
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}
	path += fmt.Sprintf("&limit=%d", limit)
	out, err := grafanaDo(ctx, "GET", baseURL, token, path, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- grafana_get_dashboard ---

type grafanaGetDashboardTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *grafanaGetDashboardTool) Name() string        { return "grafana_get_dashboard" }
func (t *grafanaGetDashboardTool) Description() string { return "Get a specific Grafana dashboard by UID." }
func (t *grafanaGetDashboardTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *grafanaGetDashboardTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "grafana_get_dashboard",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"uid"},
				Properties: map[string]backend.ToolProperty{
					"uid": {Type: "string", Description: "The dashboard UID"},
				},
			},
		},
	}
}
func (t *grafanaGetDashboardTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, token, err := grafanaCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "grafana_get_dashboard: auth: " + err.Error()}
	}
	uid, _ := args["uid"].(string)
	if uid == "" {
		return tools.ToolResult{IsError: true, Error: "grafana_get_dashboard: uid is required"}
	}
	path := fmt.Sprintf("/api/dashboards/uid/%s", uid)
	out, err := grafanaDo(ctx, "GET", baseURL, token, path, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- grafana_list_alert_rules ---

type grafanaListAlertRulesTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *grafanaListAlertRulesTool) Name() string        { return "grafana_list_alert_rules" }
func (t *grafanaListAlertRulesTool) Description() string { return "List Grafana alert rules." }
func (t *grafanaListAlertRulesTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *grafanaListAlertRulesTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "grafana_list_alert_rules",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"state": {Type: "string", Description: "Filter by alert state: alerting, ok, pending, paused, no_data"},
				},
			},
		},
	}
}
func (t *grafanaListAlertRulesTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, token, err := grafanaCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "grafana_list_alert_rules: auth: " + err.Error()}
	}
	path := "/api/v1/provisioning/alert-rules"
	out, err := grafanaDo(ctx, "GET", baseURL, token, path, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- grafana_list_datasources ---

type grafanaListDataSourcesTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *grafanaListDataSourcesTool) Name() string        { return "grafana_list_datasources" }
func (t *grafanaListDataSourcesTool) Description() string { return "List all Grafana data sources." }
func (t *grafanaListDataSourcesTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *grafanaListDataSourcesTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "grafana_list_datasources",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:       "object",
				Properties: map[string]backend.ToolProperty{},
			},
		},
	}
}
func (t *grafanaListDataSourcesTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, token, err := grafanaCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "grafana_list_datasources: auth: " + err.Error()}
	}
	out, err := grafanaDo(ctx, "GET", baseURL, token, "/api/datasources", nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- grafana_create_annotation ---

type grafanaCreateAnnotationTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *grafanaCreateAnnotationTool) Name() string        { return "grafana_create_annotation" }
func (t *grafanaCreateAnnotationTool) Description() string { return "Create a Grafana annotation." }
func (t *grafanaCreateAnnotationTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *grafanaCreateAnnotationTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "grafana_create_annotation",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"text"},
				Properties: map[string]backend.ToolProperty{
					"text":          {Type: "string", Description: "Annotation text"},
					"tags":          {Type: "array", Description: "Array of tags to apply to the annotation"},
					"dashboard_uid": {Type: "string", Description: "Dashboard UID"},
					"time":          {Type: "integer", Description: "Annotation time (Unix milliseconds)"},
				},
			},
		},
	}
}
func (t *grafanaCreateAnnotationTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, token, err := grafanaCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "grafana_create_annotation: auth: " + err.Error()}
	}
	text, _ := args["text"].(string)
	if text == "" {
		return tools.ToolResult{IsError: true, Error: "grafana_create_annotation: text is required"}
	}

	body := map[string]any{"text": text}

	// Add optional fields if provided
	var tagList []string
	if tagsRaw, ok := args["tags"].([]any); ok {
		for _, t := range tagsRaw {
			if s, ok := t.(string); ok && s != "" {
				tagList = append(tagList, s)
			}
		}
	}
	if len(tagList) > 0 {
		body["tags"] = tagList
	}
	if uid, ok := args["dashboard_uid"].(string); ok && uid != "" {
		body["dashboardUID"] = uid
	}
	if timeVal, ok := args["time"].(float64); ok && timeVal > 0 {
		body["time"] = int64(timeVal)
	}

	bodyBytes, _ := json.Marshal(body)
	out, err := grafanaDo(ctx, "POST", baseURL, token, "/api/annotations", bodyBytes)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerGrafanaTools registers all Grafana tools.
func registerGrafanaTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{
		"grafana_list_dashboards", "grafana_get_dashboard", "grafana_list_datasources",
		"grafana_list_alert_rules", "grafana_create_annotation",
	}
	for _, n := range names {
		reg.Unregister(n)
	}
	strictInject(reg, &grafanaListDashboardsTool{mgr: mgr, conns: conns})
	strictInject(reg, &grafanaGetDashboardTool{mgr: mgr, conns: conns})
	strictInject(reg, &grafanaListDataSourcesTool{mgr: mgr, conns: conns})
	strictInject(reg, &grafanaListAlertRulesTool{mgr: mgr, conns: conns})
	strictInject(reg, &grafanaCreateAnnotationTool{mgr: mgr, conns: conns})
	return nil
}
