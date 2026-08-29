package tools_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/checkpoint"
	"github.com/scrypster/huginn/internal/tools"
)

func initCheckpointToolsTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--quiet", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "--quiet", "-m", "initial")
	return dir
}

func TestRegisterCheckpointTools_SchemasValid(t *testing.T) {
	dir := initCheckpointToolsTestRepo(t)
	mgr, err := checkpoint.NewManager(context.Background(), t.TempDir(), dir, tools.NewFileLockManager())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Close()

	reg := tools.NewRegistry()
	tools.RegisterCheckpointTools(reg, mgr)

	wantPerm := map[string]tools.PermissionLevel{
		"checkpoint_list":       tools.PermRead,
		"checkpoint_diff_run":   tools.PermRead,
		"checkpoint_revert_run": tools.PermWrite,
		"checkpoint_gc":         tools.PermWrite,
	}
	for name, perm := range wantPerm {
		tool, ok := reg.Get(name)
		if !ok {
			t.Errorf("tool %q not registered", name)
			continue
		}
		if tool.Permission() != perm {
			t.Errorf("%s.Permission() = %v, want %v", name, tool.Permission(), perm)
		}
		schema := tool.Schema()
		if schema.Function.Name != name {
			t.Errorf("%s schema name = %q, want %q", name, schema.Function.Name, name)
		}
		if schema.Function.Description == "" {
			t.Errorf("%s schema has empty description", name)
		}
	}
}

func TestCheckpointListTool_Execute_EmptyThenPopulated(t *testing.T) {
	dir := initCheckpointToolsTestRepo(t)
	ctx := context.Background()
	mgr, err := checkpoint.NewManager(ctx, t.TempDir(), dir, tools.NewFileLockManager())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Close()

	listTool := &tools.CheckpointListTool{Manager: mgr}
	result := listTool.Execute(ctx, map[string]any{})
	if result.IsError {
		t.Fatalf("checkpoint_list on empty ledger errored: %s", result.Error)
	}

	if _, err := mgr.BeginRun(ctx, "t1", "coder", "task"); err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	if _, err := mgr.EndRun(ctx, "t1"); err != nil {
		t.Fatalf("EndRun: %v", err)
	}

	result = listTool.Execute(ctx, map[string]any{})
	if result.IsError {
		t.Fatalf("checkpoint_list errored: %s", result.Error)
	}
	if result.Output == "" {
		t.Fatal("checkpoint_list output empty after a run was recorded")
	}
}

func TestCheckpointRevertRunTool_MissingThreadID(t *testing.T) {
	dir := initCheckpointToolsTestRepo(t)
	ctx := context.Background()
	mgr, err := checkpoint.NewManager(ctx, t.TempDir(), dir, tools.NewFileLockManager())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Close()

	tool := &tools.CheckpointRevertRunTool{Manager: mgr}
	result := tool.Execute(ctx, map[string]any{})
	if !result.IsError {
		t.Fatal("expected an error when thread_id is missing")
	}
}

// TestCheckpointRevertRunTool_NothingCaptured_LeadsWithWarning is A7's
// regression test: a run with no recorded touched paths (e.g. it ran in a
// worktree checkpointing doesn't capture, or touched only gitignored
// files) must have its tool output LEAD with a warning, never print a
// "Reverted run X." success headline claiming files were restored.
func TestCheckpointRevertRunTool_NothingCaptured_LeadsWithWarning(t *testing.T) {
	dir := initCheckpointToolsTestRepo(t)
	ctx := context.Background()
	mgr, err := checkpoint.NewManager(ctx, t.TempDir(), dir, tools.NewFileLockManager())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer mgr.Close()

	// BeginRun with no matching EndRun/no file changes at all leaves
	// TouchedPaths empty — the same shape a worktree-run (uncaptured today)
	// or an ignored-files-only run produces.
	if _, err := mgr.BeginRun(ctx, "empty-thread", "coder", "task"); err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	if _, err := mgr.EndRun(ctx, "empty-thread"); err != nil {
		t.Fatalf("EndRun: %v", err)
	}

	tool := &tools.CheckpointRevertRunTool{Manager: mgr}
	result := tool.Execute(ctx, map[string]any{"thread_id": "empty-thread"})
	if result.IsError {
		t.Fatalf("checkpoint_revert_run on an empty-touched-paths run should not itself be an error: %s", result.Error)
	}
	if strings.Contains(result.Output, "Reverted run") {
		t.Fatalf("output claims success (\"Reverted run\") for a run with nothing captured: %q", result.Output)
	}
	if !strings.Contains(result.Output, "Nothing captured") {
		t.Fatalf("output does not lead with the nothing-captured warning: %q", result.Output)
	}
}
