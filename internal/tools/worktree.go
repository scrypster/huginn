package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/scrypster/huginn/internal/backend"
)

// runGit runs a native git command in workdir and returns (stdout, stderr, error).
// Uses native git binary, NOT go-git, to avoid known worktree reliability issues.
func runGit(ctx context.Context, workdir string, args ...string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workdir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// --- git_worktree_create ---

// GitWorktreeCreateTool creates a new git worktree with a new branch.
type GitWorktreeCreateTool struct {
	SandboxRoot string
}

func (t *GitWorktreeCreateTool) Name() string { return "git_worktree_create" }
func (t *GitWorktreeCreateTool) Description() string {
	return "Create a new git worktree with a new branch. " +
		"Path defaults to ../{repo-name}-{branch} if not specified."
}
func (t *GitWorktreeCreateTool) Permission() PermissionLevel { return PermWrite }
func (t *GitWorktreeCreateTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "git_worktree_create",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"branch_name"},
				Properties: map[string]backend.ToolProperty{
					"branch_name": {Type: "string", Description: "Name for the new branch"},
					"path":        {Type: "string", Description: "Destination path for the worktree (optional)"},
				},
			},
		},
	}
}

func (t *GitWorktreeCreateTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	branch, ok := args["branch_name"].(string)
	if !ok || strings.TrimSpace(branch) == "" {
		return ToolResult{IsError: true, Error: "git_worktree_create: 'branch_name' argument required"}
	}
	// Sanitize branch name: reject path traversal.
	if strings.Contains(branch, "..") {
		return ToolResult{IsError: true, Error: "git_worktree_create: invalid branch_name (contains '..')"}
	}

	wtPath, _ := args["path"].(string)
	if strings.TrimSpace(wtPath) == "" {
		repoName := filepath.Base(t.SandboxRoot)
		wtPath = filepath.Join(filepath.Dir(t.SandboxRoot), repoName+"-"+branch)
	}

	// Resolve to absolute path.
	absPath, err := filepath.Abs(wtPath)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("git_worktree_create: resolve path: %v", err)}
	}

	stdout, stderr, err := runGit(ctx, t.SandboxRoot, "worktree", "add", absPath, "-b", branch)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("git worktree add: %v\n%s", err, strings.TrimSpace(stderr))}
	}
	out := strings.TrimSpace(stdout)
	if out == "" {
		out = strings.TrimSpace(stderr) // git worktree add often writes to stderr on success
	}
	return ToolResult{
		Output:   fmt.Sprintf("Worktree created at: %s\n%s", absPath, out),
		Metadata: map[string]any{"path": absPath, "branch": branch},
	}
}

// --- git_worktree_remove ---

// GitWorktreeRemoveTool removes a git worktree.
type GitWorktreeRemoveTool struct {
	SandboxRoot string
}

func (t *GitWorktreeRemoveTool) Name() string { return "git_worktree_remove" }
func (t *GitWorktreeRemoveTool) Description() string {
	return "Remove a git worktree by path. Use force=true to remove even if there are uncommitted changes."
}
func (t *GitWorktreeRemoveTool) Permission() PermissionLevel { return PermWrite }
func (t *GitWorktreeRemoveTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "git_worktree_remove",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"path"},
				Properties: map[string]backend.ToolProperty{
					"path":  {Type: "string", Description: "Absolute path of the worktree to remove"},
					"force": {Type: "boolean", Description: "Force removal even if dirty (default false)"},
				},
			},
		},
	}
}

func (t *GitWorktreeRemoveTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	wtPath, ok := args["path"].(string)
	if !ok || strings.TrimSpace(wtPath) == "" {
		return ToolResult{IsError: true, Error: "git_worktree_remove: 'path' argument required"}
	}

	// Resolve to absolute.
	absPath, err := filepath.Abs(wtPath)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("git_worktree_remove: resolve path: %v", err)}
	}

	gitArgs := []string{"worktree", "remove", absPath}
	if force, _ := args["force"].(bool); force {
		gitArgs = append(gitArgs, "--force")
	}

	stdout, stderr, err := runGit(ctx, t.SandboxRoot, gitArgs...)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("git worktree remove: %v\n%s", err, strings.TrimSpace(stderr))}
	}
	out := strings.TrimSpace(stdout + " " + stderr)
	if out == "" || out == " " {
		out = fmt.Sprintf("Worktree at %s removed.", absPath)
	}
	return ToolResult{Output: out}
}
