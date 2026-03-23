package tools_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/scrypster/huginn/internal/tools"
)

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	r, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	w, err := r.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello\n"), 0644)
	w.Add("hello.txt")
	w.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@test.com", When: time.Now()},
	})
	return dir
}

func TestGitStatusTool_Name(t *testing.T) {
	tool := &tools.GitStatusTool{SandboxRoot: t.TempDir()}
	if tool.Name() != "git_status" {
		t.Errorf("expected git_status, got %q", tool.Name())
	}
}

func TestGitStatusTool_Permission(t *testing.T) {
	tool := &tools.GitStatusTool{SandboxRoot: t.TempDir()}
	if tool.Permission() != tools.PermRead {
		t.Error("expected PermRead")
	}
}

func TestGitStatusTool_CleanRepo(t *testing.T) {
	dir := initTestRepo(t)
	tool := &tools.GitStatusTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), nil)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Output == "" {
		t.Error("expected non-empty output")
	}
}

func TestGitLogTool_Name(t *testing.T) {
	tool := &tools.GitLogTool{SandboxRoot: t.TempDir()}
	if tool.Name() != "git_log" {
		t.Errorf("expected git_log")
	}
}

func TestGitLogTool_Execute_ReturnsCommits(t *testing.T) {
	dir := initTestRepo(t)
	tool := &tools.GitLogTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{"n": float64(5)})
	if result.IsError {
		t.Fatalf("error: %s", result.Error)
	}
	if result.Output == "" {
		t.Error("expected commits in output")
	}
}

func TestGitBranchTool_Name(t *testing.T) {
	tool := &tools.GitBranchTool{SandboxRoot: t.TempDir()}
	if tool.Name() != "git_branch" {
		t.Errorf("expected git_branch")
	}
}

func TestGitBranchTool_List(t *testing.T) {
	dir := initTestRepo(t)
	tool := &tools.GitBranchTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{"action": "list"})
	if result.IsError {
		t.Fatalf("error: %s", result.Error)
	}
}

func TestGitCommitTool_EmptyMessage_Error(t *testing.T) {
	dir := initTestRepo(t)
	tool := &tools.GitCommitTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{"message": ""})
	if !result.IsError {
		t.Error("expected error for empty message")
	}
}

func TestGitStashTool_InvalidAction(t *testing.T) {
	dir := initTestRepo(t)
	tool := &tools.GitStashTool{SandboxRoot: dir}
	result := tool.Execute(context.Background(), map[string]any{"action": "invalid"})
	if !result.IsError {
		t.Error("expected error for invalid action")
	}
}
