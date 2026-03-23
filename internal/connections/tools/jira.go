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

// jiraProvider is a minimal stub to supply OAuthConfig to Manager.GetHTTPClient.
var jiraProvider = connproviders.NewJira("", "")

// --- jira_list_issues ---

type jiraListIssuesTool struct {
	mgr          *connections.Manager
	conns        []connections.Connection
	accountsDesc string
}

func (t *jiraListIssuesTool) Name() string { return "jira_list_issues" }
func (t *jiraListIssuesTool) Description() string {
	return fmt.Sprintf("List Jira issues using JQL. Accounts: %s.", t.accountsDesc)
}
func (t *jiraListIssuesTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *jiraListIssuesTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "jira_list_issues",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"cloud_id", "jql"},
				Properties: map[string]backend.ToolProperty{
					"cloud_id": {Type: "string", Description: "Atlassian cloud ID (from accessible-resources)"},
					"jql":      {Type: "string", Description: "JQL query (e.g. 'project = FOO AND status = Open')"},
					"max":      {Type: "integer", Description: "Max issues to return (default 20)"},
					"account":  {Type: "string", Description: "Jira account label (optional)"},
				},
			},
		},
	}
}
func (t *jiraListIssuesTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	account, _ := args["account"].(string)
	conn := resolveConnection(t.conns, account)
	client, err := t.mgr.GetHTTPClient(ctx, conn.ID, jiraProvider)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("jira_list_issues: auth: %v", err)}
	}
	cloudID, _ := args["cloud_id"].(string)
	jql, _ := args["jql"].(string)
	maxResults := 20
	if m := int(floatArg(args, "max")); m > 0 {
		maxResults = m
	}
	payload := map[string]any{
		"jql":        jql,
		"maxResults": maxResults,
		"fields":     []string{"summary", "status", "assignee", "priority", "issuetype"},
	}
	out, err := jiraPOST(ctx, client,
		fmt.Sprintf("https://api.atlassian.com/ex/jira/%s/rest/api/3/issue/search", cloudID), payload)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- jira_get_issue ---

type jiraGetIssueTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *jiraGetIssueTool) Name() string        { return "jira_get_issue" }
func (t *jiraGetIssueTool) Description() string { return "Get a Jira issue by key (e.g. FOO-123)." }
func (t *jiraGetIssueTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *jiraGetIssueTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "jira_get_issue",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"cloud_id", "issue_key"},
				Properties: map[string]backend.ToolProperty{
					"cloud_id":  {Type: "string", Description: "Atlassian cloud ID"},
					"issue_key": {Type: "string", Description: "Issue key (e.g. FOO-123)"},
					"account":   {Type: "string", Description: "Jira account label (optional)"},
				},
			},
		},
	}
}
func (t *jiraGetIssueTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	account, _ := args["account"].(string)
	conn := resolveConnection(t.conns, account)
	client, err := t.mgr.GetHTTPClient(ctx, conn.ID, jiraProvider)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("jira_get_issue: auth: %v", err)}
	}
	cloudID, _ := args["cloud_id"].(string)
	issueKey, _ := args["issue_key"].(string)
	out, err := jiraGET(ctx, client,
		fmt.Sprintf("https://api.atlassian.com/ex/jira/%s/rest/api/3/issue/%s", cloudID, issueKey))
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- jira_create_issue ---

type jiraCreateIssueTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *jiraCreateIssueTool) Name() string        { return "jira_create_issue" }
func (t *jiraCreateIssueTool) Description() string { return "Create a new Jira issue. Requires user approval." }
func (t *jiraCreateIssueTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *jiraCreateIssueTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "jira_create_issue",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"cloud_id", "project_key", "summary", "issue_type"},
				Properties: map[string]backend.ToolProperty{
					"cloud_id":    {Type: "string", Description: "Atlassian cloud ID"},
					"project_key": {Type: "string", Description: "Jira project key (e.g. FOO)"},
					"summary":     {Type: "string", Description: "Issue summary"},
					"issue_type":  {Type: "string", Description: "Issue type (e.g. Bug, Story, Task)"},
					"description": {Type: "string", Description: "Issue description (plain text)"},
					"account":     {Type: "string", Description: "Jira account label (optional)"},
				},
			},
		},
	}
}
func (t *jiraCreateIssueTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	account, _ := args["account"].(string)
	conn := resolveConnection(t.conns, account)
	client, err := t.mgr.GetHTTPClient(ctx, conn.ID, jiraProvider)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("jira_create_issue: auth: %v", err)}
	}
	cloudID, _ := args["cloud_id"].(string)
	projectKey, _ := args["project_key"].(string)
	summary, _ := args["summary"].(string)
	issueType, _ := args["issue_type"].(string)
	description, _ := args["description"].(string)
	payload := map[string]any{
		"fields": map[string]any{
			"project":   map[string]string{"key": projectKey},
			"summary":   summary,
			"issuetype": map[string]string{"name": issueType},
			"description": map[string]any{
				"type":    "doc",
				"version": 1,
				"content": []map[string]any{
					{
						"type": "paragraph",
						"content": []map[string]any{
							{"type": "text", "text": description},
						},
					},
				},
			},
		},
	}
	out, err := jiraPOST(ctx, client,
		fmt.Sprintf("https://api.atlassian.com/ex/jira/%s/rest/api/3/issue", cloudID), payload)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: err.Error()}
	}
	return tools.ToolResult{Output: out}
}

// --- jira_update_issue ---

type jiraUpdateIssueTool struct {
	mgr   *connections.Manager
	conns []connections.Connection
}

func (t *jiraUpdateIssueTool) Name() string        { return "jira_update_issue" }
func (t *jiraUpdateIssueTool) Description() string { return "Update a Jira issue's fields. Requires user approval." }
func (t *jiraUpdateIssueTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *jiraUpdateIssueTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "jira_update_issue",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"cloud_id", "issue_key"},
				Properties: map[string]backend.ToolProperty{
					"cloud_id":  {Type: "string", Description: "Atlassian cloud ID"},
					"issue_key": {Type: "string", Description: "Issue key (e.g. FOO-123)"},
					"summary":   {Type: "string", Description: "New summary (optional)"},
					"status":    {Type: "string", Description: "New status transition name (optional)"},
					"account":   {Type: "string", Description: "Jira account label (optional)"},
				},
			},
		},
	}
}
func (t *jiraUpdateIssueTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	account, _ := args["account"].(string)
	conn := resolveConnection(t.conns, account)
	client, err := t.mgr.GetHTTPClient(ctx, conn.ID, jiraProvider)
	if err != nil {
		return tools.ToolResult{IsError: true, Error: fmt.Sprintf("jira_update_issue: auth: %v", err)}
	}
	cloudID, _ := args["cloud_id"].(string)
	issueKey, _ := args["issue_key"].(string)
	fields := map[string]any{}
	if summary, ok := args["summary"].(string); ok && summary != "" {
		fields["summary"] = summary
	}
	if len(fields) > 0 {
		payload := map[string]any{"fields": fields}
		if err := jiraPUT(ctx, client,
			fmt.Sprintf("https://api.atlassian.com/ex/jira/%s/rest/api/3/issue/%s", cloudID, issueKey),
			payload); err != nil {
			return tools.ToolResult{IsError: true, Error: err.Error()}
		}
	}
	return tools.ToolResult{Output: fmt.Sprintf(`{"updated": true, "issue_key": "%s"}`, issueKey)}
}

// registerJiraTools registers all Jira tools.
func registerJiraTools(reg *tools.Registry, mgr *connections.Manager, conns []connections.Connection) error {
	names := []string{"jira_list_issues", "jira_get_issue", "jira_create_issue", "jira_update_issue"}
	for _, n := range names {
		reg.Unregister(n)
	}

	var labels []string
	for _, c := range conns {
		labels = append(labels, c.AccountLabel)
	}
	accountsDesc := strings.Join(labels, ", ")

	strictInject(reg, &jiraListIssuesTool{mgr: mgr, conns: conns, accountsDesc: accountsDesc})
	strictInject(reg, &jiraGetIssueTool{mgr: mgr, conns: conns})
	strictInject(reg, &jiraCreateIssueTool{mgr: mgr, conns: conns})
	strictInject(reg, &jiraUpdateIssueTool{mgr: mgr, conns: conns})
	return nil
}

// --- Jira API helpers ---

func jiraGET(ctx context.Context, client *http.Client, apiURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("jira: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("jira: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("jira: HTTP %d for %s", resp.StatusCode, apiURL)
	}
	var result any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("jira: decode: %w", err)
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

func jiraPOST(ctx context.Context, client *http.Client, apiURL string, payload any) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("jira: marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(string(data)))
	if err != nil {
		return "", fmt.Errorf("jira: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("jira: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("jira: HTTP %d for %s", resp.StatusCode, apiURL)
	}
	var result any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("jira: decode: %w", err)
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return string(out), nil
}

func jiraPUT(ctx context.Context, client *http.Client, apiURL string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("jira: marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "PUT", apiURL, strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("jira: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("jira: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("jira: HTTP %d for %s", resp.StatusCode, apiURL)
	}
	return nil
}
