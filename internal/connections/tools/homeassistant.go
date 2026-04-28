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

var haDoFn = haDo

func haDo(ctx context.Context, method, apiURL, token string, body io.Reader) (string, error) {
	req, err := http.NewRequestWithContext(ctx, method, apiURL, body)
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
	if len(data) == 0 {
		return `{"ok": true}`, nil
	}
	return string(data), nil
}

func haCreds(mgr *connections.Manager, conn connections.Connection) (baseURL, token string, err error) {
	creds, err := mgr.GetCredentials(conn.ID)
	if err != nil {
		return "", "", err
	}
	baseURL = strings.TrimRight(conn.Metadata["base_url"], "/")
	if baseURL == "" {
		baseURL = "http://homeassistant.local:8123"
	}
	return baseURL, creds["token"], nil
}

type haStatesTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *haStatesTool) Name() string { return "ha_states" }
func (t *haStatesTool) Description() string {
	return "Get Home Assistant entity states. Omit entity_id to list all states."
}
func (t *haStatesTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *haStatesTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "ha_states",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"entity_id": {Type: "string", Description: "Entity ID to retrieve (e.g. 'light.living_room'). Omit to list all states."},
				},
			},
		},
	}
}
func (t *haStatesTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, token, err := haCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "ha_states: auth: " + err.Error()}
	}
	apiURL := baseURL + "/api/states"
	if entityID, ok := args["entity_id"].(string); ok && entityID != "" {
		apiURL = fmt.Sprintf("%s/api/states/%s", baseURL, url.PathEscape(entityID))
	}
	out, err := haDoFn(ctx, http.MethodGet, apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

type haCallServiceTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *haCallServiceTool) Name() string { return "ha_call_service" }
func (t *haCallServiceTool) Description() string {
	return "Call a Home Assistant service (e.g. turn on a light). Requires user approval."
}
func (t *haCallServiceTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *haCallServiceTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "ha_call_service",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"domain", "service"},
				Properties: map[string]backend.ToolProperty{
					"domain":       {Type: "string", Description: "Service domain (e.g. 'light', 'switch', 'script', 'automation')"},
					"service":      {Type: "string", Description: "Service name (e.g. 'turn_on', 'turn_off', 'toggle')"},
					"entity_id":    {Type: "string", Description: "Target entity ID (optional, e.g. 'light.living_room')"},
					"service_data": {Type: "object", Description: "Additional service data as a JSON object (optional)"},
				},
			},
		},
	}
}
func (t *haCallServiceTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, token, err := haCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "ha_call_service: auth: " + err.Error()}
	}
	domain, ok := args["domain"].(string)
	if !ok || domain == "" {
		return tools.ToolResult{IsError: true, Error: "ha_call_service: domain is required"}
	}
	service, ok := args["service"].(string)
	if !ok || service == "" {
		return tools.ToolResult{IsError: true, Error: "ha_call_service: service is required"}
	}
	payload := map[string]any{}
	if entityID, ok := args["entity_id"].(string); ok && entityID != "" {
		payload["entity_id"] = entityID
	}
	if sd, ok := args["service_data"].(map[string]any); ok {
		for k, v := range sd {
			payload[k] = v
		}
	}
	bodyBytes, _ := json.Marshal(payload)
	apiURL := fmt.Sprintf("%s/api/services/%s/%s", baseURL, domain, service)
	out, err := haDoFn(ctx, http.MethodPost, apiURL, token, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

type haSceneActivateTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *haSceneActivateTool) Name() string { return "ha_scene_activate" }
func (t *haSceneActivateTool) Description() string {
	return "Activate a Home Assistant scene. Requires user approval."
}
func (t *haSceneActivateTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *haSceneActivateTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "ha_scene_activate",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"scene_id"},
				Properties: map[string]backend.ToolProperty{
					"scene_id": {Type: "string", Description: "Scene entity ID (e.g. 'scene.movie_night')"},
				},
			},
		},
	}
}
func (t *haSceneActivateTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	baseURL, token, err := haCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "ha_scene_activate: auth: " + err.Error()}
	}
	sceneID, ok := args["scene_id"].(string)
	if !ok || sceneID == "" {
		return tools.ToolResult{IsError: true, Error: "ha_scene_activate: scene_id is required"}
	}
	payload := map[string]any{"entity_id": sceneID}
	bodyBytes, _ := json.Marshal(payload)
	apiURL := fmt.Sprintf("%s/api/services/scene/turn_on", baseURL)
	out, err := haDoFn(ctx, http.MethodPost, apiURL, token, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

func registerHomeAssistantTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{"ha_states", "ha_call_service", "ha_scene_activate"}
	for _, n := range names {
		reg.Unregister(n)
	}
	strictInject(reg, &haStatesTool{mgr: mgr, conns: conns})
	strictInject(reg, &haCallServiceTool{mgr: mgr, conns: conns})
	strictInject(reg, &haSceneActivateTool{mgr: mgr, conns: conns})
	return nil
}
