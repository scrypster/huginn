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

// terraformDo performs an authenticated Terraform API request.
func terraformDo(ctx context.Context, method, apiURL, token string, body io.Reader) (string, error) {
	req, err := http.NewRequestWithContext(ctx, method, apiURL, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/vnd.api+json")
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

// terraformCreds extracts API token and organization from a connection.
func terraformCreds(mgr *connections.Manager, conn connections.Connection) (token, organization string, err error) {
	creds, err := mgr.GetCredentials(conn.ID)
	if err != nil {
		return "", "", err
	}
	return creds["token"], conn.Metadata["organization"], nil
}

// --- terraform_list_workspaces ---

type terraformListWorkspacesTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *terraformListWorkspacesTool) Name() string {
	return "terraform_list_workspaces"
}
func (t *terraformListWorkspacesTool) Description() string {
	return "List Terraform Cloud workspaces in an organization."
}
func (t *terraformListWorkspacesTool) Permission() tools.PermissionLevel {
	return tools.PermRead
}
func (t *terraformListWorkspacesTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "terraform_list_workspaces",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"organization"},
				Properties: map[string]backend.ToolProperty{
					"organization": {Type: "string", Description: "Organization name (uses connection default if not provided)"},
					"page_size":    {Type: "integer", Description: "Maximum number of results per page (default 20)"},
					"search":       {Type: "string", Description: "Search filter by workspace name"},
				},
			},
		},
	}
}
func (t *terraformListWorkspacesTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, organization, err := terraformCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "terraform_list_workspaces: auth: " + err.Error()}
	}

	// Override organization if provided
	if org, ok := args["organization"].(string); ok && org != "" {
		organization = org
	}
	if organization == "" {
		return tools.ToolResult{IsError: true, Error: "terraform_list_workspaces: organization is required"}
	}

	pageSize := int(floatArg(args, "page_size"))
	if pageSize <= 0 {
		pageSize = 20
	}

	params := url.Values{}
	params.Set("page[size]", fmt.Sprintf("%d", pageSize))
	if search, ok := args["search"].(string); ok && search != "" {
		params.Set("search[name]", search)
	}

	apiURL := fmt.Sprintf("https://app.terraform.io/api/v2/organizations/%s/workspaces?%s", url.PathEscape(organization), params.Encode())
	out, err := terraformDo(ctx, "GET", apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- terraform_get_workspace ---

type terraformGetWorkspaceTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *terraformGetWorkspaceTool) Name() string {
	return "terraform_get_workspace"
}
func (t *terraformGetWorkspaceTool) Description() string {
	return "Get details of a specific Terraform Cloud workspace."
}
func (t *terraformGetWorkspaceTool) Permission() tools.PermissionLevel {
	return tools.PermRead
}
func (t *terraformGetWorkspaceTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "terraform_get_workspace",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"organization", "workspace_name"},
				Properties: map[string]backend.ToolProperty{
					"organization":   {Type: "string", Description: "Organization name (uses connection default if not provided)"},
					"workspace_name": {Type: "string", Description: "Workspace name"},
				},
			},
		},
	}
}
func (t *terraformGetWorkspaceTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, organization, err := terraformCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "terraform_get_workspace: auth: " + err.Error()}
	}

	workspaceName, _ := args["workspace_name"].(string)
	if workspaceName == "" {
		return tools.ToolResult{IsError: true, Error: "terraform_get_workspace: workspace_name is required"}
	}

	// Override organization if provided
	if org, ok := args["organization"].(string); ok && org != "" {
		organization = org
	}
	if organization == "" {
		return tools.ToolResult{IsError: true, Error: "terraform_get_workspace: organization is required"}
	}

	apiURL := fmt.Sprintf("https://app.terraform.io/api/v2/organizations/%s/workspaces/%s",
		url.PathEscape(organization), url.PathEscape(workspaceName))
	out, err := terraformDo(ctx, "GET", apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- terraform_list_runs ---

type terraformListRunsTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *terraformListRunsTool) Name() string {
	return "terraform_list_runs"
}
func (t *terraformListRunsTool) Description() string {
	return "List runs in a Terraform Cloud workspace."
}
func (t *terraformListRunsTool) Permission() tools.PermissionLevel {
	return tools.PermRead
}
func (t *terraformListRunsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "terraform_list_runs",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"workspace_id"},
				Properties: map[string]backend.ToolProperty{
					"workspace_id": {Type: "string", Description: "The workspace ID"},
					"page_size":    {Type: "integer", Description: "Maximum number of results per page (default 20)"},
				},
			},
		},
	}
}
func (t *terraformListRunsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, _, err := terraformCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "terraform_list_runs: auth: " + err.Error()}
	}

	workspaceID, _ := args["workspace_id"].(string)
	if workspaceID == "" {
		return tools.ToolResult{IsError: true, Error: "terraform_list_runs: workspace_id is required"}
	}

	pageSize := int(floatArg(args, "page_size"))
	if pageSize <= 0 {
		pageSize = 20
	}

	params := url.Values{}
	params.Set("page[size]", fmt.Sprintf("%d", pageSize))

	apiURL := fmt.Sprintf("https://app.terraform.io/api/v2/workspaces/%s/runs?%s", url.PathEscape(workspaceID), params.Encode())
	out, err := terraformDo(ctx, "GET", apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- terraform_get_run ---

type terraformGetRunTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *terraformGetRunTool) Name() string {
	return "terraform_get_run"
}
func (t *terraformGetRunTool) Description() string {
	return "Get details of a specific Terraform Cloud run."
}
func (t *terraformGetRunTool) Permission() tools.PermissionLevel {
	return tools.PermRead
}
func (t *terraformGetRunTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "terraform_get_run",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"run_id"},
				Properties: map[string]backend.ToolProperty{
					"run_id": {Type: "string", Description: "The run ID"},
				},
			},
		},
	}
}
func (t *terraformGetRunTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, _, err := terraformCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "terraform_get_run: auth: " + err.Error()}
	}

	runID, _ := args["run_id"].(string)
	if runID == "" {
		return tools.ToolResult{IsError: true, Error: "terraform_get_run: run_id is required"}
	}

	apiURL := fmt.Sprintf("https://app.terraform.io/api/v2/runs/%s", url.PathEscape(runID))
	out, err := terraformDo(ctx, "GET", apiURL, token, nil)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- terraform_trigger_run ---

type terraformTriggerRunTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *terraformTriggerRunTool) Name() string {
	return "terraform_trigger_run"
}
func (t *terraformTriggerRunTool) Description() string {
	return "Trigger a new run in a Terraform Cloud workspace. Requires user approval."
}
func (t *terraformTriggerRunTool) Permission() tools.PermissionLevel {
	return tools.PermWrite
}
func (t *terraformTriggerRunTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "terraform_trigger_run",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"workspace_id"},
				Properties: map[string]backend.ToolProperty{
					"workspace_id": {Type: "string", Description: "The workspace ID"},
					"message":      {Type: "string", Description: "Run message/description"},
					"is_destroy":   {Type: "boolean", Description: "Whether this is a destroy run (default false)"},
				},
			},
		},
	}
}
func (t *terraformTriggerRunTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	conn := resolveConnection(t.conns, "")
	token, _, err := terraformCreds(t.mgr, conn)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: "terraform_trigger_run: auth: " + err.Error()}
	}

	workspaceID, _ := args["workspace_id"].(string)
	if workspaceID == "" {
		return tools.ToolResult{IsError: true, Error: "terraform_trigger_run: workspace_id is required"}
	}

	message, _ := args["message"].(string)
	isDestroy := false
	if b, ok := args["is_destroy"].(bool); ok {
		isDestroy = b
	}

	// Build JSON:API formatted request body
	payload := map[string]any{
		"data": map[string]any{
			"type": "runs",
			"attributes": map[string]any{
				"message":    message,
				"is-destroy": isDestroy,
			},
			"relationships": map[string]any{
				"workspace": map[string]any{
					"data": map[string]any{
						"type": "workspaces",
						"id":   workspaceID,
					},
				},
			},
		},
	}

	bodyBytes, _ := json.Marshal(payload)
	apiURL := "https://app.terraform.io/api/v2/runs"
	out, err := terraformDo(ctx, "POST", apiURL, token, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerTerraformTools registers all 5 Terraform tools.
func registerTerraformTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{
		"terraform_list_workspaces", "terraform_get_workspace", "terraform_list_runs",
		"terraform_get_run", "terraform_trigger_run",
	}
	for _, n := range names {
		reg.Unregister(n)
	}
	strictInject(reg, &terraformListWorkspacesTool{mgr: mgr, conns: conns})
	strictInject(reg, &terraformGetWorkspaceTool{mgr: mgr, conns: conns})
	strictInject(reg, &terraformListRunsTool{mgr: mgr, conns: conns})
	strictInject(reg, &terraformGetRunTool{mgr: mgr, conns: conns})
	strictInject(reg, &terraformTriggerRunTool{mgr: mgr, conns: conns})
	return nil
}
