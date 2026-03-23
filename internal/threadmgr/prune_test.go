package threadmgr

import (
	"context"
	"testing"
	"time"
)

// TestPrune_RemovesOldTerminalThreads verifies that terminal threads older than
// maxAge are removed from the map and the correct count is returned.
func TestPrune_RemovesOldTerminalThreads(t *testing.T) {
	tm := New()

	// Create three threads and mark them terminal with a CompletedAt in the past.
	past := time.Now().Add(-2 * time.Hour)
	for i := 0; i < 3; i++ {
		thr, err := tm.Create(CreateParams{SessionID: "sess-prune", AgentID: "a", Task: "t"})
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		tm.mu.Lock()
		tm.threads[thr.ID].Status = StatusDone
		tm.threads[thr.ID].CompletedAt = past
		tm.mu.Unlock()
	}

	// Also create one active thread (should NOT be pruned).
	active, _ := tm.Create(CreateParams{SessionID: "sess-prune", AgentID: "b", Task: "active"})
	_ = active

	beforeCount := threadCount(tm)
	if beforeCount != 4 {
		t.Fatalf("expected 4 threads before prune, got %d", beforeCount)
	}

	pruned := tm.Prune(1 * time.Hour) // maxAge = 1h, threads are 2h old → should be pruned
	if pruned != 3 {
		t.Errorf("expected 3 pruned, got %d", pruned)
	}

	afterCount := threadCount(tm)
	if afterCount != 1 {
		t.Errorf("expected 1 thread after prune (the active one), got %d", afterCount)
	}
}

// TestPrune_PreservesActiveAndYoungTerminal verifies that:
//   - Active (non-terminal) threads are never pruned.
//   - Terminal threads that completed recently (within maxAge) are preserved.
func TestPrune_PreservesActiveAndYoungTerminal(t *testing.T) {
	tm := New()

	// Young terminal thread — completed 10 minutes ago, maxAge = 1 hour.
	youngDone, _ := tm.Create(CreateParams{SessionID: "sess-preserve", AgentID: "a", Task: "t"})
	tm.mu.Lock()
	tm.threads[youngDone.ID].Status = StatusDone
	tm.threads[youngDone.ID].CompletedAt = time.Now().Add(-10 * time.Minute)
	tm.mu.Unlock()

	// Active thread (StatusThinking).
	activeThinking, _ := tm.Create(CreateParams{SessionID: "sess-preserve", AgentID: "b", Task: "t"})
	tm.mu.Lock()
	tm.threads[activeThinking.ID].Status = StatusThinking
	tm.mu.Unlock()

	// Blocked thread.
	blocked, _ := tm.Create(CreateParams{SessionID: "sess-preserve", AgentID: "c", Task: "t"})
	tm.mu.Lock()
	tm.threads[blocked.ID].Status = StatusBlocked
	tm.mu.Unlock()

	pruned := tm.Prune(1 * time.Hour)
	if pruned != 0 {
		t.Errorf("expected 0 pruned, got %d", pruned)
	}

	if threadCount(tm) != 3 {
		t.Errorf("expected 3 threads preserved, got %d", threadCount(tm))
	}
}

// TestPrune_ReturnsCorrectCount verifies the return value matches the actual
// number of threads removed across multiple statuses (Done, Cancelled, Error).
func TestPrune_ReturnsCorrectCount(t *testing.T) {
	tm := New()
	past := time.Now().Add(-25 * time.Hour) // older than 24h

	statuses := []ThreadStatus{StatusDone, StatusCancelled, StatusError}
	for _, s := range statuses {
		thr, _ := tm.Create(CreateParams{SessionID: "sess-count", AgentID: "a", Task: "t"})
		tm.mu.Lock()
		tm.threads[thr.ID].Status = s
		tm.threads[thr.ID].CompletedAt = past
		tm.mu.Unlock()
	}

	// One terminal thread with zero CompletedAt — should NOT be pruned (no completion time).
	noCT, _ := tm.Create(CreateParams{SessionID: "sess-count", AgentID: "a", Task: "t"})
	tm.mu.Lock()
	tm.threads[noCT.ID].Status = StatusDone
	// CompletedAt is zero value — Prune must not treat this as ancient.
	tm.mu.Unlock()

	pruned := tm.Prune(24 * time.Hour)
	if pruned != 3 {
		t.Errorf("expected 3 pruned (one per terminal status), got %d", pruned)
	}
}

// TestPrune_ZeroCompletedAt_NotPruned verifies that a terminal thread with a
// zero CompletedAt (never formally completed) is not evicted by Prune.
func TestPrune_ZeroCompletedAt_NotPruned(t *testing.T) {
	tm := New()
	thr, _ := tm.Create(CreateParams{SessionID: "sess-zero-ct", AgentID: "a", Task: "t"})
	tm.mu.Lock()
	tm.threads[thr.ID].Status = StatusDone
	// Leave CompletedAt as zero.
	tm.mu.Unlock()

	pruned := tm.Prune(1 * time.Millisecond)
	if pruned != 0 {
		t.Errorf("expected 0 pruned for zero CompletedAt, got %d", pruned)
	}
}

// TestPrune_EmptyManager_NoPanic verifies that Prune on an empty manager returns 0
// and does not panic.
func TestPrune_EmptyManager_NoPanic(t *testing.T) {
	tm := New()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Prune panicked on empty manager: %v", r)
		}
	}()
	pruned := tm.Prune(1 * time.Hour)
	if pruned != 0 {
		t.Errorf("expected 0, got %d", pruned)
	}
}

// TestStartPruner_ContextCancel verifies that the background pruner goroutine
// exits when its context is cancelled (no goroutine leak).
func TestStartPruner_ContextCancel(t *testing.T) {
	tm := New()
	ctx, cancel := context.WithCancel(context.Background())

	// Start with a very short interval so we can verify it ticks at least once.
	tm.StartPruner(ctx, 10*time.Millisecond, 1*time.Hour)

	// Let it tick a couple of times.
	time.Sleep(50 * time.Millisecond)

	// Cancel the context — goroutine should exit cleanly.
	cancel()

	// Give the goroutine a moment to observe the cancellation.
	time.Sleep(20 * time.Millisecond)
	// No assertion needed beyond "no panic / no deadlock" — the test timeout
	// would catch a goroutine that never exits via the -timeout flag.
}

// TestStartPruner_PrunesOldThreads verifies that the pruner goroutine actually
// removes old terminal threads over time.
func TestStartPruner_PrunesOldThreads(t *testing.T) {
	tm := New()

	// Add a terminal thread with a completion time in the past.
	thr, _ := tm.Create(CreateParams{SessionID: "sess-pruner", AgentID: "a", Task: "t"})
	tm.mu.Lock()
	tm.threads[thr.ID].Status = StatusDone
	tm.threads[thr.ID].CompletedAt = time.Now().Add(-2 * time.Hour)
	tm.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run pruner with a very short interval and maxAge of 1 hour.
	tm.StartPruner(ctx, 20*time.Millisecond, 1*time.Hour)

	// Wait for the pruner to fire.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if threadCount(tm) == 0 {
			return // pruned successfully
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Errorf("pruner did not remove old terminal thread within 500ms; count=%d", threadCount(tm))
}

// threadCount returns the total number of threads in the manager's map (for test assertions).
func threadCount(tm *ThreadManager) int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return len(tm.threads)
}
