package tools

import (
	"os/exec"
	"time"
)

// RegisterBuiltins creates and registers all built-in tools with the given sandbox root.
// sandboxRoot is the project directory — tools cannot access paths outside it.
// A fresh FileLockManager is created to serialize concurrent writes to the same file
// (e.g. when parallel swarm agents target the same path). Callers that also wire up
// the checkpoint system (init_checkpoint.go) MUST use RegisterBuiltinsWithLocker
// instead, passing the SAME *FileLockManager instance given to
// checkpoint.NewManager — otherwise write_file/edit_file and checkpoint_revert_run
// serialize against two independent lock tables and never actually exclude each
// other (A1).
func RegisterBuiltins(reg *Registry, sandboxRoot string, bashTimeout time.Duration) {
	RegisterBuiltinsWithLocker(reg, sandboxRoot, bashTimeout, NewFileLockManager())
}

// RegisterBuiltinsWithLocker is RegisterBuiltins with an explicit, caller-owned
// FileLockManager instead of a fresh one — the shared-lock-manager half of the A1
// fix. See RegisterBuiltins' doc comment.
func RegisterBuiltinsWithLocker(reg *Registry, sandboxRoot string, bashTimeout time.Duration, flm *FileLockManager) {
	if bashTimeout == 0 {
		bashTimeout = 120 * time.Second
	}
	if flm == nil {
		flm = NewFileLockManager()
	}
	reg.Register(&BashTool{SandboxRoot: sandboxRoot, Timeout: bashTimeout})
	reg.Register(&ReadFileTool{SandboxRoot: sandboxRoot})
	reg.Register(&WriteFileTool{SandboxRoot: sandboxRoot, FileLock: flm})
	reg.Register(&EditFileTool{SandboxRoot: sandboxRoot, FileLock: flm})
	reg.Register(&ListDirTool{SandboxRoot: sandboxRoot})
	reg.Register(&SearchFilesTool{SandboxRoot: sandboxRoot})
	reg.Register(&GrepTool{SandboxRoot: sandboxRoot})
}

// RegisterGitTools registers all P2 git tools with the given registry.
// Safely skipped if the repo is not a git repository.
func RegisterGitTools(reg *Registry, sandboxRoot string) {
	reg.Register(&GitStatusTool{SandboxRoot: sandboxRoot})
	reg.Register(&GitDiffTool{SandboxRoot: sandboxRoot})
	reg.Register(&GitLogTool{SandboxRoot: sandboxRoot})
	reg.Register(&GitBlameTool{SandboxRoot: sandboxRoot})
	reg.Register(&GitBranchTool{SandboxRoot: sandboxRoot})
	reg.Register(&GitCommitTool{SandboxRoot: sandboxRoot})
	reg.Register(&GitStashTool{SandboxRoot: sandboxRoot})
	reg.Register(&GitPushTool{SandboxRoot: sandboxRoot})
}

// RegisterTestsTool adds the run_tests tool. Called separately to keep builtin count at 7.
func RegisterTestsTool(reg *Registry, sandboxRoot string, timeout time.Duration) {
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	reg.Register(&RunTestsTool{SandboxRoot: sandboxRoot, Timeout: timeout})
}

// RegisterWebTools registers web_search (if apiKey non-empty) and fetch_url.
func RegisterWebTools(reg *Registry, braveAPIKey string) {
	if braveAPIKey != "" {
		reg.Register(&WebSearchTool{APIKey: braveAPIKey})
	}
	reg.Register(&FetchURLTool{})
}

// RegisterWorktreeTools registers git worktree tools.
// These shell out to the native git binary.
func RegisterWorktreeTools(reg *Registry, sandboxRoot string) {
	reg.Register(&GitWorktreeCreateTool{SandboxRoot: sandboxRoot})
	reg.Register(&GitWorktreeRemoveTool{SandboxRoot: sandboxRoot})
}

// RegisterGitHubTools registers all gh CLI tools.
// Only called if exec.LookPath("gh") succeeds.
// sandboxRoot is set as the working directory for every gh invocation —
// without it, gh runs in the process's own cwd instead of the project the
// agent is operating on, silently targeting the wrong repo/PR. Deliberately
// no pr-merge tool: humans merge.
func RegisterGitHubTools(reg *Registry, sandboxRoot string) {
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return
	}
	base := ghBase{GHPath: ghPath, SandboxRoot: sandboxRoot}
	reg.Register(&GHPRListTool{ghBase: base})
	reg.Register(&GHPRViewTool{ghBase: base})
	reg.Register(&GHPRDiffTool{ghBase: base})
	reg.Register(&GHPRCreateTool{ghBase: base, DefaultBranch: detectDefaultBranch(sandboxRoot)})
	reg.Register(&GHPRChecksTool{ghBase: base})
	reg.Register(&GHPRCommentTool{ghBase: base})
	reg.Register(&GHRunViewFailedTool{ghBase: base})
	reg.Register(&GHIssueListTool{ghBase: base})
	reg.Register(&GHIssueViewTool{ghBase: base})
	reg.Register(&GHIssueCreateTool{ghBase: base})
}

// GitHubCLIToolNames returns the registered names of all gh CLI tools.
// Used by main.go to tag them with the "github_cli" provider.
func GitHubCLIToolNames() []string {
	return []string{
		"gh_pr_list", "gh_pr_view", "gh_pr_diff", "gh_pr_create",
		"gh_pr_checks", "gh_pr_comment", "gh_run_view_failed",
		"gh_issue_list", "gh_issue_view", "gh_issue_create",
	}
}

// RegisterGitLabTools registers all glab CLI tools — the second forge belt,
// mirroring RegisterGitHubTools's gh_* set for GitLab. Only called if
// exec.LookPath("glab") succeeds. sandboxRoot is set as the working
// directory for every glab invocation, same discipline as gh_* (see
// ghBase.command) — without it glab runs in the process's own cwd instead
// of the project the agent is operating on.
//
// Deliberately no typed mr-merge tool, matching gh: humans merge.
//
// Bitbucket is a separate belt (see RegisterBitbucketTools in bitbucket.go)
// — there is no maintained official Bitbucket PR CLI to mirror gh/glab
// against, so it talks to the Bitbucket Cloud REST API directly instead of
// shelling out to a CLI.
func RegisterGitLabTools(reg *Registry, sandboxRoot string) {
	glabPath, err := exec.LookPath("glab")
	if err != nil {
		return
	}
	base := glBase{GlabPath: glabPath, SandboxRoot: sandboxRoot}
	reg.Register(&GlabMRCreateTool{glBase: base, DefaultBranch: detectDefaultBranch(sandboxRoot)})
	reg.Register(&GlabMRChecksTool{glBase: base})
	reg.Register(&GlabCIViewFailedTool{glBase: base})
	reg.Register(&GlabMRCommentTool{glBase: base})
}

// GitLabCLIToolNames returns the registered names of all glab CLI tools.
// Used by main.go to tag them with the "gitlab_cli" provider.
func GitLabCLIToolNames() []string {
	return []string{
		"glab_mr_create", "glab_mr_checks", "glab_ci_view_failed", "glab_mr_comment",
	}
}

// RegisterBitbucketTools registers the bitbucket_pr_* REST-API belt (see
// bitbucket.go). Unlike RegisterGitHubTools/RegisterGitLabTools, this belt
// is not gated on a CLI binary being present — it talks to the Bitbucket
// Cloud REST API directly, so it is always registered; a missing/expired
// Bitbucket connection surfaces as a clear per-call tool error instead of
// deciding at startup whether the tools exist at all. sandboxRoot is used
// to resolve the git remote (for workspace/repo_slug) and the current
// branch, same discipline as gh_*/glab_* (see ghBase/glBase).
func RegisterBitbucketTools(reg *Registry, sandboxRoot string, clientFunc BitbucketClientFunc) {
	base := bbBase{SandboxRoot: sandboxRoot, ClientFunc: clientFunc}
	reg.Register(&BitbucketPRCreateTool{bbBase: base, DefaultBranch: detectDefaultBranch(sandboxRoot)})
	reg.Register(&BitbucketPRViewTool{bbBase: base})
	reg.Register(&BitbucketPRChecksTool{bbBase: base})
	reg.Register(&BitbucketPRCommentTool{bbBase: base})
	reg.Register(&BitbucketPRMergeTool{bbBase: base})
}

// BitbucketToolNames returns the registered names of all bitbucket_pr_*
// tools. Used by main.go to tag them with the "bitbucket" provider.
func BitbucketToolNames() []string {
	return []string{
		"bitbucket_pr_create", "bitbucket_pr_view", "bitbucket_pr_checks",
		"bitbucket_pr_comment", "bitbucket_pr_merge",
	}
}

// BuiltinToolNames returns the names of all non-external tools that are
// registered by RegisterBuiltins, RegisterGitTools, RegisterTestsTool,
// RegisterWebTools, and RegisterWorktreeTools.
// Used by main.go to tag them with the "builtin" provider.
func BuiltinToolNames() []string {
	return []string{
		// RegisterBuiltins
		"bash", "read_file", "write_file", "edit_file", "list_dir", "search_files", "grep",
		// RegisterGitTools
		"git_status", "git_diff", "git_log", "git_blame", "git_branch", "git_commit", "git_stash", "git_push",
		// RegisterTestsTool
		"run_tests",
		// RegisterWebTools
		"web_search", "fetch_url",
		// RegisterWorktreeTools
		"git_worktree_create", "git_worktree_remove",
		// Symbol tools (registered separately in main.go)
		"find_definition", "list_symbols",
		// Notes tool
		"update_memory",
		// Workflow drop-dir writer (agents author pipelines as files)
		"write_workflow",
	}
}
