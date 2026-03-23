package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initGitRepo creates a temporary git repository for testing.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@huginn.test"},
		{"config", "user.name", "Huginn Test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// Create initial commit so we can create branches.
	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# test"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "."},
		{"commit", "-m", "initial"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestGitWorktreeCreateTool_Name(t *testing.T) {
	tool := &GitWorktreeCreateTool{SandboxRoot: "/tmp"}
	if tool.Name() != "git_worktree_create" {
		t.Errorf("Name() = %q", tool.Name())
	}
}

func TestGitWorktreeRemoveTool_Name(t *testing.T) {
	tool := &GitWorktreeRemoveTool{SandboxRoot: "/tmp"}
	if tool.Name() != "git_worktree_remove" {
		t.Errorf("Name() = %q", tool.Name())
	}
}

func TestGitWorktreeTools_PermWrite(t *testing.T) {
	if (&GitWorktreeCreateTool{}).Permission() != PermWrite {
		t.Error("create should be PermWrite")
	}
	if (&GitWorktreeRemoveTool{}).Permission() != PermWrite {
		t.Error("remove should be PermWrite")
	}
}

func TestGitWorktreeCreateTool_MissingBranchName(t *testing.T) {
	tool := &GitWorktreeCreateTool{SandboxRoot: "/tmp"}
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for missing branch_name")
	}
	if !strings.Contains(result.Error, "branch_name") {
		t.Errorf("error should mention branch_name, got %q", result.Error)
	}
}

func TestGitWorktreeRemoveTool_MissingPath(t *testing.T) {
	tool := &GitWorktreeRemoveTool{SandboxRoot: "/tmp"}
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for missing path")
	}
	if !strings.Contains(result.Error, "path") {
		t.Errorf("error should mention path, got %q", result.Error)
	}
}

func TestGitWorktreeCreateTool_PathTraversalInBranch(t *testing.T) {
	tool := &GitWorktreeCreateTool{SandboxRoot: "/tmp/repo"}
	result := tool.Execute(context.Background(), map[string]any{
		"branch_name": "../../evil",
	})
	if !result.IsError {
		t.Error("expected error for path traversal in branch_name")
	}
}

func TestGitWorktreeCreateTool_NotInGitRepo(t *testing.T) {
	// Use a temp dir that is NOT a git repo.
	dir := t.TempDir()
	tool := &GitWorktreeCreateTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{
		"branch_name": "feature-x",
	})
	if !result.IsError {
		t.Error("expected error when not in a git repo")
	}
}

func TestGitWorktreeRemoveTool_NotInGitRepo(t *testing.T) {
	dir := t.TempDir()
	tool := &GitWorktreeRemoveTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{
		"path": "/nonexistent/path",
	})
	if !result.IsError {
		t.Error("expected error when not in a git repo")
	}
}

// Integration tests that require git.

func TestGitWorktreeCreateTool_Integration(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	repoDir := initGitRepo(t)

	tool := &GitWorktreeCreateTool{SandboxRoot: repoDir}
	result := tool.Execute(context.Background(), map[string]any{
		"branch_name": "test-feature",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "test-feature") {
		t.Errorf("output should mention branch name, got %q", result.Output)
	}
	// Verify metadata.
	if result.Metadata["branch"] != "test-feature" {
		t.Errorf("metadata branch = %v, want test-feature", result.Metadata["branch"])
	}
	wtPath, _ := result.Metadata["path"].(string)
	if wtPath == "" {
		t.Error("metadata should contain path")
	}

	// Cleanup: remove the worktree we just created.
	removeTool := &GitWorktreeRemoveTool{SandboxRoot: repoDir}
	removeResult := removeTool.Execute(context.Background(), map[string]any{
		"path":  wtPath,
		"force": true,
	})
	if removeResult.IsError {
		t.Errorf("cleanup failed: %s", removeResult.Error)
	}
}

func TestGitWorktreeCreateTool_DefaultPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	repoDir := initGitRepo(t)
	repoName := filepath.Base(repoDir)

	tool := &GitWorktreeCreateTool{SandboxRoot: repoDir}
	result := tool.Execute(context.Background(), map[string]any{
		"branch_name": "auto-path",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	wtPath, _ := result.Metadata["path"].(string)
	expectedSuffix := repoName + "-auto-path"
	if !strings.HasSuffix(wtPath, expectedSuffix) {
		t.Errorf("default path %q should end with %q", wtPath, expectedSuffix)
	}

	// Cleanup.
	(&GitWorktreeRemoveTool{SandboxRoot: repoDir}).Execute(
		context.Background(),
		map[string]any{"path": wtPath, "force": true},
	)
}

func TestGitWorktreeCreateTool_ExplicitPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	repoDir := initGitRepo(t)
	customPath := filepath.Join(t.TempDir(), "my-worktree")

	tool := &GitWorktreeCreateTool{SandboxRoot: repoDir}
	result := tool.Execute(context.Background(), map[string]any{
		"branch_name": "explicit-branch",
		"path":        customPath,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	wtPath, _ := result.Metadata["path"].(string)
	absCustom, _ := filepath.Abs(customPath)
	if wtPath != absCustom {
		t.Errorf("path = %q, want %q", wtPath, absCustom)
	}

	// Cleanup.
	(&GitWorktreeRemoveTool{SandboxRoot: repoDir}).Execute(
		context.Background(),
		map[string]any{"path": wtPath, "force": true},
	)
}

func TestRegisterWorktreeTools(t *testing.T) {
	reg := NewRegistry()
	RegisterWorktreeTools(reg, "/tmp/repo")

	if _, ok := reg.Get("git_worktree_create"); !ok {
		t.Error("git_worktree_create not registered")
	}
	if _, ok := reg.Get("git_worktree_remove"); !ok {
		t.Error("git_worktree_remove not registered")
	}
}
