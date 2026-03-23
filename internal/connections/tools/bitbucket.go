package conntools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/connections"
	connproviders "github.com/scrypster/huginn/internal/connections/providers"
	"github.com/scrypster/huginn/internal/tools"
)

// bitbucketProvider is a minimal stub to supply OAuthConfig to Manager.GetHTTPClient.
var bitbucketProvider = connproviders.NewBitbucket("", "")

const bitbucketAPIBase = "https://api.bitbucket.org/2.0"

// --- bitbucket_list_prs ---

type bitbucketListPRsTool struct {
	mgr          *connections.Manager
	conns        []connections.Connection
	accountsDesc string
}

func (t *bitbucketListPRsTool) Name() string { return "bitbucket_list_prs" }
func (t *bitbucketListPRsTool) Description() string {
	return fmt.Sprintf("List Bitbucket pull requests for a repository. Accounts: %s.", t.accountsDesc)
}
func (t *bitbucketListPRsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *bitbucketListPRsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "bitbucket_list_prs",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"workspace", "repo_slug"},
				Properties: map[string]backend.ToolProperty{
					"workspace": {Type: "string", Description: "Bitbucket workspace slug"},
					"repo_slug": {Type: "string", Description: "Repository slug"},
					"state":     {Type: "string", Description: "PR state: OPEN, MERGED, DECLINED (default: OPEN)"},
					"account":   {Type: "string", Description: "Bitbucket account label (optional)"},
				},
			},
		},
	}
}
func (t *bitbucketListPRsTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	account, _ := args["account"].(string)
	conn := resolveConnection(t.conns, account)
	client, err := t.mgr.GetHTTPClient(ctx, conn.ID, bitbucketProvider)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("bitbucket_list_prs: auth: %v", err)}
	}
	workspace, _ := args["workspace"].(string)
	repoSlug, _ := args["repo_slug"].(string)
	state := "OPEN"
	if s, ok := args["state"].(string); ok && s != "" {
		state = s
	}
	apiURL := fmt.Sprintf("%s/repositories/%s/%s/pullrequests?state=%s",
		bitbucketAPIBase, workspace, repoSlug, state)
	out, err := bitbucketGET(ctx, client, apiURL)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- bitbucket_get_pr ---

type bitbucketGetPRTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *bitbucketGetPRTool) Name() string        { return "bitbucket_get_pr" }
func (t *bitbucketGetPRTool) Description() string { return "Get details of a Bitbucket pull request." }
func (t *bitbucketGetPRTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *bitbucketGetPRTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "bitbucket_get_pr",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"workspace", "repo_slug", "pr_id"},
				Properties: map[string]backend.ToolProperty{
					"workspace": {Type: "string", Description: "Bitbucket workspace slug"},
					"repo_slug": {Type: "string", Description: "Repository slug"},
					"pr_id":     {Type: "integer", Description: "Pull request ID"},
					"account":   {Type: "string", Description: "Bitbucket account label (optional)"},
				},
			},
		},
	}
}
func (t *bitbucketGetPRTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	account, _ := args["account"].(string)
	conn := resolveConnection(t.conns, account)
	client, err := t.mgr.GetHTTPClient(ctx, conn.ID, bitbucketProvider)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("bitbucket_get_pr: auth: %v", err)}
	}
	workspace, _ := args["workspace"].(string)
	repoSlug, _ := args["repo_slug"].(string)
	prID := int(floatArg(args, "pr_id"))
	apiURL := fmt.Sprintf("%s/repositories/%s/%s/pullrequests/%d",
		bitbucketAPIBase, workspace, repoSlug, prID)
	out, err := bitbucketGET(ctx, client, apiURL)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- bitbucket_create_pr ---

type bitbucketCreatePRTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *bitbucketCreatePRTool) Name() string        { return "bitbucket_create_pr" }
func (t *bitbucketCreatePRTool) Description() string { return "Create a Bitbucket pull request. Requires user approval." }
func (t *bitbucketCreatePRTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *bitbucketCreatePRTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "bitbucket_create_pr",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"workspace", "repo_slug", "title", "source_branch", "destination_branch"},
				Properties: map[string]backend.ToolProperty{
					"workspace":          {Type: "string", Description: "Bitbucket workspace slug"},
					"repo_slug":          {Type: "string", Description: "Repository slug"},
					"title":              {Type: "string", Description: "Pull request title"},
					"source_branch":      {Type: "string", Description: "Source branch name"},
					"destination_branch": {Type: "string", Description: "Destination branch name"},
					"description":        {Type: "string", Description: "Pull request description (optional)"},
					"account":            {Type: "string", Description: "Bitbucket account label (optional)"},
				},
			},
		},
	}
}
func (t *bitbucketCreatePRTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	account, _ := args["account"].(string)
	conn := resolveConnection(t.conns, account)
	client, err := t.mgr.GetHTTPClient(ctx, conn.ID, bitbucketProvider)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("bitbucket_create_pr: auth: %v", err)}
	}
	workspace, _ := args["workspace"].(string)
	repoSlug, _ := args["repo_slug"].(string)
	title, _ := args["title"].(string)
	sourceBranch, _ := args["source_branch"].(string)
	destBranch, _ := args["destination_branch"].(string)
	description, _ := args["description"].(string)
	payload := map[string]any{
		"title":       title,
		"description": description,
		"source":      map[string]any{"branch": map[string]string{"name": sourceBranch}},
		"destination": map[string]any{"branch": map[string]string{"name": destBranch}},
	}
	apiURL := fmt.Sprintf("%s/repositories/%s/%s/pullrequests", bitbucketAPIBase, workspace, repoSlug)
	out, err := bitbucketPOST(ctx, client, apiURL, payload)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// registerBitbucketTools registers all Bitbucket tools.
func registerBitbucketTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{"bitbucket_list_prs", "bitbucket_get_pr", "bitbucket_create_pr"}
	for _, n := range names {
		reg.Unregister(n)
	}

	var labels []string
	for _, c := range conns {
		labels = append(labels, c.AccountLabel)
	}
	accountsDesc := strings.Join(labels, ", ")

	strictInject(reg, &bitbucketListPRsTool{mgr: mgr, conns: conns, accountsDesc: accountsDesc})
	strictInject(reg, &bitbucketGetPRTool{mgr: mgr, conns: conns})
	strictInject(reg, &bitbucketCreatePRTool{mgr: mgr, conns: conns})
	return nil
}

// --- Bitbucket API helpers ---

func bitbucketGET(ctx context.Context, client *http.Client, apiURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("bitbucket: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("bitbucket: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("bitbucket: HTTP %d for %s", resp.StatusCode, apiURL)
	}
	var result any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("bitbucket: decode: %w", err)
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

func bitbucketPOST(ctx context.Context, client *http.Client, apiURL string, payload any) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("bitbucket: marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(string(data)))
	if err != nil {
		return "", fmt.Errorf("bitbucket: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("bitbucket: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("bitbucket: HTTP %d for %s", resp.StatusCode, apiURL)
	}
	var result any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("bitbucket: decode: %w", err)
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}
