// internal/scheduler/watcher_integration_test.go
//
// End-to-end integration tests that wire a real WorkflowsWatcher into a real
// Scheduler (not a stub) and verify that writing / deleting YAML files on disk
// causes the scheduler's cron entries to be added or removed.
//
// These tests run in package scheduler so they can inspect the unexported
// workflowEntries map directly — the same technique used by
// scheduler_lifecycle_test.go.

package scheduler

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// waitForEntry polls s.workflowEntries[id] until it is present (or absent,
// depending on want) or deadline expires. Returns true if the condition was met.
func waitForEntry(t *testing.T, s *Scheduler, id string, want bool, deadline time.Duration) bool {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		s.mu.Lock()
		_, ok := s.workflowEntries[id]
		s.mu.Unlock()
		if ok == want {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// TestScheduler_WatcherIntegration_FileDropRegistersWorkflow verifies that
// dropping a YAML file into the watched directory causes the real Scheduler to
// register a cron entry for the workflow within the watcher's poll+debounce
// window (2 s poll + 500 ms debounce).
func TestScheduler_WatcherIntegration_FileDropRegistersWorkflow(t *testing.T) {
	dir := t.TempDir()

	sched := New()
	// A minimal stub runner — RegisterWorkflow fails if workflowRunner == nil.
	sched.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		return nil
	})
	sched.SetWorkflowsDir(dir)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sched.Start(ctx)
	defer sched.Stop(context.Background()) //nolint:errcheck

	// Write the workflow file AFTER Start so the watcher must detect it via polling.
	yaml := "id: integ-test\nname: Integration Test\nenabled: true\nschedule: \"@yearly\"\nsteps: []\n"
	if err := os.WriteFile(filepath.Join(dir, "integ-test.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write workflow yaml: %v", err)
	}

	// The watcher polls every 2 s then debounces 500 ms: allow up to 10 s.
	if !waitForEntry(t, sched, "integ-test", true, 10*time.Second) {
		t.Fatal("workflow \"integ-test\" was not registered within 10 s of file creation")
	}
}

// TestScheduler_WatcherIntegration_DeletedFileRemovesWorkflow verifies that
// deleting a previously-registered workflow YAML file causes the scheduler to
// remove the corresponding cron entry.
func TestScheduler_WatcherIntegration_DeletedFileRemovesWorkflow(t *testing.T) {
	dir := t.TempDir()

	// Write the file BEFORE Start so the initial sync registers it immediately.
	yaml := "id: del-integ\nname: Delete Integration\nenabled: true\nschedule: \"@yearly\"\nsteps: []\n"
	if err := os.WriteFile(filepath.Join(dir, "del-integ.yaml"), []byte(yaml), 0o600); err != nil {
		t.Fatalf("write workflow yaml: %v", err)
	}

	sched := New()
	sched.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error {
		return nil
	})
	sched.SetWorkflowsDir(dir)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sched.Start(ctx)
	defer sched.Stop(context.Background()) //nolint:errcheck

	// Initial sync (called synchronously inside Start → watcher.Start) should
	// register the entry. Allow 5 s to account for goroutine scheduling.
	if !waitForEntry(t, sched, "del-integ", true, 5*time.Second) {
		t.Fatal("workflow \"del-integ\" was not registered after initial sync")
	}

	// Delete the file.
	if err := os.Remove(filepath.Join(dir, "del-integ.yaml")); err != nil {
		t.Fatal(err)
	}

	// Wait for the watcher to detect the deletion and remove the cron entry.
	if !waitForEntry(t, sched, "del-integ", false, 10*time.Second) {
		t.Fatal("workflow \"del-integ\" was still registered 10 s after file deletion")
	}
}
