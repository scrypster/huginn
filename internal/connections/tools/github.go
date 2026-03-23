package conntools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/connections"
	connproviders "github.com/scrypster/huginn/internal/connections/providers"
	"github.com/scrypster/huginn/internal/tools"
)

// githubProvider is a minimal stub to supply OAuthConfig to Manager.GetHTTPClient.
var githubProvider = connproviders.NewGitHub("", "")

const githubAcceptHeader = "application/vnd.github.v3+json"

// --- github_list_prs ---

type githubListPRsTool struct {
	mgr          *connections.Manager
	conns        []connections.Connection
	accountsDesc string
}

func (t *githubListPRsTool) Name() string { return "github_list_prs" }
func (t *githubListPRsTool) Description() string {
	return fmt.Sprintf("List open pull requests for a GitHub repository. Accounts: %s.", t.accountsDesc)
}
func (t *githubListPRsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *githubListPRsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "github_list_prs",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"owner", "repo"},
				Properties: map[string]backend.ToolProperty{
					"owner":   {Type: "string", Description: "Repository owner (user or org)"},
					"repo":    {Type: "string", Description: "Repository name"},
					"state":   {Type: "string", Description: "PR state: open, closed, or all (default: open)"},
					"account": {Type: "string", Description: "GitHub account label (optional)"},
				},
			},
		},
	}
}
func (t *githubListPRsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	account, _ := args["account"].(string)
	conn := resolveConnection(t.conns, account)
	client, err := t.mgr.GetHTTPClient(ctx, conn.ID, githubProvider)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("github_list_prs: auth: %v", err)}
	}
	owner, _ := args["owner"].(string)
	repo, _ := args["repo"].(string)
	state := "open"
	if s, ok := args["state"].(string); ok && s != "" {
		state = s
	}
	out, err := githubGET(ctx, client,
		fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls?state=%s", owner, repo, url.QueryEscape(state)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- github_get_pr ---

type githubGetPRTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *githubGetPRTool) Name() string        { return "github_get_pr" }
func (t *githubGetPRTool) Description() string { return "Get details of a specific GitHub pull request." }
func (t *githubGetPRTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *githubGetPRTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "github_get_pr",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"owner", "repo", "number"},
				Properties: map[string]backend.ToolProperty{
					"owner":   {Type: "string", Description: "Repository owner"},
					"repo":    {Type: "string", Description: "Repository name"},
					"number":  {Type: "integer", Description: "Pull request number"},
					"account": {Type: "string", Description: "GitHub account label (optional)"},
				},
			},
		},
	}
}
func (t *githubGetPRTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	account, _ := args["account"].(string)
	conn := resolveConnection(t.conns, account)
	client, err := t.mgr.GetHTTPClient(ctx, conn.ID, githubProvider)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("github_get_pr: auth: %v", err)}
	}
	owner, _ := args["owner"].(string)
	repo, _ := args["repo"].(string)
	number := int(floatArg(args, "number"))
	out, err := githubGET(ctx, client,
		fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", owner, repo, number))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- github_create_issue ---

type githubCreateIssueTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *githubCreateIssueTool) Name() string        { return "github_create_issue" }
func (t *githubCreateIssueTool) Description() string { return "Create a new issue in a GitHub repository." }
func (t *githubCreateIssueTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *githubCreateIssueTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "github_create_issue",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"owner", "repo", "title"},
				Properties: map[string]backend.ToolProperty{
					"owner":   {Type: "string", Description: "Repository owner"},
					"repo":    {Type: "string", Description: "Repository name"},
					"title":   {Type: "string", Description: "Issue title"},
					"body":    {Type: "string", Description: "Issue body (markdown)"},
					"account": {Type: "string", Description: "GitHub account label (optional)"},
				},
			},
		},
	}
}
func (t *githubCreateIssueTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	account, _ := args["account"].(string)
	conn := resolveConnection(t.conns, account)
	client, err := t.mgr.GetHTTPClient(ctx, conn.ID, githubProvider)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("github_create_issue: auth: %v", err)}
	}
	owner, _ := args["owner"].(string)
	repo, _ := args["repo"].(string)
	title, _ := args["title"].(string)
	body, _ := args["body"].(string)
	payload := map[string]string{"title": title, "body": body}
	out, err := githubPOST(ctx, client,
		fmt.Sprintf("https://api.github.com/repos/%s/%s/issues", owner, repo), payload)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- github_search_code ---

type githubSearchCodeTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *githubSearchCodeTool) Name() string        { return "github_search_code" }
func (t *githubSearchCodeTool) Description() string { return "Search code on GitHub." }
func (t *githubSearchCodeTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *githubSearchCodeTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "github_search_code",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"query"},
				Properties: map[string]backend.ToolProperty{
					"query":   {Type: "string", Description: "GitHub code search query (e.g. 'repo:owner/name func main language:go')"},
					"account": {Type: "string", Description: "GitHub account label (optional)"},
				},
			},
		},
	}
}
func (t *githubSearchCodeTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	account, _ := args["account"].(string)
	conn := resolveConnection(t.conns, account)
	client, err := t.mgr.GetHTTPClient(ctx, conn.ID, githubProvider)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("github_search_code: auth: %v", err)}
	}
	query, _ := args["query"].(string)
	out, err := githubGET(ctx, client,
		fmt.Sprintf("https://api.github.com/search/code?q=%s", url.QueryEscape(query)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- github_list_issues ---

type githubListIssuesTool struct {
	mgr          *connections.Manager
	conns        []connections.Connection
	accountsDesc string
}

func (t *githubListIssuesTool) Name() string { return "github_list_issues" }
func (t *githubListIssuesTool) Description() string {
	return fmt.Sprintf("List issues for a GitHub repository. Accounts: %s.", t.accountsDesc)
}
func (t *githubListIssuesTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *githubListIssuesTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "github_list_issues",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"owner", "repo"},
				Properties: map[string]backend.ToolProperty{
					"owner":   {Type: "string", Description: "Repository owner"},
					"repo":    {Type: "string", Description: "Repository name"},
					"state":   {Type: "string", Description: "Issue state: open, closed, or all (default: open)"},
					"account": {Type: "string", Description: "GitHub account label (optional)"},
				},
			},
		},
	}
}
func (t *githubListIssuesTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	account, _ := args["account"].(string)
	conn := resolveConnection(t.conns, account)
	client, err := t.mgr.GetHTTPClient(ctx, conn.ID, githubProvider)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("github_list_issues: auth: %v", err)}
	}
	owner, _ := args["owner"].(string)
	repo, _ := args["repo"].(string)
	state := "open"
	if s, ok := args["state"].(string); ok && s != "" {
		state = s
	}
	out, err := githubGET(ctx, client,
		fmt.Sprintf("https://api.github.com/repos/%s/%s/issues?state=%s", owner, repo, url.QueryEscape(state)))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerGitHubTools registers all GitHub tools.
func registerGitHubTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{"github_list_prs", "github_get_pr", "github_create_issue", "github_search_code", "github_list_issues"}
	for _, n := range names {
		reg.Unregister(n)
	}

	var labels []string
	for _, c := range conns {
		labels = append(labels, c.AccountLabel)
	}
	accountsDesc := strings.Join(labels, ", ")

	strictInject(reg, &githubListPRsTool{mgr: mgr, conns: conns, accountsDesc: accountsDesc})
	strictInject(reg, &githubGetPRTool{mgr: mgr, conns: conns})
	strictInject(reg, &githubCreateIssueTool{mgr: mgr, conns: conns})
	strictInject(reg, &githubSearchCodeTool{mgr: mgr, conns: conns})
	strictInject(reg, &githubListIssuesTool{mgr: mgr, conns: conns, accountsDesc: accountsDesc})
	return nil
}

// --- HTTP helpers ---

func githubGET(ctx context.Context, client *http.Client, apiURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Accept", githubAcceptHeader)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("github: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("github: HTTP %d for %s", resp.StatusCode, apiURL)
	}
	var result any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("github: decode: %w", err)
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

func githubPOST(ctx context.Context, client *http.Client, apiURL string, payload any) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("github: marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(string(data)))
	if err != nil {
		return "", fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Accept", githubAcceptHeader)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("github: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("github: HTTP %d for %s", resp.StatusCode, apiURL)
	}
	var result any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("github: decode: %w", err)
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

func floatArg(args map[string]any, key string) float64 {
	if v, ok := args[key].(float64); ok {
		return v
	}
	return 0
}
