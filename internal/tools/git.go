package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/scrypster/huginn/internal/backend"
)

func openRepo(absPath string) (*git.Repository, error) {
	return git.PlainOpenWithOptions(absPath, &git.PlainOpenOptions{DetectDotGit: true})
}

// --- git_status ---

type GitStatusTool struct {
	SandboxRoot string
}

func (t *GitStatusTool) Name() string { return "git_status" }

func (t *GitStatusTool) Description() string { return "Show git working tree status" }

func (t *GitStatusTool) Permission() PermissionLevel { return PermRead }

func (t *GitStatusTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "git_status",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"path": {Type: "string", Description: "Path within project (default: root)"},
				},
			},
		},
	}
}

func (t *GitStatusTool) Execute(_ context.Context, args map[string]any) ToolResult {
	target := t.SandboxRoot
	if p, ok := args["path"].(string); ok && p != "" {
		resolved, err := ResolveSandboxed(t.SandboxRoot, p)
		if err != nil {
			return ToolResult{IsError: true, Error: err.Error()}
		}
		target = resolved
	}

	r, err := openRepo(target)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("not a git repo: %v", err)}
	}

	wt, err := r.Worktree()
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}

	status, err := wt.Status()
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}

	if status.IsClean() {
		return ToolResult{Output: "nothing to commit, working tree clean"}
	}

	var sb strings.Builder
	for path, s := range status {
		fmt.Fprintf(&sb, "%c%c %s\n", s.Staging, s.Worktree, path)
	}
	return ToolResult{Output: sb.String()}
}

// --- git_log ---

type GitLogTool struct {
	SandboxRoot string
}

func (t *GitLogTool) Name() string { return "git_log" }

func (t *GitLogTool) Description() string { return "Show git commit history" }

func (t *GitLogTool) Permission() PermissionLevel { return PermRead }

func (t *GitLogTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "git_log",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"n": {Type: "integer", Description: "Number of commits to show (default 10)"},
				},
			},
		},
	}
}

func (t *GitLogTool) Execute(_ context.Context, args map[string]any) ToolResult {
	n := 10
	if v, ok := args["n"]; ok {
		switch x := v.(type) {
		case float64:
			n = int(x)
		case int:
			n = x
		}
	}
	if n <= 0 || n > 100 {
		n = 10
	}

	r, err := openRepo(t.SandboxRoot)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("not a git repo: %v", err)}
	}

	log, err := r.Log(&git.LogOptions{})
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}

	var sb strings.Builder
	count := 0
	err = log.ForEach(func(c *object.Commit) error {
		if count >= n {
			return fmt.Errorf("stop")
		}
		subject := strings.SplitN(c.Message, "\n", 2)[0]
		fmt.Fprintf(&sb, "%s %s %s\n", c.Hash.String()[:7], c.Author.When.Format("2006-01-02"), subject)
		count++
		return nil
	})
	_ = err // "stop" error is expected

	return ToolResult{Output: sb.String()}
}

// --- git_diff ---

type GitDiffTool struct {
	SandboxRoot string
}

func (t *GitDiffTool) Name() string { return "git_diff" }

func (t *GitDiffTool) Description() string { return "Show git diff of working changes" }

func (t *GitDiffTool) Permission() PermissionLevel { return PermRead }

func (t *GitDiffTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "git_diff",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"path": {Type: "string", Description: "Optional file path to diff"},
				},
			},
		},
	}
}

func (t *GitDiffTool) Execute(_ context.Context, args map[string]any) ToolResult {
	r, err := openRepo(t.SandboxRoot)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("not a git repo: %v", err)}
	}

	wt, err := r.Worktree()
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}

	status, err := wt.Status()
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}

	if status.IsClean() {
		return ToolResult{Output: "no changes"}
	}

	const maxDiffOutputBytes = 100 * 1024
	var sb strings.Builder
	for p, s := range status {
		if s.Worktree != git.Unmodified || s.Staging != git.Unmodified {
			fmt.Fprintf(&sb, "%c%c %s\n", s.Staging, s.Worktree, p)
			if sb.Len() > maxDiffOutputBytes {
				sb.WriteString("... [truncated]\n")
				break
			}
		}
	}
	return ToolResult{Output: sb.String()}
}

// --- git_branch ---

type GitBranchTool struct {
	SandboxRoot string
}

func (t *GitBranchTool) Name() string { return "git_branch" }

func (t *GitBranchTool) Description() string { return "Manage git branches" }

func (t *GitBranchTool) Permission() PermissionLevel { return PermWrite }

func (t *GitBranchTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "git_branch",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"action"},
				Properties: map[string]backend.ToolProperty{
					"action": {Type: "string", Description: "list, create, or switch"},
					"name":   {Type: "string", Description: "Branch name for create/switch"},
				},
			},
		},
	}
}

var validBranchName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/\-]*$`)

func (t *GitBranchTool) Execute(_ context.Context, args map[string]any) ToolResult {
	action, _ := args["action"].(string)

	r, err := openRepo(t.SandboxRoot)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("not a git repo: %v", err)}
	}

	switch action {
	case "list":
		branches, err := r.Branches()
		if err != nil {
			return ToolResult{IsError: true, Error: err.Error()}
		}

		head, _ := r.Head()
		var sb strings.Builder
		branches.ForEach(func(ref *plumbing.Reference) error {
			name := ref.Name().Short()
			prefix := "  "
			if head != nil && head.Name() == ref.Name() {
				prefix = "* "
			}
			fmt.Fprintf(&sb, "%s%s\n", prefix, name)
			return nil
		})
		return ToolResult{Output: sb.String()}

	case "create", "switch":
		name, _ := args["name"].(string)
		if name == "" {
			return ToolResult{IsError: true, Error: "name required"}
		}

		if strings.Contains(name, "..") || !validBranchName.MatchString(name) {
			return ToolResult{IsError: true, Error: fmt.Sprintf("invalid branch name %q", name)}
		}

		wt, err := r.Worktree()
		if err != nil {
			return ToolResult{IsError: true, Error: err.Error()}
		}

		refName := plumbing.NewBranchReferenceName(name)
		err = wt.Checkout(&git.CheckoutOptions{Branch: refName, Create: action == "create"})
		if err != nil {
			return ToolResult{IsError: true, Error: err.Error()}
		}

		return ToolResult{Output: fmt.Sprintf("switched to branch %q", name)}
	}

	return ToolResult{IsError: true, Error: fmt.Sprintf("unknown action %q (list/create/switch)", action)}
}

// --- git_commit ---

type GitCommitTool struct {
	SandboxRoot string
}

func (t *GitCommitTool) Name() string { return "git_commit" }

func (t *GitCommitTool) Description() string { return "Stage and commit changes" }

func (t *GitCommitTool) Permission() PermissionLevel { return PermWrite }

func (t *GitCommitTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "git_commit",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"message"},
				Properties: map[string]backend.ToolProperty{
					"message": {Type: "string", Description: "Commit message"},
					"paths":   {Type: "array", Description: "Paths to stage (optional, stages all if omitted)"},
				},
			},
		},
	}
}

const maxCommitMessageBytes = 10000

func (t *GitCommitTool) Execute(_ context.Context, args map[string]any) ToolResult {
	msg, ok := args["message"].(string)
	if !ok || strings.TrimSpace(msg) == "" {
		return ToolResult{IsError: true, Error: "message required"}
	}
	if len(msg) > maxCommitMessageBytes {
		msg = msg[:maxCommitMessageBytes]
	}

	r, err := openRepo(t.SandboxRoot)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("not a git repo: %v", err)}
	}

	wt, err := r.Worktree()
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}

	root := wt.Filesystem.Root()
	if rawPaths, ok := args["paths"].([]any); ok {
		for _, p := range rawPaths {
			if path, ok := p.(string); ok {
				abs := filepath.Join(t.SandboxRoot, path)
				rel, err := filepath.Rel(root, abs)
				if err != nil {
					continue
				}
				wt.Add(rel)
			}
		}
	} else {
		wt.Add(".")
	}

	hash, err := wt.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{Name: "huginn", Email: "huginn@local", When: time.Now()},
	})
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}

	return ToolResult{Output: fmt.Sprintf("committed %s", hash.String()[:7])}
}

// --- git_blame ---

type GitBlameTool struct {
	SandboxRoot string
}

func (t *GitBlameTool) Name() string { return "git_blame" }

func (t *GitBlameTool) Description() string { return "Show git blame for a file" }

func (t *GitBlameTool) Permission() PermissionLevel { return PermRead }

func (t *GitBlameTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "git_blame",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"path"},
				Properties: map[string]backend.ToolProperty{
					"path": {Type: "string", Description: "File path to blame"},
				},
			},
		},
	}
}

func (t *GitBlameTool) Execute(_ context.Context, args map[string]any) ToolResult {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return ToolResult{IsError: true, Error: "path required"}
	}

	resolved, err := ResolveSandboxed(t.SandboxRoot, path)
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}

	r, err := openRepo(resolved)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("not a git repo: %v", err)}
	}

	head, err := r.Head()
	if err != nil {
		return ToolResult{IsError: true, Error: "no commits yet"}
	}

	commit, err := r.CommitObject(head.Hash())
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}

	wt, _ := r.Worktree()
	root := wt.Filesystem.Root()
	relPath, err := filepath.Rel(root, resolved)
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}

	blame, err := git.Blame(commit, relPath)
	if err != nil {
		return ToolResult{IsError: true, Error: err.Error()}
	}

	const maxBlameOutputBytes = 100 * 1024
	var sb strings.Builder
	for i, line := range blame.Lines {
		fmt.Fprintf(&sb, "%d | %s | %s | %s\n", i+1, line.Hash.String()[:7], line.Author, line.Text)
		if sb.Len() > maxBlameOutputBytes {
			sb.WriteString("... [truncated]\n")
			break
		}
	}
	return ToolResult{Output: sb.String()}
}

// --- git_stash ---

type GitStashTool struct {
	SandboxRoot string
}

func (t *GitStashTool) Name() string { return "git_stash" }

func (t *GitStashTool) Description() string { return "Stash or pop git changes" }

func (t *GitStashTool) Permission() PermissionLevel { return PermWrite }

func (t *GitStashTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "git_stash",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"action"},
				Properties: map[string]backend.ToolProperty{
					"action": {Type: "string", Description: "push or pop"},
				},
			},
		},
	}
}

func (t *GitStashTool) Execute(_ context.Context, args map[string]any) ToolResult {
	action, _ := args["action"].(string)

	switch action {
	case "push":
		r, err := openRepo(t.SandboxRoot)
		if err != nil {
			return ToolResult{IsError: true, Error: fmt.Sprintf("not a git repo: %v", err)}
		}

		wt, _ := r.Worktree()
		err = wt.Reset(&git.ResetOptions{Mode: git.HardReset})
		if err != nil {
			return ToolResult{IsError: true, Error: err.Error()}
		}

		return ToolResult{Output: "stashed changes (hard reset)"}

	case "pop":
		return ToolResult{Output: "stash pop not implemented; use git_stash push to save state"}
	}

	return ToolResult{IsError: true, Error: fmt.Sprintf("unknown action %q (push/pop)", action)}
}
