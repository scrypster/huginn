package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/checkpoint"
	"github.com/scrypster/huginn/internal/threadmgr"
	"github.com/scrypster/huginn/internal/tools"
)

func initCheckpointTestRepo(t *testing.T) string {
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
	if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte("package app\n\nfunc Version() string { return \"v1\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".huginn/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "--quiet", "-m", "initial")
	return dir
}

// TestInitCheckpoints_EndToEndRunLifecycle simulates a full delegated agent
// run end to end through the real ThreadManager: create -> start (fires the
// checkpoint pre-run snapshot via wireThreadManagerCheckpoints) -> the
// "agent" edits files -> complete (fires the post-run snapshot) -> a
// second run makes more edits -> checkpoint_revert_run undoes the first
// run's file back to its pre-run state, and the shadow store is shown to
// hold both runs' snapshots.
func TestInitCheckpoints_EndToEndRunLifecycle(t *testing.T) {
	dir := initCheckpointTestRepo(t)
	ctx := context.Background()

	tm := threadmgr.New()
	toolReg := tools.NewRegistry()

	mgr, teardown, err := initCheckpoints(ctx, dir, toolReg, tm)
	if err != nil {
		t.Fatalf("initCheckpoints: %v", err)
	}
	defer teardown()

	// checkpoint_* tools must be registered on the shared registry.
	for _, name := range []string{"checkpoint_list", "checkpoint_diff_run", "checkpoint_revert_run", "checkpoint_gc"} {
		if _, ok := toolReg.Get(name); !ok {
			t.Errorf("tool %q not registered by initCheckpoints", name)
		}
	}

	original, err := os.ReadFile(filepath.Join(dir, "app.go"))
	if err != nil {
		t.Fatal(err)
	}

	// --- Run 1: an agent edits app.go ---
	th1, err := tm.Create(threadmgr.CreateParams{SessionID: "sess-1", AgentID: "coder", Task: "bump version to v2"})
	if err != nil {
		t.Fatalf("Create run1: %v", err)
	}
	runCtx1, cancel1 := context.WithCancel(context.Background())
	if !tm.Start(th1.ID, runCtx1, cancel1) {
		t.Fatal("Start run1 returned false")
	}
	if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte("package app\n\nfunc Version() string { return \"v2\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm.Complete(th1.ID, threadmgr.FinishSummary{Status: "completed", Summary: "bumped version"})

	waitForRun(t, mgr, th1.ID, checkpoint.RunCompleted)

	rec1, err := mgr.Get(ctx, th1.ID)
	if err != nil {
		t.Fatalf("Get run1: %v", err)
	}
	if len(rec1.TouchedPaths) != 1 || rec1.TouchedPaths[0] != "app.go" {
		t.Fatalf("run1 TouchedPaths = %v, want [app.go]", rec1.TouchedPaths)
	}

	// --- Run 2: a second agent makes more, unrelated edits ---
	th2, err := tm.Create(threadmgr.CreateParams{SessionID: "sess-1", AgentID: "coder", Task: "add readme"})
	if err != nil {
		t.Fatalf("Create run2: %v", err)
	}
	runCtx2, cancel2 := context.WithCancel(context.Background())
	if !tm.Start(th2.ID, runCtx2, cancel2) {
		t.Fatal("Start run2 returned false")
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm.Complete(th2.ID, threadmgr.FinishSummary{Status: "completed", Summary: "added readme"})
	waitForRun(t, mgr, th2.ID, checkpoint.RunCompleted)

	// The shadow store now holds snapshots for both runs.
	runs, err := mgr.List(ctx, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("List = %d runs, want 2", len(runs))
	}

	// --- Undo run 1 via the tool surface, exactly as the model would ---
	revertTool, ok := toolReg.Get("checkpoint_revert_run")
	if !ok {
		t.Fatal("checkpoint_revert_run not registered")
	}
	result := revertTool.Execute(ctx, map[string]any{"thread_id": th1.ID})
	if result.IsError {
		t.Fatalf("checkpoint_revert_run failed: %s", result.Error)
	}

	got, err := os.ReadFile(filepath.Join(dir, "app.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("app.go after revert = %q, want original %q", got, original)
	}
	// Run 2's unrelated file must be untouched by run 1's revert.
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Fatalf("README.md from run2 should still exist after reverting run1: %v", err)
	}
}

func waitForRun(t *testing.T, mgr *checkpoint.Manager, threadID string, want checkpoint.RunStatus) {
	t.Helper()
	// The status-change hook fires synchronously from tm.Complete's call to
	// fireStatusChange, so by the time Complete() returns the checkpoint
	// EndRun has already run — this just double-checks the ledger agrees.
	rec, err := mgr.Get(context.Background(), threadID)
	if err != nil {
		t.Fatalf("Get(%s): %v", threadID, err)
	}
	if rec.Status != want {
		t.Fatalf("run %s status = %v, want %v (capture error: %s)", threadID, rec.Status, want, rec.CaptureError)
	}
}
