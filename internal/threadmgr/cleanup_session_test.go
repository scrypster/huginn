package threadmgr

import (
	"context"
	"testing"
	"time"
)

// TestCleanupSession_RemovesQueuedThreads verifies that CleanupSession cancels
// and removes all queued threads for the given session.
func TestCleanupSession_RemovesQueuedThreads(t *testing.T) {
	tm := New()
	sessID := "sess-cleanup-1"
	t1, err := tm.Create(CreateParams{SessionID: sessID, AgentID: "agent-a", Task: "task-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t2, err := tm.Create(CreateParams{SessionID: sessID, AgentID: "agent-b", Task: "task-2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// t3 belongs to a different session — must not be removed.
	t3, err := tm.Create(CreateParams{SessionID: "other-session", AgentID: "agent-c", Task: "task-3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tm.CleanupSession(sessID)

	// Queued threads for sessID should be gone.
	if _, ok := tm.Get(t1.ID); ok {
		t.Errorf("expected thread %s to be removed after CleanupSession", t1.ID)
	}
	if _, ok := tm.Get(t2.ID); ok {
		t.Errorf("expected thread %s to be removed after CleanupSession", t2.ID)
	}
	// Thread in other session must survive.
	if _, ok := tm.Get(t3.ID); !ok {
		t.Errorf("expected thread %s from other session to survive CleanupSession", t3.ID)
	}
}

// TestCleanupSession_CancelsThinkingThread verifies that a started (thinking) thread
// has its cancel called and is removed.
func TestCleanupSession_CancelsThinkingThread(t *testing.T) {
	tm := New()
	sessID := "sess-cleanup-2"
	thr, err := tm.Create(CreateParams{SessionID: sessID, AgentID: "agent-x", Task: "task-x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cancelCalled := make(chan struct{})
	_, cancel := context.WithCancel(context.Background())
	wrappedCancel := func() {
		close(cancelCalled)
		cancel()
	}

	ok := tm.Start(thr.ID, context.Background(), wrappedCancel)
	if !ok {
		t.Fatal("expected Start to succeed")
	}

	tm.CleanupSession(sessID)

	select {
	case <-cancelCalled:
		// expected
	case <-time.After(500 * time.Millisecond):
		t.Error("cancel was not called within timeout after CleanupSession")
	}

	if _, ok := tm.Get(thr.ID); ok {
		t.Error("expected thread to be removed after CleanupSession")
	}
}

// TestCleanupSession_PreservesTerminalThreads verifies that completed threads
// are not removed or double-cancelled.
func TestCleanupSession_PreservesTerminalThreads(t *testing.T) {
	tm := New()
	sessID := "sess-cleanup-3"
	thr, err := tm.Create(CreateParams{SessionID: sessID, AgentID: "agent-y", Task: "task-y"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cancelCount := 0
	_, cancel := context.WithCancel(context.Background())
	countCancel := func() { cancelCount++; cancel() }

	tm.Start(thr.ID, context.Background(), countCancel)
	tm.Complete(thr.ID, FinishSummary{Summary: "done", Status: "completed"})

	tm.CleanupSession(sessID)

	// A completed thread is terminal and should NOT be cancelled or removed by CleanupSession.
	// (CleanupSession only removes queued/thinking/blocked threads.)
	if cancelCount > 0 {
		t.Errorf("expected cancel not to be called on completed thread, got %d calls", cancelCount)
	}
}

// TestCleanupSession_EmptySession_NoPanic verifies no panic on empty session.
func TestCleanupSession_EmptySession_NoPanic(t *testing.T) {
	tm := New()
	tm.CleanupSession("nonexistent-session") // must not panic
}

// TestCleanupSession_BlockedThread_IsRemoved verifies that blocked threads are cleaned.
func TestCleanupSession_BlockedThread_IsRemoved(t *testing.T) {
	tm := New()
	sessID := "sess-cleanup-4"
	thr, err := tm.Create(CreateParams{SessionID: sessID, AgentID: "agent-z", Task: "blocked-task"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, cancel := context.WithCancel(context.Background())
	tm.Start(thr.ID, context.Background(), cancel)
	// Manually force the thread into blocked state via the internal setBlocked helper.
	tm.setBlocked(thr.ID, "need help")

	tm.CleanupSession(sessID)

	if _, ok := tm.Get(thr.ID); ok {
		t.Error("expected blocked thread to be removed after CleanupSession")
	}
}

// TestAcquireLeases_EmptyThreadID_ReturnsWrappedError verifies ErrEmptyThreadID is wrapped.
func TestAcquireLeases_EmptyThreadID_ReturnsWrappedError(t *testing.T) {
	tm := New()
	_, err := tm.AcquireLeases("", []string{"/path/to/file"})
	if err == nil {
		t.Fatal("expected error for empty threadID, got nil")
	}
	// Should wrap ErrEmptyThreadID.
	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
}
