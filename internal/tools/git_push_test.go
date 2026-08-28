package tools

// G3: git_push — native `git push`, setting upstream automatically the first
// time a branch is pushed (worktree_test.go's initGitRepo gives us a
// hermetic native-git repo, independent of the host's global git config).

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// initBareRemote creates a bare git repo suitable as a push target.
func initBareRemote(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "--bare", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}
	return dir
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
}

func TestGitPushTool_Name(t *testing.T) {
	tool := &GitPushTool{SandboxRoot: "/tmp"}
	if tool.Name() != "git_push" {
		t.Errorf("expected git_push, got %q", tool.Name())
	}
}

func TestGitPushTool_Permission(t *testing.T) {
	tool := &GitPushTool{SandboxRoot: "/tmp"}
	if tool.Permission() != PermWrite {
		t.Error("expected PermWrite")
	}
}

func TestGitPushTool_NotGitRepo(t *testing.T) {
	tool := &GitPushTool{SandboxRoot: t.TempDir()}
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for non-git dir")
	}
}

func TestGitPushTool_NoRemote_Errors(t *testing.T) {
	requireGit(t)
	dir := initGitRepo(t)
	tool := &GitPushTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error pushing with no remote configured")
	}
}

func TestGitPushTool_SetsUpstreamOnFirstPush(t *testing.T) {
	requireGit(t)
	dir := initGitRepo(t)
	remote := initBareRemote(t)

	if _, stderr, err := runGit(context.Background(), dir, "remote", "add", "origin", remote); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, stderr)
	}

	// No upstream configured yet.
	if _, _, err := runGit(context.Background(), dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err == nil {
		t.Fatal("test setup: branch should not have an upstream yet")
	}

	tool := &GitPushTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	// Upstream should now be configured.
	out, _, err := runGit(context.Background(), dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		t.Fatalf("expected upstream to be set after push: %v", err)
	}
	if !strings.Contains(out, "origin/") {
		t.Errorf("expected upstream tracking origin, got: %s", out)
	}
}

func TestGitPushTool_SecondPush_NoDashU(t *testing.T) {
	requireGit(t)
	dir := initGitRepo(t)
	remote := initBareRemote(t)
	if _, stderr, err := runGit(context.Background(), dir, "remote", "add", "origin", remote); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, stderr)
	}

	tool := &GitPushTool{SandboxRoot: dir}
	if result := tool.Execute(context.Background(), map[string]any{}); result.IsError {
		t.Fatalf("first push failed: %s", result.Error)
	}

	// A second push with nothing new should still succeed (git reports
	// "Everything up-to-date" on stderr, not an error).
	result := tool.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Errorf("second push should succeed, got error: %s", result.Error)
	}
}

func TestGitPushTool_CustomRemote(t *testing.T) {
	requireGit(t)
	dir := initGitRepo(t)
	remote := initBareRemote(t)
	if _, stderr, err := runGit(context.Background(), dir, "remote", "add", "upstream", remote); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, stderr)
	}

	tool := &GitPushTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{"remote": "upstream"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
}
