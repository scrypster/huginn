package tools

import (
	"os/exec"
	"time"
)

// RegisterBuiltins creates and registers all built-in tools with the given sandbox root.
// sandboxRoot is the project directory — tools cannot access paths outside it.
// A shared FileLockManager is created to serialize concurrent writes to the same file
// (e.g. when parallel swarm agents target the same path).
func RegisterBuiltins(reg *Registry, sandboxRoot string, bashTimeout time.Duration) {
	if bashTimeout == 0 {
		bashTimeout = 120 * time.Second
	}
	flm := NewFileLockManager()
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
func RegisterGitHubTools(reg *Registry) {
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return
	}
	base := ghBase{GHPath: ghPath}
	reg.Register(&GHPRListTool{ghBase: base})
	reg.Register(&GHPRViewTool{ghBase: base})
	reg.Register(&GHPRDiffTool{ghBase: base})
	reg.Register(&GHPRCreateTool{ghBase: base})
	reg.Register(&GHIssueListTool{ghBase: base})
	reg.Register(&GHIssueViewTool{ghBase: base})
	reg.Register(&GHIssueCreateTool{ghBase: base})
}

// GitHubCLIToolNames returns the registered names of all gh CLI tools.
// Used by main.go to tag them with the "github_cli" provider.
func GitHubCLIToolNames() []string {
	return []string{
		"gh_pr_list", "gh_pr_view", "gh_pr_diff", "gh_pr_create",
		"gh_issue_list", "gh_issue_view", "gh_issue_create",
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
		"git_status", "git_diff", "git_log", "git_blame", "git_branch", "git_commit", "git_stash",
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
	}
}
