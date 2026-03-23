package conntools

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
	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/tools"
)

// datadogDo performs an HTTP request against the Datadog API with API/App key auth.
func datadogDo(ctx context.Context, method, apiURL, apiKey, appKey string, body io.Reader) (string, error) {
	req, err := http.NewRequestWithContext(ctx, method, apiURL, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("DD-API-KEY", apiKey)
	req.Header.Set("DD-APPLICATION-KEY", appKey)
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

// datadogCreds extracts the base URL and credentials for a Datadog connection.
func datadogCreds(mgr *connections.Manager, conn connections.Connection) (baseURL, apiKey, appKey string, err error) {
	creds, err := mgr.GetCredentials(conn.ID)
	if err != nil {
		return "", "", "", err
	}
	baseURL = conn.Metadata["url"]
	if baseURL == "" {
		baseURL = "https://api.datadoghq.com"
	}
	return baseURL, creds["api_key"], creds["app_key"], nil
}

// --- datadog_query_metrics ---

type datadogQueryMetricsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *datadogQueryMetricsTool) Name() string        { return "datadog_query_metrics" }
func (t *datadogQueryMetricsTool) Description() string { return "Query Datadog metrics using the metrics query API." }
func (t *datadogQueryMetricsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *datadogQueryMetricsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "datadog_query_metrics",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"query", "from", "to"},
				Properties: map[string]backend.ToolProperty{
					"query": {Type: "string", Description: "Metrics query string"},
					"from":  {Type: "integer", Description: "Start of query window (Unix epoch seconds)"},
					"to":    {Type: "integer", Description: "End of query window (Unix epoch seconds)"},
				},
			},
		},
	}
}
func (t *datadogQueryMetricsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, apiKey, appKey, err := datadogCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "datadog_query_metrics: auth: " + err.Error()}
	}
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return tools.ToolResult{IsError: true, Error: "datadog_query_metrics: query parameter required"}
	}
	from := int64(floatArg(args, "from"))
	to := int64(floatArg(args, "to"))
	params := url.Values{}
	params.Set("query", query)
	params.Set("from", fmt.Sprintf("%d", from))
	params.Set("to", fmt.Sprintf("%d", to))
	apiURL := fmt.Sprintf("%s/api/v1/query?%s", baseURL, params.Encode())
	out, err := datadogDo(ctx, "GET", apiURL, apiKey, appKey, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- datadog_search_logs ---

type datadogSearchLogsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *datadogSearchLogsTool) Name() string        { return "datadog_search_logs" }
func (t *datadogSearchLogsTool) Description() string { return "Search Datadog logs using the logs search API." }
func (t *datadogSearchLogsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *datadogSearchLogsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "datadog_search_logs",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"query"},
				Properties: map[string]backend.ToolProperty{
					"query":  {Type: "string", Description: "Log search query"},
					"limit":  {Type: "integer", Description: "Max logs to return (default 50)"},
					"cursor": {Type: "string", Description: "Pagination cursor from previous response"},
				},
			},
		},
	}
}
func (t *datadogSearchLogsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, apiKey, appKey, err := datadogCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "datadog_search_logs: auth: " + err.Error()}
	}
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return tools.ToolResult{IsError: true, Error: "datadog_search_logs: query parameter required"}
	}
	limit := int(floatArg(args, "limit"))
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	page := map[string]any{"limit": limit}
	if cursor, ok := args["cursor"].(string); ok && cursor != "" {
		page["cursor"] = cursor
	}
	bodyData := map[string]any{
		"filter": map[string]any{"query": query},
		"page":   page,
	}
	bodyBytes, _ := json.Marshal(bodyData)
	apiURL := fmt.Sprintf("%s/api/v2/logs/events/search", baseURL)
	out, err := datadogDo(ctx, "POST", apiURL, apiKey, appKey, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- datadog_list_monitors ---

type datadogListMonitorsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *datadogListMonitorsTool) Name() string        { return "datadog_list_monitors" }
func (t *datadogListMonitorsTool) Description() string { return "List Datadog monitors, optionally filtered by name or tags." }
func (t *datadogListMonitorsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *datadogListMonitorsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "datadog_list_monitors",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"name":      {Type: "string", Description: "Filter monitors by name"},
					"tags":      {Type: "string", Description: "Comma-separated monitor tags to filter by"},
					"page_size": {Type: "integer", Description: "Max monitors to return (default 100)"},
				},
			},
		},
	}
}
func (t *datadogListMonitorsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, apiKey, appKey, err := datadogCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "datadog_list_monitors: auth: " + err.Error()}
	}
	params := url.Values{}
	if name, ok := args["name"].(string); ok && name != "" {
		params.Set("name", name)
	}
	if tags, ok := args["tags"].(string); ok && tags != "" {
		params.Set("monitor_tags", tags)
	}
	pageSize := int(floatArg(args, "page_size"))
	if pageSize <= 0 {
		pageSize = 100
	}
	params.Set("page_size", fmt.Sprintf("%d", pageSize))
	apiURL := fmt.Sprintf("%s/api/v1/monitor?%s", baseURL, params.Encode())
	out, err := datadogDo(ctx, "GET", apiURL, apiKey, appKey, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- datadog_get_monitor ---

type datadogGetMonitorTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *datadogGetMonitorTool) Name() string        { return "datadog_get_monitor" }
func (t *datadogGetMonitorTool) Description() string { return "Get a single Datadog monitor by ID." }
func (t *datadogGetMonitorTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *datadogGetMonitorTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "datadog_get_monitor",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"monitor_id"},
				Properties: map[string]backend.ToolProperty{
					"monitor_id": {Type: "integer", Description: "Monitor ID"},
				},
			},
		},
	}
}
func (t *datadogGetMonitorTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, apiKey, appKey, err := datadogCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "datadog_get_monitor: auth: " + err.Error()}
	}
	monitorID := int64(floatArg(args, "monitor_id"))
	apiURL := fmt.Sprintf("%s/api/v1/monitor/%d", baseURL, monitorID)
	out, err := datadogDo(ctx, "GET", apiURL, apiKey, appKey, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- datadog_create_monitor ---

type datadogCreateMonitorTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *datadogCreateMonitorTool) Name() string        { return "datadog_create_monitor" }
func (t *datadogCreateMonitorTool) Description() string { return "Create a new Datadog monitor. Requires user approval." }
func (t *datadogCreateMonitorTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *datadogCreateMonitorTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "datadog_create_monitor",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"monitor"},
				Properties: map[string]backend.ToolProperty{
					"monitor": {Type: "object", Description: "Monitor definition object (see Datadog monitor API docs)"},
				},
			},
		},
	}
}
func (t *datadogCreateMonitorTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, apiKey, appKey, err := datadogCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "datadog_create_monitor: auth: " + err.Error()}
	}
	monitor, ok := args["monitor"]
	if !ok {
		return tools.ToolResult{IsError: true, Error: "datadog_create_monitor: monitor object required"}
	}
	bodyBytes, err := json.Marshal(monitor)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "datadog_create_monitor: marshal: " + err.Error()}
	}
	apiURL := fmt.Sprintf("%s/api/v1/monitor", baseURL)
	out, err := datadogDo(ctx, "POST", apiURL, apiKey, appKey, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- datadog_update_monitor ---

type datadogUpdateMonitorTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *datadogUpdateMonitorTool) Name() string        { return "datadog_update_monitor" }
func (t *datadogUpdateMonitorTool) Description() string { return "Update an existing Datadog monitor. Requires user approval." }
func (t *datadogUpdateMonitorTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *datadogUpdateMonitorTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "datadog_update_monitor",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"monitor_id", "updates"},
				Properties: map[string]backend.ToolProperty{
					"monitor_id": {Type: "integer", Description: "Monitor ID to update"},
					"updates":    {Type: "object", Description: "Fields to update on the monitor"},
				},
			},
		},
	}
}
func (t *datadogUpdateMonitorTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, apiKey, appKey, err := datadogCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "datadog_update_monitor: auth: " + err.Error()}
	}
	monitorID := int64(floatArg(args, "monitor_id"))
	updates, ok := args["updates"]
	if !ok {
		return tools.ToolResult{IsError: true, Error: "datadog_update_monitor: updates object required"}
	}
	bodyBytes, err := json.Marshal(updates)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "datadog_update_monitor: marshal: " + err.Error()}
	}
	apiURL := fmt.Sprintf("%s/api/v1/monitor/%d", baseURL, monitorID)
	out, err := datadogDo(ctx, "PUT", apiURL, apiKey, appKey, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- datadog_mute_monitor ---

type datadogMuteMonitorTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *datadogMuteMonitorTool) Name() string        { return "datadog_mute_monitor" }
func (t *datadogMuteMonitorTool) Description() string { return "Mute a Datadog monitor. Requires user approval." }
func (t *datadogMuteMonitorTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *datadogMuteMonitorTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "datadog_mute_monitor",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"monitor_id"},
				Properties: map[string]backend.ToolProperty{
					"monitor_id": {Type: "integer", Description: "Monitor ID to mute"},
					"end":        {Type: "integer", Description: "Unix epoch timestamp when the mute should end (optional)"},
				},
			},
		},
	}
}
func (t *datadogMuteMonitorTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, apiKey, appKey, err := datadogCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "datadog_mute_monitor: auth: " + err.Error()}
	}
	monitorID := int64(floatArg(args, "monitor_id"))
	var bodyReader io.Reader
	if end := int64(floatArg(args, "end")); end > 0 {
		bodyData := map[string]any{"end": end}
		bodyBytes, _ := json.Marshal(bodyData)
		bodyReader = strings.NewReader(string(bodyBytes))
	}
	apiURL := fmt.Sprintf("%s/api/v1/monitor/%d/mute", baseURL, monitorID)
	out, err := datadogDo(ctx, "POST", apiURL, apiKey, appKey, bodyReader)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- datadog_list_dashboards ---

type datadogListDashboardsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *datadogListDashboardsTool) Name() string        { return "datadog_list_dashboards" }
func (t *datadogListDashboardsTool) Description() string { return "List all Datadog dashboards." }
func (t *datadogListDashboardsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *datadogListDashboardsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "datadog_list_dashboards",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:       "object",
				Properties: map[string]backend.ToolProperty{},
			},
		},
	}
}
func (t *datadogListDashboardsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, apiKey, appKey, err := datadogCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "datadog_list_dashboards: auth: " + err.Error()}
	}
	apiURL := fmt.Sprintf("%s/api/v1/dashboard", baseURL)
	out, err := datadogDo(ctx, "GET", apiURL, apiKey, appKey, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- datadog_query_events ---

type datadogQueryEventsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *datadogQueryEventsTool) Name() string        { return "datadog_query_events" }
func (t *datadogQueryEventsTool) Description() string { return "Query Datadog events within a time range." }
func (t *datadogQueryEventsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *datadogQueryEventsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "datadog_query_events",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"start", "end"},
				Properties: map[string]backend.ToolProperty{
					"start":    {Type: "integer", Description: "Start of query window (Unix epoch seconds)"},
					"end":      {Type: "integer", Description: "End of query window (Unix epoch seconds)"},
					"tags":     {Type: "string", Description: "Comma-separated list of tags to filter events"},
					"sources":  {Type: "string", Description: "Comma-separated list of sources to filter events"},
					"priority": {Type: "string", Description: "Event priority filter (normal or low)"},
				},
			},
		},
	}
}
func (t *datadogQueryEventsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, apiKey, appKey, err := datadogCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "datadog_query_events: auth: " + err.Error()}
	}
	start := int64(floatArg(args, "start"))
	end := int64(floatArg(args, "end"))
	params := url.Values{}
	params.Set("start", fmt.Sprintf("%d", start))
	params.Set("end", fmt.Sprintf("%d", end))
	if tags, ok := args["tags"].(string); ok && tags != "" {
		params.Set("tags", tags)
	}
	if sources, ok := args["sources"].(string); ok && sources != "" {
		params.Set("sources", sources)
	}
	if priority, ok := args["priority"].(string); ok && priority != "" {
		params.Set("priority", priority)
	}
	apiURL := fmt.Sprintf("%s/api/v1/events?%s", baseURL, params.Encode())
	out, err := datadogDo(ctx, "GET", apiURL, apiKey, appKey, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- datadog_list_hosts ---

type datadogListHostsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *datadogListHostsTool) Name() string        { return "datadog_list_hosts" }
func (t *datadogListHostsTool) Description() string { return "List hosts reporting to Datadog." }
func (t *datadogListHostsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *datadogListHostsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "datadog_list_hosts",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"filter": {Type: "string", Description: "Filter string to search hosts by name or tags"},
					"count":  {Type: "integer", Description: "Max number of hosts to return (default 100)"},
					"start":  {Type: "integer", Description: "Starting offset for pagination"},
				},
			},
		},
	}
}
func (t *datadogListHostsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, apiKey, appKey, err := datadogCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "datadog_list_hosts: auth: " + err.Error()}
	}
	params := url.Values{}
	if filter, ok := args["filter"].(string); ok && filter != "" {
		params.Set("filter", filter)
	}
	count := int(floatArg(args, "count"))
	if count <= 0 {
		count = 100
	}
	params.Set("count", fmt.Sprintf("%d", count))
	if start := int64(floatArg(args, "start")); start > 0 {
		params.Set("start", fmt.Sprintf("%d", start))
	}
	apiURL := fmt.Sprintf("%s/api/v1/hosts?%s", baseURL, params.Encode())
	out, err := datadogDo(ctx, "GET", apiURL, apiKey, appKey, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- datadog_list_slos ---

type datadogListSLOsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *datadogListSLOsTool) Name() string        { return "datadog_list_slos" }
func (t *datadogListSLOsTool) Description() string { return "List Datadog Service Level Objectives (SLOs)." }
func (t *datadogListSLOsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *datadogListSLOsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "datadog_list_slos",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"query":       {Type: "string", Description: "Text query to filter SLOs by name"},
					"tags_query":  {Type: "string", Description: "Comma-separated tags to filter SLOs"},
					"limit":       {Type: "integer", Description: "Max SLOs to return (default 100)"},
				},
			},
		},
	}
}
func (t *datadogListSLOsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, apiKey, appKey, err := datadogCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "datadog_list_slos: auth: " + err.Error()}
	}
	params := url.Values{}
	if query, ok := args["query"].(string); ok && query != "" {
		params.Set("query", query)
	}
	if tagsQuery, ok := args["tags_query"].(string); ok && tagsQuery != "" {
		params.Set("tags_query", tagsQuery)
	}
	limit := int(floatArg(args, "limit"))
	if limit <= 0 {
		limit = 100
	}
	params.Set("limit", fmt.Sprintf("%d", limit))
	apiURL := fmt.Sprintf("%s/api/v1/slo?%s", baseURL, params.Encode())
	out, err := datadogDo(ctx, "GET", apiURL, apiKey, appKey, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerDatadogTools registers all 11 Datadog tools.
func registerDatadogTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{
		"datadog_query_metrics", "datadog_search_logs", "datadog_list_monitors",
		"datadog_get_monitor", "datadog_create_monitor", "datadog_update_monitor",
		"datadog_mute_monitor", "datadog_list_dashboards", "datadog_query_events",
		"datadog_list_hosts", "datadog_list_slos",
	}
	for _, n := range names {
		reg.Unregister(n)
	}
	strictInject(reg, &datadogQueryMetricsTool{mgr: mgr, conns: conns})
	strictInject(reg, &datadogSearchLogsTool{mgr: mgr, conns: conns})
	strictInject(reg, &datadogListMonitorsTool{mgr: mgr, conns: conns})
	strictInject(reg, &datadogGetMonitorTool{mgr: mgr, conns: conns})
	strictInject(reg, &datadogCreateMonitorTool{mgr: mgr, conns: conns})
	strictInject(reg, &datadogUpdateMonitorTool{mgr: mgr, conns: conns})
	strictInject(reg, &datadogMuteMonitorTool{mgr: mgr, conns: conns})
	strictInject(reg, &datadogListDashboardsTool{mgr: mgr, conns: conns})
	strictInject(reg, &datadogQueryEventsTool{mgr: mgr, conns: conns})
	strictInject(reg, &datadogListHostsTool{mgr: mgr, conns: conns})
	strictInject(reg, &datadogListSLOsTool{mgr: mgr, conns: conns})
	return nil
}
