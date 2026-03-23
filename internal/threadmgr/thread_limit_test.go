package threadmgr

import (
	"errors"
	"testing"
)

// --- Thread limit enforcement ---

// TestCreate_ThreadLimitExceeded verifies that Create returns ErrThreadLimitExceeded
// when the session already has MaxThreadsPerSession active threads.
func TestCreate_ThreadLimitExceeded(t *testing.T) {
	tm := New()
	tm.MaxThreadsPerSession = 3

	for i := 0; i < 3; i++ {
		_, err := tm.Create(CreateParams{
			SessionID: "sess-limit",
			AgentID:   "agent",
			Task:      "task",
		})
		if err != nil {
			t.Fatalf("unexpected error on thread %d: %v", i+1, err)
		}
	}

	// 4th thread should be rejected.
	_, err := tm.Create(CreateParams{
		SessionID: "sess-limit",
		AgentID:   "agent",
		Task:      "overflow task",
	})
	if !errors.Is(err, ErrThreadLimitExceeded) {
		t.Errorf("expected ErrThreadLimitExceeded, got %v", err)
	}
}

// TestCreate_LimitDoesNotApplyAcrossSessions verifies that the cap is per-session.
func TestCreate_LimitDoesNotApplyAcrossSessions(t *testing.T) {
	tm := New()
	tm.MaxThreadsPerSession = 2

	// Fill session A to limit.
	for i := 0; i < 2; i++ {
		_, err := tm.Create(CreateParams{SessionID: "sess-A-i5", AgentID: "a", Task: "t"})
		if err != nil {
			t.Fatalf("unexpected error for sess-A thread %d: %v", i+1, err)
		}
	}

	// Session B should still be able to create threads.
	_, err := tm.Create(CreateParams{SessionID: "sess-B-i5", AgentID: "b", Task: "t"})
	if err != nil {
		t.Errorf("session B should not be affected by session A's limit: %v", err)
	}
}

// TestCreate_TerminalThreadsDoNotCountTowardLimit verifies that completed/cancelled
// threads are not counted toward the active thread limit.
func TestCreate_TerminalThreadsDoNotCountTowardLimit(t *testing.T) {
	tm := New()
	tm.MaxThreadsPerSession = 2

	// Create and complete 2 threads.
	for i := 0; i < 2; i++ {
		thr, err := tm.Create(CreateParams{SessionID: "sess-term-i5", AgentID: "a", Task: "t"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		tm.Complete(thr.ID, FinishSummary{Status: "completed", Summary: "done"})
	}

	// Even though 2 threads exist for this session, both are terminal.
	// A new active thread should be allowed.
	_, err := tm.Create(CreateParams{SessionID: "sess-term-i5", AgentID: "a", Task: "new active"})
	if err != nil {
		t.Errorf("terminal threads should not block new thread creation: %v", err)
	}
}

// TestCreate_EmptySessionID_NoLimit verifies that threads with no session ID bypass the limit.
func TestCreate_EmptySessionID_NoLimit(t *testing.T) {
	tm := New()
	tm.MaxThreadsPerSession = 1

	// First thread with empty session ID
	_, err := tm.Create(CreateParams{SessionID: "", AgentID: "a", Task: "t1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Second thread with empty session ID should NOT be blocked
	_, err = tm.Create(CreateParams{SessionID: "", AgentID: "b", Task: "t2"})
	if err != nil {
		t.Errorf("empty-session threads should bypass the per-session limit: %v", err)
	}
}

// TestErrThreadLimitExceeded_IsError verifies the error var is properly initialized.
func TestErrThreadLimitExceeded_IsError(t *testing.T) {
	if ErrThreadLimitExceeded == nil {
		t.Error("ErrThreadLimitExceeded should not be nil")
	}
	if ErrThreadLimitExceeded.Error() == "" {
		t.Error("ErrThreadLimitExceeded should have a non-empty message")
	}
}

// --- SummaryCache.Store with empty sessionID ---

// TestSummaryCache_StoreEmptySessionID_NoPanic verifies graceful handling of empty session IDs.
func TestSummaryCache_StoreEmptySessionID_NoPanic(t *testing.T) {
	cache := NewSummaryCache()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("SummaryCache.Store panicked with empty sessionID: %v", r)
		}
	}()
	cache.Store("", 1, "summary text")
	// Should store without panicking; recall should return the stored text.
	got, ok := cache.Get("")
	if !ok {
		t.Error("expected summary to be stored for empty sessionID")
	}
	if got != "summary text" {
		t.Errorf("expected 'summary text', got %q", got)
	}
}

// TestSummaryCache_GetNonExistent verifies Get returns empty string for unknown key.
func TestSummaryCache_GetNonExistent(t *testing.T) {
	cache := NewSummaryCache()
	got, ok := cache.Get("sess-x-nonexistent")
	if ok {
		t.Error("expected ok=false for non-existent key")
	}
	if got != "" {
		t.Errorf("expected empty string for non-existent key, got %q", got)
	}
}

// TestCreate_DefaultMaxThreadsPerSession verifies the default is applied when
// MaxThreadsPerSession is set to 0.
func TestCreate_DefaultMaxThreadsPerSession(t *testing.T) {
	tm := New()
	tm.MaxThreadsPerSession = 0 // should fall back to DefaultMaxThreadsPerSession

	// Create DefaultMaxThreadsPerSession threads — all should succeed.
	for i := 0; i < DefaultMaxThreadsPerSession; i++ {
		_, err := tm.Create(CreateParams{
			SessionID: "sess-default-limit",
			AgentID:   "agent",
			Task:      "task",
		})
		if err != nil {
			t.Fatalf("unexpected error on thread %d: %v", i+1, err)
		}
	}

	// The next one should fail.
	_, err := tm.Create(CreateParams{
		SessionID: "sess-default-limit",
		AgentID:   "agent",
		Task:      "overflow",
	})
	if !errors.Is(err, ErrThreadLimitExceeded) {
		t.Errorf("expected ErrThreadLimitExceeded at default limit, got %v", err)
	}
}
