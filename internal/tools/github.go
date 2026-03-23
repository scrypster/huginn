package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/scrypster/huginn/internal/agent/session"
	"github.com/scrypster/huginn/internal/backend"
)

// ghAvailable returns true if the `gh` CLI is in PATH.
func ghAvailable() bool {
	_, err := exec.LookPath("gh")
	return err == nil
}

// ghBase is embedded in all gh CLI tool structs to share GHPath and command helpers.
type ghBase struct {
	GHPath string // absolute path to gh binary, resolved before PATH shims
}

func (b *ghBase) command(ctx context.Context, args ...string) *exec.Cmd {
	path := b.GHPath
	if path == "" {
		path = "gh" // fallback if not set (no shims active)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	if sessionEnv := session.EnvFrom(ctx); len(sessionEnv) > 0 {
		cmd.Env = mergeEnv(os.Environ(), sessionEnv)
	}
	return cmd
}

// runGHCmd runs an exec.Cmd built via ghBase.command and returns (stdout, stderr, error).
func runGHCmd(cmd *exec.Cmd) (string, string, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// NewGHPRListTool constructs a GHPRListTool with the given absolute gh binary path.
// Provided for use in tests that cannot use composite literals with unexported embedded types.
func NewGHPRListTool(ghPath string) *GHPRListTool {
	return &GHPRListTool{ghBase: ghBase{GHPath: ghPath}}
}

// --- gh_pr_list ---

type GHPRListTool struct{ ghBase }

func (t *GHPRListTool) Name() string             { return "gh_pr_list" }
func (t *GHPRListTool) Description() string      { return "List open pull requests using the gh CLI." }
func (t *GHPRListTool) Permission() PermissionLevel { return PermRead }
func (t *GHPRListTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "gh_pr_list",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"state": {Type: "string", Description: "Filter by state: open (default), closed, merged, all"},
					"limit": {Type: "integer", Description: "Max results (default 30)"},
				},
			},
		},
	}
}
func (t *GHPRListTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	state := "open"
	if s, ok := args["state"].(string); ok && s != "" {
		state = s
	}
	limit := "30"
	if l, ok := args["limit"]; ok {
		switch v := l.(type) {
		case float64:
			limit = fmt.Sprintf("%d", int(v))
		case int:
			limit = fmt.Sprintf("%d", v)
		}
	}
	cmd := t.command(ctx, "pr", "list",
		"--state", state,
		"--limit", limit,
		"--json", "number,title,state,url,headRefName,author",
	)
	stdout, stderr, err := runGHCmd(cmd)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("gh pr list: %v\n%s", err, stderr)}
	}
	return ToolResult{Output: stdout}
}

// --- gh_pr_view ---

type GHPRViewTool struct{ ghBase }

func (t *GHPRViewTool) Name() string             { return "gh_pr_view" }
func (t *GHPRViewTool) Description() string      { return "View a pull request by number using the gh CLI." }
func (t *GHPRViewTool) Permission() PermissionLevel { return PermRead }
func (t *GHPRViewTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "gh_pr_view",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"number"},
				Properties: map[string]backend.ToolProperty{
					"number": {Type: "integer", Description: "PR number"},
				},
			},
		},
	}
}
func (t *GHPRViewTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	num, ok := intArg(args, "number")
	if !ok {
		return ToolResult{IsError: true, Error: "gh_pr_view: 'number' argument required"}
	}
	cmd := t.command(ctx, "pr", "view", fmt.Sprintf("%d", num),
		"--json", "number,title,state,url,body,headRefName,baseRefName,author,reviews,comments",
	)
	stdout, stderr, err := runGHCmd(cmd)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("gh pr view: %v\n%s", err, stderr)}
	}
	return ToolResult{Output: stdout}
}

// --- gh_pr_diff ---

type GHPRDiffTool struct{ ghBase }

func (t *GHPRDiffTool) Name() string             { return "gh_pr_diff" }
func (t *GHPRDiffTool) Description() string      { return "Show the diff for a pull request." }
func (t *GHPRDiffTool) Permission() PermissionLevel { return PermRead }
func (t *GHPRDiffTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "gh_pr_diff",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"number"},
				Properties: map[string]backend.ToolProperty{
					"number": {Type: "integer", Description: "PR number"},
				},
			},
		},
	}
}
func (t *GHPRDiffTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	num, ok := intArg(args, "number")
	if !ok {
		return ToolResult{IsError: true, Error: "gh_pr_diff: 'number' argument required"}
	}
	cmd := t.command(ctx, "pr", "diff", fmt.Sprintf("%d", num))
	stdout, stderr, err := runGHCmd(cmd)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("gh pr diff: %v\n%s", err, stderr)}
	}
	return ToolResult{Output: truncate(stdout, maxOutputBytes)}
}

// --- gh_pr_create ---

type GHPRCreateTool struct{ ghBase }

func (t *GHPRCreateTool) Name() string             { return "gh_pr_create" }
func (t *GHPRCreateTool) Description() string      { return "Create a new pull request using the gh CLI." }
func (t *GHPRCreateTool) Permission() PermissionLevel { return PermWrite }
func (t *GHPRCreateTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "gh_pr_create",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"title", "body"},
				Properties: map[string]backend.ToolProperty{
					"title": {Type: "string", Description: "PR title"},
					"body":  {Type: "string", Description: "PR body (markdown)"},
					"draft": {Type: "boolean", Description: "Create as draft PR"},
					"base":  {Type: "string", Description: "Base branch (default: repo default branch)"},
				},
			},
		},
	}
}
func (t *GHPRCreateTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	title, _ := args["title"].(string)
	body, _ := args["body"].(string)
	if strings.TrimSpace(title) == "" {
		return ToolResult{IsError: true, Error: "gh_pr_create: 'title' argument required"}
	}
	ghArgs := []string{"pr", "create", "--title", title, "--body", body}
	if draft, _ := args["draft"].(bool); draft {
		ghArgs = append(ghArgs, "--draft")
	}
	if base, _ := args["base"].(string); base != "" {
		ghArgs = append(ghArgs, "--base", base)
	}
	cmd := t.command(ctx, ghArgs...)
	stdout, stderr, err := runGHCmd(cmd)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("gh pr create: %v\n%s", err, stderr)}
	}
	return ToolResult{Output: strings.TrimSpace(stdout)}
}

// --- gh_issue_list ---

type GHIssueListTool struct{ ghBase }

func (t *GHIssueListTool) Name() string             { return "gh_issue_list" }
func (t *GHIssueListTool) Description() string      { return "List issues using the gh CLI." }
func (t *GHIssueListTool) Permission() PermissionLevel { return PermRead }
func (t *GHIssueListTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "gh_issue_list",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"state": {Type: "string", Description: "Filter: open (default), closed, all"},
					"limit": {Type: "integer", Description: "Max results (default 30)"},
					"label": {Type: "string", Description: "Filter by label"},
				},
			},
		},
	}
}
func (t *GHIssueListTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	state := "open"
	if s, ok := args["state"].(string); ok && s != "" {
		state = s
	}
	limit := "30"
	if l, ok := args["limit"]; ok {
		switch v := l.(type) {
		case float64:
			limit = fmt.Sprintf("%d", int(v))
		case int:
			limit = fmt.Sprintf("%d", v)
		}
	}
	ghArgs := []string{"issue", "list",
		"--state", state,
		"--limit", limit,
		"--json", "number,title,state,url,labels,assignees",
	}
	if label, _ := args["label"].(string); label != "" {
		ghArgs = append(ghArgs, "--label", label)
	}
	cmd := t.command(ctx, ghArgs...)
	stdout, stderr, err := runGHCmd(cmd)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("gh issue list: %v\n%s", err, stderr)}
	}
	return ToolResult{Output: stdout}
}

// --- gh_issue_view ---

type GHIssueViewTool struct{ ghBase }

func (t *GHIssueViewTool) Name() string             { return "gh_issue_view" }
func (t *GHIssueViewTool) Description() string      { return "View an issue by number using the gh CLI." }
func (t *GHIssueViewTool) Permission() PermissionLevel { return PermRead }
func (t *GHIssueViewTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "gh_issue_view",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"number"},
				Properties: map[string]backend.ToolProperty{
					"number": {Type: "integer", Description: "Issue number"},
				},
			},
		},
	}
}
func (t *GHIssueViewTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	num, ok := intArg(args, "number")
	if !ok {
		return ToolResult{IsError: true, Error: "gh_issue_view: 'number' argument required"}
	}
	cmd := t.command(ctx, "issue", "view", fmt.Sprintf("%d", num),
		"--json", "number,title,state,url,body,labels,assignees,comments",
	)
	stdout, stderr, err := runGHCmd(cmd)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("gh issue view: %v\n%s", err, stderr)}
	}
	return ToolResult{Output: stdout}
}

// --- gh_issue_create ---

type GHIssueCreateTool struct{ ghBase }

func (t *GHIssueCreateTool) Name() string             { return "gh_issue_create" }
func (t *GHIssueCreateTool) Description() string      { return "Create a new issue using the gh CLI." }
func (t *GHIssueCreateTool) Permission() PermissionLevel { return PermWrite }
func (t *GHIssueCreateTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "gh_issue_create",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"title"},
				Properties: map[string]backend.ToolProperty{
					"title":    {Type: "string", Description: "Issue title"},
					"body":     {Type: "string", Description: "Issue body (markdown)"},
					"label":    {Type: "string", Description: "Label to apply"},
					"assignee": {Type: "string", Description: "Assignee login"},
				},
			},
		},
	}
}
func (t *GHIssueCreateTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	title, _ := args["title"].(string)
	if strings.TrimSpace(title) == "" {
		return ToolResult{IsError: true, Error: "gh_issue_create: 'title' argument required"}
	}
	body, _ := args["body"].(string)
	ghArgs := []string{"issue", "create", "--title", title, "--body", body}
	if label, _ := args["label"].(string); label != "" {
		ghArgs = append(ghArgs, "--label", label)
	}
	if assignee, _ := args["assignee"].(string); assignee != "" {
		ghArgs = append(ghArgs, "--assignee", assignee)
	}
	cmd := t.command(ctx, ghArgs...)
	stdout, stderr, err := runGHCmd(cmd)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("gh issue create: %v\n%s", err, stderr)}
	}
	return ToolResult{Output: strings.TrimSpace(stdout)}
}

// intArg extracts an integer argument from args (handles float64 from JSON decode).
func intArg(args map[string]any, key string) (int, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}
