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

// asanaDo performs an authenticated Asana API request.
func asanaDo(ctx context.Context, method, apiURL, token string, body io.Reader) (string, error) {
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

// asanaCreds extracts the token from a connection.
func asanaCreds(mgr *connections.Manager, conn connections.Connection) (string, error) {
	creds, err := mgr.GetCredentials(conn.ID)
	if err != nil {
		return "", err
	}
	return creds["token"], nil
}

// --- asana_list_workspaces ---

type asanaListWorkspacesTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *asanaListWorkspacesTool) Name() string        { return "asana_list_workspaces" }
func (t *asanaListWorkspacesTool) Description() string { return "List Asana workspaces." }
func (t *asanaListWorkspacesTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *asanaListWorkspacesTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "asana_list_workspaces",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:       "object",
				Properties: map[string]backend.ToolProperty{},
			},
		},
	}
}
func (t *asanaListWorkspacesTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := asanaCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "asana_list_workspaces: auth: " + err.Error()}
	}
	out, err := asanaDo(ctx, "GET", "https://app.asana.com/api/1.0/workspaces", token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- asana_list_projects ---

type asanaListProjectsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *asanaListProjectsTool) Name() string        { return "asana_list_projects" }
func (t *asanaListProjectsTool) Description() string { return "List Asana projects." }
func (t *asanaListProjectsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *asanaListProjectsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "asana_list_projects",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"workspace": {Type: "string", Description: "Workspace GID to filter projects"},
					"limit":     {Type: "integer", Description: "Maximum number of results (default 25)"},
				},
			},
		},
	}
}
func (t *asanaListProjectsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := asanaCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "asana_list_projects: auth: " + err.Error()}
	}

	limit := int(floatArg(args, "limit"))
	if limit <= 0 {
		limit = 25
	}

	params := url.Values{}
	params.Set("limit", strconv.Itoa(limit))
	if workspace, ok := args["workspace"].(string); ok && workspace != "" {
		params.Set("workspace", workspace)
	}

	apiURL := "https://app.asana.com/api/1.0/projects?" + params.Encode()
	out, err := asanaDo(ctx, "GET", apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- asana_list_tasks ---

type asanaListTasksTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *asanaListTasksTool) Name() string        { return "asana_list_tasks" }
func (t *asanaListTasksTool) Description() string { return "List tasks in an Asana project." }
func (t *asanaListTasksTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *asanaListTasksTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "asana_list_tasks",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"project"},
				Properties: map[string]backend.ToolProperty{
					"project": {Type: "string", Description: "Project GID"},
					"limit":   {Type: "integer", Description: "Maximum number of results (default 25)"},
				},
			},
		},
	}
}
func (t *asanaListTasksTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := asanaCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "asana_list_tasks: auth: " + err.Error()}
	}

	project, ok := args["project"].(string)
	if !ok || project == "" {
		return tools.ToolResult{IsError: true, Error: "asana_list_tasks: project is required"}
	}

	limit := int(floatArg(args, "limit"))
	if limit <= 0 {
		limit = 25
	}

	params := url.Values{}
	params.Set("project", project)
	params.Set("limit", strconv.Itoa(limit))
	apiURL := "https://app.asana.com/api/1.0/tasks?" + params.Encode()
	out, err := asanaDo(ctx, "GET", apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- asana_get_task ---

type asanaGetTaskTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *asanaGetTaskTool) Name() string        { return "asana_get_task" }
func (t *asanaGetTaskTool) Description() string { return "Get an Asana task by GID." }
func (t *asanaGetTaskTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *asanaGetTaskTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "asana_get_task",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"task_gid"},
				Properties: map[string]backend.ToolProperty{
					"task_gid": {Type: "string", Description: "The task GID"},
				},
			},
		},
	}
}
func (t *asanaGetTaskTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := asanaCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "asana_get_task: auth: " + err.Error()}
	}
	taskGID, ok := args["task_gid"].(string)
	if !ok || taskGID == "" {
		return tools.ToolResult{IsError: true, Error: "asana_get_task: task_gid is required"}
	}
	apiURL := fmt.Sprintf("https://app.asana.com/api/1.0/tasks/%s", taskGID)
	out, err := asanaDo(ctx, "GET", apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- asana_create_task ---

type asanaCreateTaskTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *asanaCreateTaskTool) Name() string        { return "asana_create_task" }
func (t *asanaCreateTaskTool) Description() string { return "Create a new Asana task. Requires user approval." }
func (t *asanaCreateTaskTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *asanaCreateTaskTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "asana_create_task",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"name", "workspace"},
				Properties: map[string]backend.ToolProperty{
					"name":      {Type: "string", Description: "Task name"},
					"workspace": {Type: "string", Description: "Workspace GID"},
					"notes":     {Type: "string", Description: "Task notes / description"},
					"projects":  {Type: "array", Description: "List of project GIDs to add the task to"},
				},
			},
		},
	}
}
func (t *asanaCreateTaskTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := asanaCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "asana_create_task: auth: " + err.Error()}
	}

	name, _ := args["name"].(string)
	workspace, _ := args["workspace"].(string)
	if name == "" || workspace == "" {
		return tools.ToolResult{IsError: true, Error: "asana_create_task: name and workspace are required"}
	}

	taskData := map[string]any{
		"name":      name,
		"workspace": workspace,
	}
	if notes, ok := args["notes"].(string); ok && notes != "" {
		taskData["notes"] = notes
	}
	if projects, ok := args["projects"]; ok && projects != nil {
		taskData["projects"] = projects
	}

	payload := map[string]any{"data": taskData}
	bodyBytes, _ := json.Marshal(payload)
	out, err := asanaDo(ctx, "POST", "https://app.asana.com/api/1.0/tasks", token, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerAsanaTools registers all 5 Asana tools.
func registerAsanaTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{
		"asana_list_workspaces", "asana_list_projects", "asana_list_tasks",
		"asana_get_task", "asana_create_task",
	}
	for _, n := range names {
		reg.Unregister(n)
	}
	strictInject(reg, &asanaListWorkspacesTool{mgr: mgr, conns: conns})
	strictInject(reg, &asanaListProjectsTool{mgr: mgr, conns: conns})
	strictInject(reg, &asanaListTasksTool{mgr: mgr, conns: conns})
	strictInject(reg, &asanaGetTaskTool{mgr: mgr, conns: conns})
	strictInject(reg, &asanaCreateTaskTool{mgr: mgr, conns: conns})
	return nil
}
