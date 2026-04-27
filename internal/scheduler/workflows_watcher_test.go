package scheduler_test

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/scheduler"
)

// stubWatcherScheduler satisfies the WatcherScheduler interface.
type stubWatcherScheduler struct {
	registered int32
	removed    int32
}

func (s *stubWatcherScheduler) RegisterWorkflow(w *scheduler.Workflow) error {
	atomic.AddInt32(&s.registered, 1)
	return nil
}

func (s *stubWatcherScheduler) RemoveWorkflow(id string) {
	atomic.AddInt32(&s.removed, 1)
}

func writeWorkflowYAML(t *testing.T, dir, id string, enabled bool) {
	t.Helper()
	enabledStr := "false"
	if enabled {
		enabledStr = "true"
	}
	content := "id: " + id + "\nname: Test\nenabled: " + enabledStr + "\nschedule: \"@daily\"\nsteps: []\n"
	if err := os.WriteFile(filepath.Join(dir, id+".yaml"), []byte(content), 0o600); err != nil {
		t.Fatalf("write workflow yaml: %v", err)
	}
}

func TestWorkflowsWatcher_NewEnabledFile_RegistersCron(t *testing.T) {
	dir := t.TempDir()
	stub := &stubWatcherScheduler{}
	onChange := make(chan struct{}, 1)
	w := scheduler.NewWorkflowsWatcher(dir, stub, func() {
		select {
		case onChange <- struct{}{}:
		default:
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go w.Start(ctx)

	// Give watcher one poll to seed its initial hash.
	time.Sleep(100 * time.Millisecond)

	writeWorkflowYAML(t, dir, "wf-enabled", true)

	select {
	case <-onChange:
	case <-ctx.Done():
		t.Fatal("timed out waiting for onChange callback")
	}

	if atomic.LoadInt32(&stub.registered) < 1 {
		t.Errorf("expected RegisterWorkflow to be called, got 0")
	}
}

func TestWorkflowsWatcher_NewDisabledFile_LoadsButDoesntRegisterCron(t *testing.T) {
	dir := t.TempDir()
	stub := &stubWatcherScheduler{}
	onChange := make(chan struct{}, 1)
	w := scheduler.NewWorkflowsWatcher(dir, stub, func() {
		select {
		case onChange <- struct{}{}:
		default:
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go w.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	writeWorkflowYAML(t, dir, "wf-disabled", false)

	select {
	case <-onChange:
	case <-ctx.Done():
		t.Fatal("timed out waiting for onChange callback")
	}

	// The watcher calls RegisterWorkflow for all workflows.
	// The Scheduler.RegisterWorkflow implementation respects disabled:false
	// and no-ops, but the call still happens. So we expect RegisterWorkflow
	// to be called, even though no cron entry will be created.
	if atomic.LoadInt32(&stub.registered) < 1 {
		t.Errorf("expected RegisterWorkflow to be called for disabled workflow, got 0")
	}
}

func TestWorkflowsWatcher_DeletedFile_RemovesCron(t *testing.T) {
	dir := t.TempDir()
	stub := &stubWatcherScheduler{}
	onChange := make(chan struct{}, 4)

	// Write BEFORE starting watcher so it seeds initial hash with the file.
	writeWorkflowYAML(t, dir, "wf-del", true)

	w := scheduler.NewWorkflowsWatcher(dir, stub, func() {
		select {
		case onChange <- struct{}{}:
		default:
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go w.Start(ctx)

	// Wait for watcher to seed initial state (file is present).
	time.Sleep(100 * time.Millisecond)

	// Delete the file — watcher should call RemoveWorkflow.
	if err := os.Remove(filepath.Join(dir, "wf-del.yaml")); err != nil {
		t.Fatal(err)
	}

	select {
	case <-onChange:
	case <-ctx.Done():
		t.Fatal("timed out waiting for onChange after delete")
	}

	if atomic.LoadInt32(&stub.removed) < 1 {
		t.Errorf("expected RemoveWorkflow to be called, got 0")
	}
}

func TestWorkflowsWatcher_CtxCancel_Exits(t *testing.T) {
	dir := t.TempDir()
	stub := &stubWatcherScheduler{}
	w := scheduler.NewWorkflowsWatcher(dir, stub, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Start(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher did not exit after context cancellation")
	}
}
