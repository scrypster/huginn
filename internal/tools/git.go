package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

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

func (t *GitCommitTool) Description() string {
	return "Stage and commit changes. Stages tracked modifications/deletions (git add -u); " +
		"new untracked files are committed only when named in `paths`."
}

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
					"paths": {Type: "array", Description: "Paths to stage. NEW (untracked) files are ONLY committed if named here. " +
						"When omitted, only already-tracked files that you modified or deleted are staged (git add -u) — " +
						"any file you just created is left out."},
				},
			},
		},
	}
}

const maxCommitMessageBytes = 10000

// Execute stages and commits changes using the NATIVE git binary — not go-git.
// This matters: go-git can't run the user's hooks (pre-commit, commit-msg,
// pre-push) or honor their commit-signing config, and the old implementation
// hardcoded a fake "huginn <huginn@local>" author, misattributing every
// commit. Shelling out to `git` gives us the user's real identity, hooks, and
// signing setup for free — exactly what committing from their own terminal
// would do.
//
// Staging is deliberately narrow: `git add -u` (tracked, modified/deleted
// files only) plus any explicitly named `paths` — never a blanket `git add .`
// / `wt.Add(".")`, which would happily stage scratch files, secrets, or any
// other untracked junk sitting in the working tree.
func (t *GitCommitTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	msg, ok := args["message"].(string)
	if !ok || strings.TrimSpace(msg) == "" {
		return ToolResult{IsError: true, Error: "message required"}
	}
	if len(msg) > maxCommitMessageBytes {
		msg = msg[:maxCommitMessageBytes]
	}

	if _, err := openRepo(t.SandboxRoot); err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("not a git repo: %v", err)}
	}

	// Stage tracked modifications/deletions unconditionally — this never
	// pulls in new untracked files, so it's always safe to run.
	if _, stderr, err := runGit(ctx, t.SandboxRoot, "add", "-u"); err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("git add -u: %v\n%s", err, strings.TrimSpace(stderr))}
	}

	// Explicitly named paths are staged individually — this is the only way
	// a brand-new (untracked) file gets included, and only when the caller
	// named it specifically.
	if rawPaths, ok := args["paths"].([]any); ok {
		for _, p := range rawPaths {
			path, ok := p.(string)
			if !ok || path == "" {
				continue
			}
			resolved, err := ResolveSandboxed(t.SandboxRoot, path)
			if err != nil {
				return ToolResult{IsError: true, Error: fmt.Sprintf("git add: %v", err)}
			}
			// ResolveSandboxed evaluates symlinks on both sides (macOS: /tmp ->
			// /private/tmp), so t.SandboxRoot itself must be resolved the same
			// way before computing the relative path — otherwise Rel produces a
			// bogus "../../.." path even for a file safely inside the sandbox.
			resolvedRoot, rootErr := filepath.EvalSymlinks(t.SandboxRoot)
			if rootErr != nil {
				resolvedRoot = t.SandboxRoot
			}
			rel, err := filepath.Rel(resolvedRoot, resolved)
			if err != nil {
				return ToolResult{IsError: true, Error: fmt.Sprintf("git add: %v", err)}
			}
			if _, stderr, err := runGit(ctx, t.SandboxRoot, "add", "--", rel); err != nil {
				return ToolResult{IsError: true, Error: fmt.Sprintf("git add %s: %v\n%s", rel, err, strings.TrimSpace(stderr))}
			}
		}
	}

	// `git commit` — no --author, no --no-verify: the user's configured
	// identity, hooks, and signing settings all apply exactly as they would
	// from a terminal. A hook rejection (e.g. a failing pre-commit lint)
	// surfaces as a normal tool error with the hook's own output.
	stdout, stderr, err := runGit(ctx, t.SandboxRoot, "commit", "-m", msg)
	if err != nil {
		combined := strings.TrimSpace(stdout + "\n" + stderr)
		return ToolResult{IsError: true, Error: fmt.Sprintf("git commit: %v\n%s", err, combined)}
	}

	// Disclose what was left out. `git add -u` never stages untracked files,
	// so a brand-new file the agent just created is silently absent from the
	// commit unless it was named in `paths`. Reporting "committed abc1234"
	// while quietly dropping the agent's own new work is the kind of
	// surprise that costs someone a file, so name the omissions explicitly.
	omitted := untrackedFilesNote(ctx, t.SandboxRoot)

	hashOut, _, err := runGit(ctx, t.SandboxRoot, "rev-parse", "--short", "HEAD")
	if err != nil || strings.TrimSpace(hashOut) == "" {
		// Commit succeeded but we couldn't resolve the hash — still report success.
		return ToolResult{Output: strings.TrimSpace(stdout) + omitted}
	}

	return ToolResult{Output: fmt.Sprintf("committed %s%s", strings.TrimSpace(hashOut), omitted)}
}

// maxReportedUntracked caps how many untracked filenames a commit result
// lists before summarising the remainder as a count.
const maxReportedUntracked = 10

// untrackedFilesNote returns a human-readable warning naming files that are
// still untracked after a commit (i.e. were NOT included), or "" when the
// working tree has no untracked files. Best-effort: any git failure yields
// "" rather than a misleading claim in either direction.
func untrackedFilesNote(ctx context.Context, root string) string {
	stdout, _, err := runGit(ctx, root, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return ""
	}
	var files []string
	for _, line := range strings.Split(stdout, "\n") {
		if f := strings.TrimSpace(line); f != "" {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return ""
	}
	shown := files
	suffix := ""
	if len(shown) > maxReportedUntracked {
		shown = shown[:maxReportedUntracked]
		suffix = fmt.Sprintf(" (+%d more)", len(files)-maxReportedUntracked)
	}
	return fmt.Sprintf(
		"\n\nNOT COMMITTED — %d untracked file(s) were left out: %s%s\n"+
			"git_commit stages tracked changes only. To include a new file, "+
			"call git_commit again with it named in `paths`.",
		len(files), strings.Join(shown, ", "), suffix)
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

func (t *GitStashTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	action, _ := args["action"].(string)

	switch action {
	case "push":
		if _, err := openRepo(t.SandboxRoot); err != nil {
			return ToolResult{IsError: true, Error: fmt.Sprintf("not a git repo: %v", err)}
		}

		stdout, stderr, err := runGit(ctx, t.SandboxRoot, "stash", "push")
		if err != nil {
			return ToolResult{IsError: true, Error: fmt.Sprintf("git stash push: %v\n%s", err, strings.TrimSpace(stderr))}
		}
		out := strings.TrimSpace(stdout)
		if out == "" {
			out = "no local changes to save"
		}
		return ToolResult{Output: out}

	case "pop":
		if _, err := openRepo(t.SandboxRoot); err != nil {
			return ToolResult{IsError: true, Error: fmt.Sprintf("not a git repo: %v", err)}
		}

		stdout, stderr, err := runGit(ctx, t.SandboxRoot, "stash", "pop")
		if err != nil {
			combined := strings.TrimSpace(stdout + "\n" + stderr)
			return ToolResult{IsError: true, Error: fmt.Sprintf("git stash pop: %v\n%s", err, combined)}
		}
		out := strings.TrimSpace(stdout)
		if out == "" {
			out = strings.TrimSpace(stderr)
		}
		return ToolResult{Output: out}
	}

	return ToolResult{IsError: true, Error: fmt.Sprintf("unknown action %q (push/pop)", action)}
}

// --- git_push ---

// GitPushTool pushes the current branch to its remote using native git.
// If the current branch has no upstream configured yet, it sets one
// (`git push -u <remote> <branch>`) instead of failing with git's default
// "no upstream branch" error.
type GitPushTool struct {
	SandboxRoot string
}

func (t *GitPushTool) Name() string { return "git_push" }
func (t *GitPushTool) Description() string {
	return "Push the current branch to its remote (native git). " +
		"Sets the upstream automatically the first time a branch is pushed."
}
func (t *GitPushTool) Permission() PermissionLevel { return PermWrite }
func (t *GitPushTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "git_push",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"remote": {Type: "string", Description: "Remote name (default: origin)"},
					"force":  {Type: "boolean", Description: "Force-push with lease (--force-with-lease). Default false."},
				},
			},
		},
	}
}

func (t *GitPushTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	if _, err := openRepo(t.SandboxRoot); err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("not a git repo: %v", err)}
	}

	remote, _ := args["remote"].(string)
	remote = strings.TrimSpace(remote)
	if remote == "" {
		remote = "origin"
	}
	// `remote` lands in an argument position (`git push -u <remote> <branch>`),
	// so a value beginning with "-" would be parsed by git as an OPTION, not a
	// remote name. git push accepts --receive-pack=<cmd> / --exec=<cmd>, which
	// git executes for a local-path remote — turning a model-chosen (or
	// prompt-injected) string into command execution. There is no shell here,
	// so this is the only injection surface, and a leading dash is never a
	// legitimate remote name.
	if strings.HasPrefix(remote, "-") {
		return ToolResult{IsError: true, Error: fmt.Sprintf("git_push: invalid remote name %q (must not begin with '-')", remote)}
	}
	force, _ := args["force"].(bool)

	branchOut, stderr, err := runGit(ctx, t.SandboxRoot, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("git rev-parse HEAD: %v\n%s", err, strings.TrimSpace(stderr))}
	}
	branch := strings.TrimSpace(branchOut)
	if branch == "" || branch == "HEAD" {
		return ToolResult{IsError: true, Error: "git_push: cannot push from a detached HEAD"}
	}

	// Does this branch already have an upstream?
	_, _, upstreamErr := runGit(ctx, t.SandboxRoot, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")

	pushArgs := []string{"push"}
	if force {
		pushArgs = append(pushArgs, "--force-with-lease")
	}
	if upstreamErr != nil {
		// No upstream configured yet — set one on this push.
		pushArgs = append(pushArgs, "-u", remote, branch)
	}

	stdout, stderr, err := runGit(ctx, t.SandboxRoot, pushArgs...)
	combined := strings.TrimSpace(stdout + "\n" + stderr)
	if err != nil {
		return ToolResult{IsError: true, Error: fmt.Sprintf("git push: %v\n%s", err, combined)}
	}
	if combined == "" {
		combined = fmt.Sprintf("pushed %s to %s", branch, remote)
	}
	return ToolResult{Output: combined}
}
