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

// todoistDo performs an authenticated Todoist REST API v2 request.
func todoistDo(ctx context.Context, method, apiURL, token string, body io.Reader) (string, error) {
	req, err := http.NewRequestWithContext(ctx, method, apiURL, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
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

// todoistCreds extracts the API token from a connection.
func todoistCreds(mgr *connections.Manager, conn connections.Connection) (string, error) {
	creds, err := mgr.GetCredentials(conn.ID)
	if err != nil {
		return "", err
	}
	return creds["api_key"], nil
}

func buildTodoistTasksListURL(args map[string]any) string {
	baseURL := "https://api.todoist.com/rest/v2/tasks"
	params := url.Values{}
	if pid, ok := args["project_id"].(string); ok && pid != "" {
		params.Set("project_id", pid)
	}
	if f, ok := args["filter"].(string); ok && f != "" {
		params.Set("filter", f)
	}
	encoded := params.Encode()
	if encoded == "" {
		return baseURL
	}
	return baseURL + "?" + encoded
}

func buildTodoistTaskCompleteURL(taskID string) string {
	return fmt.Sprintf("https://api.todoist.com/rest/v2/tasks/%s/close", url.PathEscape(taskID))
}

// --- tasks_list ---

type todoistTasksListTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *todoistTasksListTool) Name() string { return "tasks_list" }
func (t *todoistTasksListTool) Description() string {
	return "List Todoist tasks, optionally filtered by project or filter string."
}
func (t *todoistTasksListTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *todoistTasksListTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "tasks_list",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"project_id": {Type: "string", Description: "Filter tasks by project ID (optional)"},
					"filter":     {Type: "string", Description: "Todoist filter expression, e.g. 'today' or 'overdue' (optional)"},
				},
			},
		},
	}
}
func (t *todoistTasksListTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := todoistCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "tasks_list: auth: " + err.Error()}
	}
	apiURL := buildTodoistTasksListURL(args)
	out, err := todoistDo(ctx, http.MethodGet, apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- task_create ---

type todoistTaskCreateTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *todoistTaskCreateTool) Name() string { return "task_create" }
func (t *todoistTaskCreateTool) Description() string {
	return "Create a new Todoist task. Requires user approval."
}
func (t *todoistTaskCreateTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *todoistTaskCreateTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "task_create",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"content"},
				Properties: map[string]backend.ToolProperty{
					"content":    {Type: "string", Description: "Task content / title"},
					"project_id": {Type: "string", Description: "Project ID to add the task to (optional, defaults to Inbox)"},
					"due_string": {Type: "string", Description: "Natural language due date, e.g. 'tomorrow', 'next Monday' (optional)"},
				},
			},
		},
	}
}
func (t *todoistTaskCreateTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := todoistCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "task_create: auth: " + err.Error()}
	}
	content, ok := args["content"].(string)
	if !ok || content == "" {
		return tools.ToolResult{IsError: true, Error: "task_create: content is required"}
	}
	payload := map[string]any{"content": content}
	if pid, ok := args["project_id"].(string); ok && pid != "" {
		payload["project_id"] = pid
	}
	if due, ok := args["due_string"].(string); ok && due != "" {
		payload["due_string"] = due
	}
	bodyBytes, _ := json.Marshal(payload)
	out, err := todoistDo(ctx, http.MethodPost, "https://api.todoist.com/rest/v2/tasks", token, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- task_complete ---

type todoistTaskCompleteTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *todoistTaskCompleteTool) Name() string { return "task_complete" }
func (t *todoistTaskCompleteTool) Description() string {
	return "Mark a Todoist task as complete. Requires user approval."
}
func (t *todoistTaskCompleteTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *todoistTaskCompleteTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "task_complete",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"task_id"},
				Properties: map[string]backend.ToolProperty{
					"task_id": {Type: "string", Description: "The Todoist task ID to mark complete"},
				},
			},
		},
	}
}
func (t *todoistTaskCompleteTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := todoistCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "task_complete: auth: " + err.Error()}
	}
	taskID, ok := args["task_id"].(string)
	if !ok || taskID == "" {
		return tools.ToolResult{IsError: true, Error: "task_complete: task_id is required"}
	}
	apiURL := buildTodoistTaskCompleteURL(taskID)
	out, err := todoistDo(ctx, http.MethodPost, apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- tasks_list_projects ---

type todoistListProjectsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *todoistListProjectsTool) Name() string                      { return "tasks_list_projects" }
func (t *todoistListProjectsTool) Description() string               { return "List all Todoist projects." }
func (t *todoistListProjectsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *todoistListProjectsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "tasks_list_projects",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:       "object",
				Properties: map[string]backend.ToolProperty{},
			},
		},
	}
}
func (t *todoistListProjectsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, err := todoistCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "tasks_list_projects: auth: " + err.Error()}
	}
	out, err := todoistDo(ctx, http.MethodGet, "https://api.todoist.com/rest/v2/projects", token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerTodoistTools registers all 4 Todoist tools.
func registerTodoistTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{"tasks_list", "task_create", "task_complete", "tasks_list_projects"}
	for _, n := range names {
		reg.Unregister(n)
	}
	strictInject(reg, &todoistTasksListTool{mgr: mgr, conns: conns})
	strictInject(reg, &todoistTaskCreateTool{mgr: mgr, conns: conns})
	strictInject(reg, &todoistTaskCompleteTool{mgr: mgr, conns: conns})
	strictInject(reg, &todoistListProjectsTool{mgr: mgr, conns: conns})
	return nil
}
