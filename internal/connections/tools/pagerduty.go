package conntools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/connections"
	"github.com/scrypster/huginn/internal/tools"
)

// pagerdutyDo performs an authenticated PagerDuty API request.
func pagerdutyDo(ctx context.Context, method, apiURL, apiToken string, body io.Reader) (string, error) {
	req, err := http.NewRequestWithContext(ctx, method, apiURL, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Token token="+apiToken)
	req.Header.Set("Accept", "application/vnd.pagerduty+json;version=2")
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

// pagerdutyCreds extracts the API token from a connection.
func pagerdutyCreds(mgr *connections.Manager, conn connections.Connection) (apiToken string, err error) {
	creds, err := mgr.GetCredentials(conn.ID)
	if err != nil {
		return "", err
	}
	return creds["api_token"], nil
}

// --- pagerduty_list_incidents ---

type pagerdutyListIncidentsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *pagerdutyListIncidentsTool) Name() string        { return "pagerduty_list_incidents" }
func (t *pagerdutyListIncidentsTool) Description() string { return "List PagerDuty incidents, optionally filtered by status." }
func (t *pagerdutyListIncidentsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *pagerdutyListIncidentsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "pagerduty_list_incidents",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"statuses": {Type: "string", Description: "Comma-separated statuses to filter: triggered, acknowledged, resolved (default: triggered,acknowledged)"},
					"limit":    {Type: "integer", Description: "Maximum number of results (default 25)"},
				},
			},
		},
	}
}
func (t *pagerdutyListIncidentsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	apiToken, err := pagerdutyCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "pagerduty_list_incidents: auth: " + err.Error()}
	}

	statuses := "triggered,acknowledged"
	if s, ok := args["statuses"].(string); ok && s != "" {
		statuses = s
	}

	limit := int(floatArg(args, "limit"))
	if limit <= 0 {
		limit = 25
	}

	// Build query string with multi-value statuses parameter
	params := url.Values{}
	for _, status := range strings.Split(statuses, ",") {
		status = strings.TrimSpace(status)
		if status != "" {
			params.Add("statuses[]", status)
		}
	}
	params.Set("limit", strconv.Itoa(limit))
	apiURL := "https://api.pagerduty.com/incidents?" + params.Encode()
	out, err := pagerdutyDo(ctx, "GET", apiURL, apiToken, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- pagerduty_get_incident ---

type pagerdutyGetIncidentTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *pagerdutyGetIncidentTool) Name() string        { return "pagerduty_get_incident" }
func (t *pagerdutyGetIncidentTool) Description() string { return "Get details of a specific PagerDuty incident by ID." }
func (t *pagerdutyGetIncidentTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *pagerdutyGetIncidentTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "pagerduty_get_incident",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"incident_id"},
				Properties: map[string]backend.ToolProperty{
					"incident_id": {Type: "string", Description: "The PagerDuty incident ID"},
				},
			},
		},
	}
}
func (t *pagerdutyGetIncidentTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	apiToken, err := pagerdutyCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "pagerduty_get_incident: auth: " + err.Error()}
	}
	id, ok := args["incident_id"].(string)
	if !ok || id == "" {
		return tools.ToolResult{IsError: true, Error: "pagerduty_get_incident: incident_id is required"}
	}
	apiURL := fmt.Sprintf("https://api.pagerduty.com/incidents/%s", id)
	out, err := pagerdutyDo(ctx, "GET", apiURL, apiToken, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- pagerduty_create_incident ---

type pagerdutyCreateIncidentTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *pagerdutyCreateIncidentTool) Name() string        { return "pagerduty_create_incident" }
func (t *pagerdutyCreateIncidentTool) Description() string { return "Create a new PagerDuty incident. Requires user approval." }
func (t *pagerdutyCreateIncidentTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *pagerdutyCreateIncidentTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "pagerduty_create_incident",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"title", "service_id"},
				Properties: map[string]backend.ToolProperty{
					"title":      {Type: "string", Description: "Incident title/summary"},
					"service_id": {Type: "string", Description: "The service ID this incident belongs to"},
					"urgency":    {Type: "string", Description: "Urgency: high or low (default: high)"},
					"body":       {Type: "string", Description: "Additional details for the incident body"},
				},
			},
		},
	}
}
func (t *pagerdutyCreateIncidentTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	apiToken, err := pagerdutyCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "pagerduty_create_incident: auth: " + err.Error()}
	}

	title, _ := args["title"].(string)
	serviceID, _ := args["service_id"].(string)
	if title == "" || serviceID == "" {
		return tools.ToolResult{IsError: true, Error: "pagerduty_create_incident: title and service_id are required"}
	}

	urgency, _ := args["urgency"].(string)
	if urgency == "" {
		urgency = "high"
	}

	bodyText, _ := args["body"].(string)

	incident := map[string]any{
		"type":    "incident",
		"title":   title,
		"urgency": urgency,
		"service": map[string]any{
			"id":   serviceID,
			"type": "service_reference",
		},
	}
	if bodyText != "" {
		incident["body"] = map[string]any{
			"type":    "incident_body",
			"details": bodyText,
		}
	}
	payload := map[string]any{
		"incident": incident,
	}

	bodyBytes, _ := json.Marshal(payload)
	apiURL := "https://api.pagerduty.com/incidents"
	out, err := pagerdutyDo(ctx, "POST", apiURL, apiToken, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- pagerduty_update_incident ---

type pagerdutyUpdateIncidentTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *pagerdutyUpdateIncidentTool) Name() string        { return "pagerduty_update_incident" }
func (t *pagerdutyUpdateIncidentTool) Description() string { return "Update a PagerDuty incident status. Requires user approval." }
func (t *pagerdutyUpdateIncidentTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *pagerdutyUpdateIncidentTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "pagerduty_update_incident",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"incident_id", "status"},
				Properties: map[string]backend.ToolProperty{
					"incident_id": {Type: "string", Description: "The incident ID to update"},
					"status":      {Type: "string", Description: "New status: acknowledged or resolved"},
				},
			},
		},
	}
}
func (t *pagerdutyUpdateIncidentTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	apiToken, err := pagerdutyCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "pagerduty_update_incident: auth: " + err.Error()}
	}

	incidentID, _ := args["incident_id"].(string)
	status, _ := args["status"].(string)
	if incidentID == "" || status == "" {
		return tools.ToolResult{IsError: true, Error: "pagerduty_update_incident: incident_id and status are required"}
	}

	payload := map[string]any{
		"incident": map[string]any{
			"type":   "incident",
			"status": status,
		},
	}

	bodyBytes, _ := json.Marshal(payload)
	apiURL := fmt.Sprintf("https://api.pagerduty.com/incidents/%s", incidentID)

	// Need to add From header for PagerDuty API
	req, err := http.NewRequestWithContext(ctx, "PUT", apiURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	req.Header.Set("Authorization", "Token token="+apiToken)
	req.Header.Set("Accept", "application/vnd.pagerduty+json;version=2")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("From", "noreply@huginn.ai")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Errorf("network: %w", err).Error()}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, data)}
	}
	return tools.ToolResult{Output: string(data)}
}

// --- pagerduty_list_services ---

type pagerdutyListServicesTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *pagerdutyListServicesTool) Name() string        { return "pagerduty_list_services" }
func (t *pagerdutyListServicesTool) Description() string { return "List PagerDuty services." }
func (t *pagerdutyListServicesTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *pagerdutyListServicesTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "pagerduty_list_services",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"limit": {Type: "integer", Description: "Maximum number of results (default 25)"},
					"query": {Type: "string", Description: "Search query to filter services by name"},
				},
			},
		},
	}
}
func (t *pagerdutyListServicesTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	apiToken, err := pagerdutyCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "pagerduty_list_services: auth: " + err.Error()}
	}

	limit := int(floatArg(args, "limit"))
	if limit <= 0 {
		limit = 25
	}

	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	if q, ok := args["query"].(string); ok && q != "" {
		params.Set("query", q)
	}
	apiURL := "https://api.pagerduty.com/services?" + params.Encode()
	out, err := pagerdutyDo(ctx, "GET", apiURL, apiToken, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- pagerduty_list_on_calls ---

type pagerdutyListOnCallsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *pagerdutyListOnCallsTool) Name() string        { return "pagerduty_list_on_calls" }
func (t *pagerdutyListOnCallsTool) Description() string { return "List current on-call schedules showing who is on call." }
func (t *pagerdutyListOnCallsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *pagerdutyListOnCallsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "pagerduty_list_on_calls",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"schedule_ids": {Type: "string", Description: "Comma-separated schedule IDs to filter"},
					"limit":        {Type: "integer", Description: "Maximum number of results (default 25)"},
				},
			},
		},
	}
}
func (t *pagerdutyListOnCallsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	apiToken, err := pagerdutyCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "pagerduty_list_on_calls: auth: " + err.Error()}
	}

	params := url.Values{}
	if ids, ok := args["schedule_ids"].(string); ok && ids != "" {
		for _, id := range strings.Split(ids, ",") {
			id = strings.TrimSpace(id)
			if id != "" {
				params.Add("schedule_ids[]", id)
			}
		}
	}

	limit := int(floatArg(args, "limit"))
	if limit <= 0 {
		limit = 25
	}
	params.Set("limit", strconv.Itoa(limit))

	apiURL := "https://api.pagerduty.com/oncalls?" + params.Encode()
	out, err := pagerdutyDo(ctx, "GET", apiURL, apiToken, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerPagerDutyTools registers all 6 PagerDuty tools.
func registerPagerDutyTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{
		"pagerduty_list_incidents", "pagerduty_get_incident", "pagerduty_create_incident",
		"pagerduty_update_incident", "pagerduty_list_services", "pagerduty_list_on_calls",
	}
	for _, n := range names {
		reg.Unregister(n)
	}
	strictInject(reg, &pagerdutyListIncidentsTool{mgr: mgr, conns: conns})
	strictInject(reg, &pagerdutyGetIncidentTool{mgr: mgr, conns: conns})
	strictInject(reg, &pagerdutyCreateIncidentTool{mgr: mgr, conns: conns})
	strictInject(reg, &pagerdutyUpdateIncidentTool{mgr: mgr, conns: conns})
	strictInject(reg, &pagerdutyListServicesTool{mgr: mgr, conns: conns})
	strictInject(reg, &pagerdutyListOnCallsTool{mgr: mgr, conns: conns})
	return nil
}
