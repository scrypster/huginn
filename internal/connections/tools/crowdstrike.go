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

// crowdstrikeGetToken retrieves an OAuth2 access token from CrowdStrike.
func crowdstrikeGetToken(ctx context.Context, clientID, clientSecret string) (string, error) {
	body := strings.NewReader("client_id=" + url.QueryEscape(clientID) + "&client_secret=" + url.QueryEscape(clientSecret))
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.crowdstrike.com/oauth2/token", body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("network: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 201 {
		return "", fmt.Errorf("token HTTP %d: %s", resp.StatusCode, data)
	}
	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", err
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("crowdstrike: token response contained empty access_token")
	}
	return result.AccessToken, nil
}

// crowdstrikeDo performs an authenticated CrowdStrike API request with OAuth2.
func crowdstrikeDo(ctx context.Context, method, apiURL, clientID, clientSecret string, body io.Reader) (string, error) {
	token, err := crowdstrikeGetToken(ctx, clientID, clientSecret)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, method, apiURL, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
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

// crowdstrikeCreds extracts client ID and secret from a connection.
func crowdstrikeCreds(mgr *connections.Manager, conn connections.Connection) (clientID, clientSecret string, err error) {
	creds, err := mgr.GetCredentials(conn.ID)
	if err != nil {
		return "", "", err
	}
	return creds["client_id"], creds["client_secret"], nil
}

// --- crowdstrike_list_detections ---

type crowdstrikeListDetectionsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *crowdstrikeListDetectionsTool) Name() string {
	return "crowdstrike_list_detections"
}
func (t *crowdstrikeListDetectionsTool) Description() string {
	return "List CrowdStrike detections, optionally filtered."
}
func (t *crowdstrikeListDetectionsTool) Permission() tools.PermissionLevel {
	return tools.PermRead
}
func (t *crowdstrikeListDetectionsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "crowdstrike_list_detections",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"limit":  {Type: "integer", Description: "Maximum number of results (default 20)"},
					"filter": {Type: "string", Description: "FQL filter string"},
				},
			},
		},
	}
}
func (t *crowdstrikeListDetectionsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	clientID, clientSecret, err := crowdstrikeCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "crowdstrike_list_detections: auth: " + err.Error()}
	}

	limit := int(floatArg(args, "limit"))
	if limit <= 0 {
		limit = 20
	}

	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	if filter, ok := args["filter"].(string); ok && filter != "" {
		params.Set("filter", filter)
	}

	apiURL := "https://api.crowdstrike.com/detects/queries/detects/v1?" + params.Encode()
	out, err := crowdstrikeDo(ctx, "GET", apiURL, clientID, clientSecret, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- crowdstrike_get_detection ---

type crowdstrikeGetDetectionTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *crowdstrikeGetDetectionTool) Name() string {
	return "crowdstrike_get_detection"
}
func (t *crowdstrikeGetDetectionTool) Description() string {
	return "Get details of specific CrowdStrike detections by IDs."
}
func (t *crowdstrikeGetDetectionTool) Permission() tools.PermissionLevel {
	return tools.PermRead
}
func (t *crowdstrikeGetDetectionTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "crowdstrike_get_detection",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"detection_ids"},
				Properties: map[string]backend.ToolProperty{
					"detection_ids": {Type: "array", Description: "Array of detection IDs to fetch"},
				},
			},
		},
	}
}
func (t *crowdstrikeGetDetectionTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	clientID, clientSecret, err := crowdstrikeCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "crowdstrike_get_detection: auth: " + err.Error()}
	}

	raw, ok := args["detection_ids"]
	if !ok {
		return tools.ToolResult{IsError: true, Error: "crowdstrike_get_detection: detection_ids is required"}
	}

	var ids []string
	if rawList, ok := raw.([]any); ok {
		for _, v := range rawList {
			if s, ok := v.(string); ok {
				ids = append(ids, s)
			}
		}
	}
	if len(ids) == 0 {
		return tools.ToolResult{IsError: true, Error: "crowdstrike_get_detection: detection_ids must be non-empty array"}
	}

	payload := map[string]any{
		"ids": ids,
	}
	bodyBytes, _ := json.Marshal(payload)
	apiURL := "https://api.crowdstrike.com/detects/entities/summaries/GET/v1"
	out, err := crowdstrikeDo(ctx, "POST", apiURL, clientID, clientSecret, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- crowdstrike_list_incidents ---

type crowdstrikeListIncidentsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *crowdstrikeListIncidentsTool) Name() string {
	return "crowdstrike_list_incidents"
}
func (t *crowdstrikeListIncidentsTool) Description() string {
	return "List CrowdStrike incidents, optionally filtered."
}
func (t *crowdstrikeListIncidentsTool) Permission() tools.PermissionLevel {
	return tools.PermRead
}
func (t *crowdstrikeListIncidentsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "crowdstrike_list_incidents",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"limit":  {Type: "integer", Description: "Maximum number of results (default 20)"},
					"filter": {Type: "string", Description: "FQL filter string"},
				},
			},
		},
	}
}
func (t *crowdstrikeListIncidentsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	clientID, clientSecret, err := crowdstrikeCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "crowdstrike_list_incidents: auth: " + err.Error()}
	}

	limit := int(floatArg(args, "limit"))
	if limit <= 0 {
		limit = 20
	}

	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	if filter, ok := args["filter"].(string); ok && filter != "" {
		params.Set("filter", filter)
	}

	apiURL := "https://api.crowdstrike.com/incidents/queries/incidents/v1?" + params.Encode()
	out, err := crowdstrikeDo(ctx, "GET", apiURL, clientID, clientSecret, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- crowdstrike_list_hosts ---

type crowdstrikeListHostsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *crowdstrikeListHostsTool) Name() string {
	return "crowdstrike_list_hosts"
}
func (t *crowdstrikeListHostsTool) Description() string {
	return "List CrowdStrike hosts, optionally filtered."
}
func (t *crowdstrikeListHostsTool) Permission() tools.PermissionLevel {
	return tools.PermRead
}
func (t *crowdstrikeListHostsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "crowdstrike_list_hosts",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"limit":  {Type: "integer", Description: "Maximum number of results (default 20)"},
					"filter": {Type: "string", Description: "FQL filter string"},
				},
			},
		},
	}
}
func (t *crowdstrikeListHostsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	clientID, clientSecret, err := crowdstrikeCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "crowdstrike_list_hosts: auth: " + err.Error()}
	}

	limit := int(floatArg(args, "limit"))
	if limit <= 0 {
		limit = 20
	}

	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	if filter, ok := args["filter"].(string); ok && filter != "" {
		params.Set("filter", filter)
	}

	apiURL := "https://api.crowdstrike.com/devices/queries/devices/v1?" + params.Encode()
	out, err := crowdstrikeDo(ctx, "GET", apiURL, clientID, clientSecret, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- crowdstrike_get_host ---

type crowdstrikeGetHostTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *crowdstrikeGetHostTool) Name() string {
	return "crowdstrike_get_host"
}
func (t *crowdstrikeGetHostTool) Description() string {
	return "Get details of specific CrowdStrike hosts by device IDs."
}
func (t *crowdstrikeGetHostTool) Permission() tools.PermissionLevel {
	return tools.PermRead
}
func (t *crowdstrikeGetHostTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "crowdstrike_get_host",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"device_ids"},
				Properties: map[string]backend.ToolProperty{
					"device_ids": {Type: "array", Description: "Array of device IDs to fetch"},
				},
			},
		},
	}
}
func (t *crowdstrikeGetHostTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	clientID, clientSecret, err := crowdstrikeCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "crowdstrike_get_host: auth: " + err.Error()}
	}

	raw, ok := args["device_ids"]
	if !ok {
		return tools.ToolResult{IsError: true, Error: "crowdstrike_get_host: device_ids is required"}
	}

	var ids []string
	if rawList, ok := raw.([]any); ok {
		for _, v := range rawList {
			if s, ok := v.(string); ok {
				ids = append(ids, s)
			}
		}
	}
	if len(ids) == 0 {
		return tools.ToolResult{IsError: true, Error: "crowdstrike_get_host: device_ids must be non-empty array"}
	}

	params := url.Values{}
	for _, id := range ids {
		params.Add("ids", id)
	}

	apiURL := "https://api.crowdstrike.com/devices/entities/devices/v2?" + params.Encode()
	out, err := crowdstrikeDo(ctx, "GET", apiURL, clientID, clientSecret, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerCrowdStrikeTools registers all 5 CrowdStrike tools.
func registerCrowdStrikeTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{
		"crowdstrike_list_detections", "crowdstrike_get_detection", "crowdstrike_list_incidents",
		"crowdstrike_list_hosts", "crowdstrike_get_host",
	}
	for _, n := range names {
		reg.Unregister(n)
	}
	strictInject(reg, &crowdstrikeListDetectionsTool{mgr: mgr, conns: conns})
	strictInject(reg, &crowdstrikeGetDetectionTool{mgr: mgr, conns: conns})
	strictInject(reg, &crowdstrikeListIncidentsTool{mgr: mgr, conns: conns})
	strictInject(reg, &crowdstrikeListHostsTool{mgr: mgr, conns: conns})
	strictInject(reg, &crowdstrikeGetHostTool{mgr: mgr, conns: conns})
	return nil
}
