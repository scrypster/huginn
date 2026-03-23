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

// servicenowDo performs an authenticated ServiceNow API request with Basic Auth.
func servicenowDo(ctx context.Context, method, apiURL, username, password string, body io.Reader) (string, error) {
	req, err := http.NewRequestWithContext(ctx, method, apiURL, body)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(username, password)
	req.Header.Set("Accept", "application/json")
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

// servicenowCreds extracts base URL, username, and password from a connection.
// The handler stores instance_url and username in metadata, password in credentials.
func servicenowCreds(mgr *connections.Manager, conn connections.Connection) (baseURL, username, password string, err error) {
	creds, err := mgr.GetCredentials(conn.ID)
	if err != nil {
		return "", "", "", err
	}
	baseURL = conn.Metadata["instance_url"]
	if baseURL == "" {
		return "", "", "", fmt.Errorf("servicenow: instance_url not configured")
	}
	// Normalize: strip trailing slash
	baseURL = strings.TrimRight(baseURL, "/")
	username = conn.Metadata["username"]
	return baseURL, username, creds["password"], nil
}

// --- servicenow_list_incidents ---

type servicenowListIncidentsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *servicenowListIncidentsTool) Name() string {
	return "servicenow_list_incidents"
}
func (t *servicenowListIncidentsTool) Description() string {
	return "List ServiceNow incidents, optionally filtered by state."
}
func (t *servicenowListIncidentsTool) Permission() tools.PermissionLevel {
	return tools.PermRead
}
func (t *servicenowListIncidentsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "servicenow_list_incidents",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"limit": {Type: "integer", Description: "Maximum number of results (default 20)"},
					"query": {Type: "string", Description: "Encoded query string (sysparm_query)"},
				},
			},
		},
	}
}
func (t *servicenowListIncidentsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, username, password, err := servicenowCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "servicenow_list_incidents: auth: " + err.Error()}
	}

	limit := int(floatArg(args, "limit"))
	if limit <= 0 {
		limit = 20
	}

	apiURL := baseURL + "/api/now/table/incident?sysparm_limit=" + fmt.Sprintf("%d", limit)
	if query, ok := args["query"].(string); ok && query != "" {
		apiURL += "&sysparm_query=" + url.QueryEscape(query)
	}

	out, err := servicenowDo(ctx, "GET", apiURL, username, password, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- servicenow_get_incident ---

type servicenowGetIncidentTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *servicenowGetIncidentTool) Name() string {
	return "servicenow_get_incident"
}
func (t *servicenowGetIncidentTool) Description() string {
	return "Get a specific ServiceNow incident by sys_id."
}
func (t *servicenowGetIncidentTool) Permission() tools.PermissionLevel {
	return tools.PermRead
}
func (t *servicenowGetIncidentTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "servicenow_get_incident",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"sys_id"},
				Properties: map[string]backend.ToolProperty{
					"sys_id": {Type: "string", Description: "The incident sys_id"},
				},
			},
		},
	}
}
func (t *servicenowGetIncidentTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, username, password, err := servicenowCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "servicenow_get_incident: auth: " + err.Error()}
	}

	sysID, _ := args["sys_id"].(string)
	if sysID == "" {
		return tools.ToolResult{IsError: true, Error: "servicenow_get_incident: sys_id is required"}
	}

	apiURL := baseURL + "/api/now/table/incident/" + url.PathEscape(sysID)
	out, err := servicenowDo(ctx, "GET", apiURL, username, password, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- servicenow_create_incident ---

type servicenowCreateIncidentTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *servicenowCreateIncidentTool) Name() string {
	return "servicenow_create_incident"
}
func (t *servicenowCreateIncidentTool) Description() string {
	return "Create a new ServiceNow incident. Requires user approval."
}
func (t *servicenowCreateIncidentTool) Permission() tools.PermissionLevel {
	return tools.PermWrite
}
func (t *servicenowCreateIncidentTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "servicenow_create_incident",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"short_description"},
				Properties: map[string]backend.ToolProperty{
					"short_description": {Type: "string", Description: "Brief incident description"},
					"caller_id":         {Type: "string", Description: "User ID of the incident caller"},
					"urgency":           {Type: "string", Description: "Urgency level (1=High, 2=Medium, 3=Low)"},
					"description":       {Type: "string", Description: "Detailed incident description"},
				},
			},
		},
	}
}
func (t *servicenowCreateIncidentTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, username, password, err := servicenowCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "servicenow_create_incident: auth: " + err.Error()}
	}

	shortDesc, _ := args["short_description"].(string)
	if shortDesc == "" {
		return tools.ToolResult{IsError: true, Error: "servicenow_create_incident: short_description is required"}
	}

	payload := map[string]any{
		"short_description": shortDesc,
	}
	if callerID, ok := args["caller_id"].(string); ok && callerID != "" {
		payload["caller_id"] = callerID
	}
	if urgency, ok := args["urgency"].(string); ok && urgency != "" {
		payload["urgency"] = urgency
	}
	if description, ok := args["description"].(string); ok && description != "" {
		payload["description"] = description
	}

	bodyBytes, _ := json.Marshal(payload)
	apiURL := baseURL + "/api/now/table/incident"
	out, err := servicenowDo(ctx, "POST", apiURL, username, password, bytes.NewReader(bodyBytes))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- servicenow_update_incident ---

type servicenowUpdateIncidentTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *servicenowUpdateIncidentTool) Name() string {
	return "servicenow_update_incident"
}
func (t *servicenowUpdateIncidentTool) Description() string {
	return "Update a ServiceNow incident. Requires user approval."
}
func (t *servicenowUpdateIncidentTool) Permission() tools.PermissionLevel {
	return tools.PermWrite
}
func (t *servicenowUpdateIncidentTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "servicenow_update_incident",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"sys_id", "fields"},
				Properties: map[string]backend.ToolProperty{
					"sys_id": {Type: "string", Description: "The incident sys_id"},
					"fields": {Type: "object", Description: "Fields to update on the incident"},
				},
			},
		},
	}
}
func (t *servicenowUpdateIncidentTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, username, password, err := servicenowCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "servicenow_update_incident: auth: " + err.Error()}
	}

	sysID, _ := args["sys_id"].(string)
	if sysID == "" {
		return tools.ToolResult{IsError: true, Error: "servicenow_update_incident: sys_id is required"}
	}

	fields, ok := args["fields"].(map[string]any)
	if !ok {
		return tools.ToolResult{IsError: true, Error: "servicenow_update_incident: fields must be an object"}
	}
	bodyBytes, _ := json.Marshal(fields)
	apiURL := baseURL + "/api/now/table/incident/" + url.PathEscape(sysID)
	out, err := servicenowDo(ctx, "PATCH", apiURL, username, password, bytes.NewReader(bodyBytes))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- servicenow_search_records ---

type servicenowSearchRecordsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *servicenowSearchRecordsTool) Name() string {
	return "servicenow_search_records"
}
func (t *servicenowSearchRecordsTool) Description() string {
	return "Search ServiceNow records in a table with a query filter."
}
func (t *servicenowSearchRecordsTool) Permission() tools.PermissionLevel {
	return tools.PermRead
}
func (t *servicenowSearchRecordsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "servicenow_search_records",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"table"},
				Properties: map[string]backend.ToolProperty{
					"table":  {Type: "string", Description: "Table name (e.g., incident, change_request)"},
					"query":  {Type: "string", Description: "ServiceNow query filter (sysparm_query format)"},
					"limit":  {Type: "integer", Description: "Maximum number of results (default 20)"},
					"fields": {Type: "string", Description: "Comma-separated list of fields to return"},
				},
			},
		},
	}
}
func (t *servicenowSearchRecordsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, username, password, err := servicenowCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "servicenow_search_records: auth: " + err.Error()}
	}

	table, _ := args["table"].(string)
	query, _ := args["query"].(string)
	if table == "" {
		return tools.ToolResult{IsError: true, Error: "servicenow_search_records: table is required"}
	}

	limit := int(floatArg(args, "limit"))
	if limit <= 0 {
		limit = 20
	}

	apiURL := fmt.Sprintf("%s/api/now/table/%s?sysparm_limit=%d",
		baseURL, url.PathEscape(table), limit)
	if query != "" {
		apiURL += "&sysparm_query=" + url.QueryEscape(query)
	}
	if fields, ok := args["fields"].(string); ok && fields != "" {
		apiURL += "&sysparm_fields=" + url.QueryEscape(fields)
	}

	out, err := servicenowDo(ctx, "GET", apiURL, username, password, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerServiceNowTools registers all 5 ServiceNow tools.
func registerServiceNowTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{
		"servicenow_list_incidents", "servicenow_get_incident", "servicenow_create_incident",
		"servicenow_update_incident", "servicenow_search_records",
	}
	for _, n := range names {
		reg.Unregister(n)
	}
	strictInject(reg, &servicenowListIncidentsTool{mgr: mgr, conns: conns})
	strictInject(reg, &servicenowGetIncidentTool{mgr: mgr, conns: conns})
	strictInject(reg, &servicenowCreateIncidentTool{mgr: mgr, conns: conns})
	strictInject(reg, &servicenowUpdateIncidentTool{mgr: mgr, conns: conns})
	strictInject(reg, &servicenowSearchRecordsTool{mgr: mgr, conns: conns})
	return nil
}
