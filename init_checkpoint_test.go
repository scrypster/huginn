package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

	mgr, teardown, err := initCheckpoints(ctx, t.TempDir(), dir, toolReg, tm, nil)
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

// TestInitCheckpoints_ProductionWiring_SharesLockNamespace_WithWriteFileTool
// is A1's real regression test: it builds the tool registry and checkpoint
// manager EXACTLY the way main.go's server-mode wiring does — one shared
// *tools.FileLockManager passed to both tools.RegisterBuiltinsWithLocker
// and initCheckpoints — then proves RevertRun and write_file's own lock
// key actually collide.
//
// Before A1, this failed for two independent reasons, either alone
// sufficient to make the "shared lock" guarantee theater:
//  1. init_checkpoint.go constructed its OWN fresh tools.NewFileLockManager(),
//     never the one write_file/edit_file use — proven here by using the
//     literal production entry point (initCheckpoints) with an explicit flm
//     and asserting it is what the checkpoint manager actually uses (via the
//     blocking behavior below, not just reference equality, since Manager
//     doesn't expose its locker).
//  2. Even with a shared instance, RevertRun locked the bare repo-relative
//     path ("hello.txt") while write_file locks the ABSOLUTE,
//     symlink-resolved path ResolveSandboxed produces
//     ("/tmp/.../hello.txt") — two keys under the same FileLockManager that
//     never intersect, so Lock() never actually blocked the other side.
//
// The test proves both are fixed without relying on OS-level write
// atomicity (a single os.WriteFile call to a regular file is atomic on its
// own on common filesystems regardless of app-level locking, which is why
// a "torn content" style race test can't reliably distinguish a real fix
// from a no-op one): it takes the SAME FileLockManager instance, locks the
// EXACT key write_file's own ResolveSandboxed would produce for this path,
// holds it for a measurable duration, and asserts a concurrent
// checkpoint_revert_run on that path only completes AFTER the external
// lock is released — i.e. RevertRun's internal Lock() call genuinely
// blocked on it, which is only possible if RevertRun computes the same key.
func TestInitCheckpoints_ProductionWiring_SharesLockNamespace_WithWriteFileTool(t *testing.T) {
	dir := initCheckpointTestRepo(t)
	ctx := context.Background()

	tm := threadmgr.New()
	toolReg := tools.NewRegistry()

	// Exactly main.go's server-mode sequence (main.go: srvFileLock ->
	// RegisterBuiltinsWithLocker -> initCheckpoints(..., srvFileLock)).
	flm := tools.NewFileLockManager()
	tools.RegisterBuiltinsWithLocker(toolReg, dir, 10*time.Second, flm)
	mgr, teardown, err := initCheckpoints(ctx, t.TempDir(), dir, toolReg, tm, flm)
	if err != nil {
		t.Fatalf("initCheckpoints: %v", err)
	}
	defer teardown()

	// Give hello.txt a run to revert.
	if _, err := mgr.BeginRun(ctx, "lock-thread", "coder", "task"); err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("run edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EndRun(ctx, "lock-thread"); err != nil {
		t.Fatalf("EndRun: %v", err)
	}

	// The exact key write_file/edit_file compute for this path — via the
	// real exported helper they both call, not a hand-rolled equivalent.
	resolvedPath, err := tools.ResolveSandboxed(dir, "hello.txt")
	if err != nil {
		t.Fatalf("ResolveSandboxed: %v", err)
	}

	const holdTime = 300 * time.Millisecond
	var revertStarted, revertDone time.Time
	var mu sync.Mutex

	var wg sync.WaitGroup
	wg.Add(2)

	unlockAt := make(chan time.Time, 1)
	go func() {
		defer wg.Done()
		flm.Lock(resolvedPath)
		time.Sleep(holdTime)
		unlockAt <- time.Now()
		flm.Unlock(resolvedPath)
	}()
	go func() {
		defer wg.Done()
		// Give the external lock a head start so it's reliably held first.
		time.Sleep(30 * time.Millisecond)
		mu.Lock()
		revertStarted = time.Now()
		mu.Unlock()
		if _, err := mgr.RevertRun(ctx, "lock-thread", checkpoint.RevertOptions{All: true}); err != nil {
			t.Errorf("RevertRun: %v", err)
		}
		mu.Lock()
		revertDone = time.Now()
		mu.Unlock()
	}()
	wg.Wait()

	released := <-unlockAt
	if revertDone.Before(released) {
		t.Fatalf("RevertRun completed at %v, BEFORE the external lock on the same resolved path was released at %v — "+
			"RevertRun and write_file/edit_file are not actually serializing on the same lock key (A1 regression)",
			revertDone, released)
	}
	_ = revertStarted // recorded for debugging output above, not asserted on directly
}

// TestInitCheckpoints_PushGuard_EndToEnd is A2's regression test: git_push
// against a real local bare remote must flag every completed-but-unsynced
// checkpoint run as Pushed, so checkpoint_revert_run's AllowAfterPush guard
// actually fires — before A2, MarkPushed had zero production callers, so
// RunRecord.Pushed was always false and this guard was permanently dead.
func TestInitCheckpoints_PushGuard_EndToEnd(t *testing.T) {
	ctx := context.Background()

	// A bare remote to push to, plus a clone of it as the sandbox — mirrors
	// a real "agent working in a cloned repo, pushes to origin" setup.
	remoteDir := t.TempDir()
	runIn := func(dir string, args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v (in %s): %v\n%s", args, dir, err, out)
		}
		return string(out)
	}
	runIn(remoteDir, "init", "--quiet", "--bare", "--initial-branch=main")

	dir := t.TempDir()
	if out, err := exec.Command("git", "clone", "--quiet", remoteDir, dir).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte("package app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runIn(dir, "add", "-A")
	runIn(dir, "commit", "--quiet", "-m", "initial")
	runIn(dir, "push", "--quiet", "-u", "origin", "main")

	tm := threadmgr.New()
	toolReg := tools.NewRegistry()
	flm := tools.NewFileLockManager()
	tools.RegisterGitTools(toolReg, dir)
	mgr, teardown, err := initCheckpoints(ctx, t.TempDir(), dir, toolReg, tm, flm)
	if err != nil {
		t.Fatalf("initCheckpoints: %v", err)
	}
	defer teardown()

	// A run edits a file (never pushed yet).
	if _, err := mgr.BeginRun(ctx, "push-thread", "coder", "edit app.go"); err != nil {
		t.Fatalf("BeginRun: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte("package app\n\nfunc V() string { return \"v2\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EndRun(ctx, "push-thread"); err != nil {
		t.Fatalf("EndRun: %v", err)
	}

	rec, err := mgr.Get(ctx, "push-thread")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if rec.Pushed {
		t.Fatal("run should not be marked Pushed before git_push has run")
	}

	// Revert without allow_after_push must succeed pre-push. Uses a
	// throwaway second thread/file rather than push-thread itself — A16
	// (RunReverted status) means reverting push-thread here would flip its
	// status away from RunCompleted, which would then make it invisible to
	// MarkAllUnsyncedPushed's "completed but unsynced" filter below and
	// falsely look like an A2 regression. That's a real, separate
	// consequence of A16 worth noting, not a bug in the guard itself: a
	// run that's already been reverted has nothing left for the pushed
	// guard to protect.
	if _, err := mgr.BeginRun(ctx, "pre-push-revert-thread", "coder", "scratch"); err != nil {
		t.Fatalf("BeginRun scratch: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scratch.txt"), []byte("scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EndRun(ctx, "pre-push-revert-thread"); err != nil {
		t.Fatalf("EndRun scratch: %v", err)
	}
	if _, err := mgr.RevertRun(ctx, "pre-push-revert-thread", checkpoint.RevertOptions{}); err != nil {
		t.Fatalf("RevertRun before push should succeed: %v", err)
	}

	runIn(dir, "add", "-A")
	runIn(dir, "commit", "--quiet", "-m", "v2")

	// Now actually push via the real git_push tool, exactly as an agent
	// would — this is what must flag the run as pushed.
	pushTool, ok := toolReg.Get("git_push")
	if !ok {
		t.Fatal("git_push not registered")
	}
	result := pushTool.Execute(ctx, map[string]any{})
	if result.IsError {
		t.Fatalf("git_push failed: %s", result.Error)
	}

	rec, err = mgr.Get(ctx, "push-thread")
	if err != nil {
		t.Fatalf("Get after push: %v", err)
	}
	if !rec.Pushed {
		t.Fatal("run should be marked Pushed after a successful git_push (A2 regression: MarkPushed had zero production callers)")
	}

	// Revert without allow_after_push must now be refused.
	if _, err := mgr.RevertRun(ctx, "push-thread", checkpoint.RevertOptions{}); err != checkpoint.ErrAlreadyPushed {
		t.Fatalf("RevertRun after push without AllowAfterPush = %v, want ErrAlreadyPushed", err)
	}
	// With allow_after_push=true, revert proceeds (guidance path).
	revertResult, err := mgr.RevertRun(ctx, "push-thread", checkpoint.RevertOptions{All: true, AllowAfterPush: true})
	if err != nil {
		t.Fatalf("RevertRun with AllowAfterPush: %v", err)
	}
	if !strings.Contains(revertResult.Warning, "already pushed") {
		t.Errorf("RevertRun with AllowAfterPush Warning = %q, want it to mention the pushed commit was not rewritten", revertResult.Warning)
	}
}
