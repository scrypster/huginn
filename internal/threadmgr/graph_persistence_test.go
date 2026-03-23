package threadmgr_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/threadmgr"
)

// TestSnapshotAndLoadGraph verifies that a thread dependency graph can be
// persisted to disk and restored into a fresh ThreadManager.
func TestSnapshotAndLoadGraph(t *testing.T) {
	dir := t.TempDir()
	tm := threadmgr.New()
	tm.SetGraphDir(dir)

	const sess = "sess-graph-1"

	// Create two threads; downstream depends on upstream.
	upstream, err := tm.Create(threadmgr.CreateParams{
		SessionID:     sess,
		AgentID:       "stacy",
		Task:          "implement auth",
		CreatedByUser: "primary",
		CreatedReason: "auth module needed",
	})
	if err != nil {
		t.Fatalf("create upstream: %v", err)
	}
	downstream, err := tm.Create(threadmgr.CreateParams{
		SessionID: sess,
		AgentID:   "sam",
		Task:      "write tests",
		DependsOn: []string{upstream.ID},
	})
	if err != nil {
		t.Fatalf("create downstream: %v", err)
	}

	// Complete upstream so it is terminal before snapshot.
	tm.Complete(upstream.ID, threadmgr.FinishSummary{Summary: "auth done", Status: "completed"})

	// A graph JSON file should exist in the dir.
	graphFiles, _ := filepath.Glob(filepath.Join(dir, "graph-"+sess+".json"))
	if len(graphFiles) == 0 {
		t.Fatal("expected graph file to be written on Create/Complete")
	}

	// Restore into a new manager.
	tm2 := threadmgr.New()
	tm2.SetGraphDir(dir)
	n, err := tm2.RestoreSession(sess)
	if err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 threads restored, got %d", n)
	}

	// Upstream should be StatusDone; downstream should be StatusError
	// (was non-terminal when snapshotted).
	up, ok := tm2.Get(upstream.ID)
	if !ok {
		t.Fatal("upstream not found after restore")
	}
	if up.Status != threadmgr.StatusDone {
		t.Errorf("expected upstream StatusDone, got %s", up.Status)
	}
	if up.AgentID != "stacy" {
		t.Errorf("expected AgentID stacy, got %s", up.AgentID)
	}

	dn, ok := tm2.Get(downstream.ID)
	if !ok {
		t.Fatal("downstream not found after restore")
	}
	// downstream was StatusQueued at snapshot time → restored as StatusError.
	if dn.Status != threadmgr.StatusError {
		t.Errorf("expected downstream StatusError (non-terminal on restore), got %s", dn.Status)
	}
	if len(dn.DependsOn) == 0 || dn.DependsOn[0] != upstream.ID {
		t.Errorf("dependency not restored: %v", dn.DependsOn)
	}
}

// TestRestoreSession_NoFile verifies that RestoreSession is a no-op when no
// graph file exists.
func TestRestoreSession_NoFile(t *testing.T) {
	dir := t.TempDir()
	tm := threadmgr.New()
	tm.SetGraphDir(dir)

	n, err := tm.RestoreSession("nonexistent-session")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 restored, got %d", n)
	}
}

// TestRestoreSession_Idempotent verifies that calling RestoreSession twice
// does not duplicate threads in the manager.
func TestRestoreSession_Idempotent(t *testing.T) {
	dir := t.TempDir()
	tm := threadmgr.New()
	tm.SetGraphDir(dir)

	const sess = "sess-idem"
	if _, err := tm.Create(threadmgr.CreateParams{
		SessionID: sess, AgentID: "alice", Task: "do stuff",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Restore into a new manager twice.
	tm2 := threadmgr.New()
	tm2.SetGraphDir(dir)
	if _, err := tm2.RestoreSession(sess); err != nil {
		t.Fatalf("first restore: %v", err)
	}
	if _, err := tm2.RestoreSession(sess); err != nil {
		t.Fatalf("second restore: %v", err)
	}
	// Only one thread should exist.
	threads := tm2.ListBySession(sess)
	if len(threads) != 1 {
		t.Errorf("expected 1 thread after idempotent restore, got %d", len(threads))
	}
}

// TestCanReach verifies the targeted DFS cycle-detection helper.
func TestHasCycle(t *testing.T) {
	tm := threadmgr.New()

	// A → B → C (no cycle)
	a, _ := tm.Create(threadmgr.CreateParams{SessionID: "s", AgentID: "a", Task: "a"})
	b, _ := tm.Create(threadmgr.CreateParams{SessionID: "s", AgentID: "b", Task: "b", DependsOn: []string{a.ID}})
	c, _ := tm.Create(threadmgr.CreateParams{SessionID: "s", AgentID: "c", Task: "c", DependsOn: []string{b.ID}})

	// Adding C → A would create a cycle (C depends on B depends on A, then A depends on C).
	if !tm.HasCycle(a.ID, c.ID) {
		t.Error("expected HasCycle(a,c) == true (c can reach a via b)")
	}

	// Adding D → C is fine (D has no current path to D through C).
	d, _ := tm.Create(threadmgr.CreateParams{SessionID: "s", AgentID: "d", Task: "d"})
	if tm.HasCycle(d.ID, c.ID) {
		t.Error("expected HasCycle(d,c) == false (no cycle)")
	}
}

// TestSetGraphDir_EmptyDisablesPersistence verifies that an empty graphDir
// disables graph snapshots and RestoreSession is a no-op.
func TestSetGraphDir_EmptyDisablesPersistence(t *testing.T) {
	tm := threadmgr.New()
	// graphDir is empty by default.
	if _, err := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-no-dir", AgentID: "bot", Task: "task",
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	// No graph files should have been written to the working directory.
	matches, _ := filepath.Glob("graph-sess-no-dir.json")
	if len(matches) > 0 {
		os.Remove(matches[0])
		t.Errorf("unexpected graph file created when graphDir is empty")
	}

	// RestoreSession should return 0, nil.
	n, err := tm.RestoreSession("sess-no-dir")
	if err != nil || n != 0 {
		t.Errorf("expected (0, nil), got (%d, %v)", n, err)
	}
}

// TestSnapshotGraph_CreatedAtPreserved checks that CreatedAt timestamps survive
// a snapshot/restore cycle.
func TestSnapshotGraph_CreatedAtPreserved(t *testing.T) {
	dir := t.TempDir()
	tm := threadmgr.New()
	tm.SetGraphDir(dir)

	before := time.Now().Truncate(time.Second)
	thread, _ := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-ts", AgentID: "bot", Task: "do it",
	})
	after := time.Now().Add(time.Second)

	tm2 := threadmgr.New()
	tm2.SetGraphDir(dir)
	tm2.RestoreSession("sess-ts")

	got, ok := tm2.Get(thread.ID)
	if !ok {
		t.Fatal("thread not found after restore")
	}
	if got.CreatedAt.Before(before) || got.CreatedAt.After(after) {
		t.Errorf("CreatedAt not preserved: %v (want between %v and %v)",
			got.CreatedAt, before, after)
	}
}
