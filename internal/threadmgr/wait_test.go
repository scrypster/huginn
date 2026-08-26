package threadmgr

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestWaitForThreads_AllComplete verifies the barrier returns as soon as every
// thread reaches a terminal status, with full summaries attached.
func TestWaitForThreads_AllComplete(t *testing.T) {
	tm := New()

	t1, _ := tm.Create(CreateParams{SessionID: "s1", AgentID: "Sam", Task: "a"})
	t2, _ := tm.Create(CreateParams{SessionID: "s1", AgentID: "Dave", Task: "b"})

	go func() {
		time.Sleep(100 * time.Millisecond)
		tm.Complete(t1.ID, FinishSummary{Summary: "done a", Status: "completed"})
		time.Sleep(200 * time.Millisecond)
		tm.Complete(t2.ID, FinishSummary{Summary: "done b", Status: "completed"})
	}()

	report := tm.WaitForThreads(context.Background(), "s1", []string{t1.ID, t2.ID}, 5*time.Second)
	if report.TimedOut {
		t.Fatal("expected report not to time out")
	}
	if len(report.Completed) != 2 || len(report.Pending) != 0 {
		t.Fatalf("expected 2 completed / 0 pending, got %d/%d", len(report.Completed), len(report.Pending))
	}
	for _, th := range report.Completed {
		if th.Summary == nil {
			t.Errorf("thread %s missing summary in wait report", th.ID)
		}
	}
}

// TestWaitForThreads_Timeout verifies that a never-finishing thread is reported
// as pending with TimedOut set.
func TestWaitForThreads_Timeout(t *testing.T) {
	tm := New()
	t1, _ := tm.Create(CreateParams{SessionID: "s1", AgentID: "Sam", Task: "slow"})

	report := tm.WaitForThreads(context.Background(), "s1", []string{t1.ID}, 600*time.Millisecond)
	if !report.TimedOut {
		t.Fatal("expected timeout")
	}
	if len(report.Pending) != 1 || report.Pending[0].ID != t1.ID {
		t.Fatalf("expected the slow thread pending, got %+v", report.Pending)
	}
}

// TestWaitForThreads_ContextCancel verifies ctx cancellation unblocks the wait.
func TestWaitForThreads_ContextCancel(t *testing.T) {
	tm := New()
	t1, _ := tm.Create(CreateParams{SessionID: "s1", AgentID: "Sam", Task: "slow"})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	report := tm.WaitForThreads(ctx, "s1", []string{t1.ID}, 30*time.Second)
	if time.Since(start) > 5*time.Second {
		t.Fatal("wait did not unblock promptly on ctx cancel")
	}
	if !report.TimedOut || len(report.Pending) != 1 {
		t.Fatalf("expected timed-out report with 1 pending, got %+v", report)
	}
}

// TestWaitForThreads_SessionScoping verifies threads from other sessions are ignored.
func TestWaitForThreads_SessionScoping(t *testing.T) {
	tm := New()
	other, _ := tm.Create(CreateParams{SessionID: "other", AgentID: "Sam", Task: "x"})

	report := tm.WaitForThreads(context.Background(), "s1", []string{other.ID}, 300*time.Millisecond)
	if report.TimedOut || len(report.Completed) != 0 || len(report.Pending) != 0 {
		t.Fatalf("cross-session thread must be ignored, got %+v", report)
	}
}

// TestRecordActivity_Heartbeat verifies heartbeat fields update and survive Get's copy.
func TestRecordActivity_Heartbeat(t *testing.T) {
	tm := New()
	th, _ := tm.Create(CreateParams{SessionID: "s1", AgentID: "Sam", Task: "x"})

	tm.RecordActivity(th.ID, `running tool "bash"`)
	got, ok := tm.Get(th.ID)
	if !ok {
		t.Fatal("thread not found")
	}
	if got.CurrentActivity != `running tool "bash"` {
		t.Errorf("CurrentActivity = %q", got.CurrentActivity)
	}
	if got.LastActivityAt.IsZero() {
		t.Error("LastActivityAt not stamped")
	}

	// Empty activity refreshes the timestamp but keeps the description.
	prev := got.LastActivityAt
	time.Sleep(10 * time.Millisecond)
	tm.RecordActivity(th.ID, "")
	got2, _ := tm.Get(th.ID)
	if got2.CurrentActivity != `running tool "bash"` {
		t.Errorf("empty activity must not clear description, got %q", got2.CurrentActivity)
	}
	if !got2.LastActivityAt.After(prev) {
		t.Error("LastActivityAt not refreshed")
	}
}

// TestDescribeThreadLiveness_Stall verifies the stall warning appears only for
// quiet non-terminal threads.
func TestDescribeThreadLiveness_Stall(t *testing.T) {
	active := &Thread{
		Status:          StatusThinking,
		CurrentActivity: "thinking (turn 2/50)",
		LastActivityAt:  time.Now(),
		CreatedAt:       time.Now(),
	}
	if s := describeThreadLiveness(active); strings.Contains(s, "stalled") {
		t.Errorf("fresh thread must not be flagged stalled: %q", s)
	}

	quiet := &Thread{
		Status:          StatusThinking,
		CurrentActivity: "thinking (turn 2/50)",
		LastActivityAt:  time.Now().Add(-10 * time.Minute),
		CreatedAt:       time.Now().Add(-11 * time.Minute),
	}
	if s := describeThreadLiveness(quiet); !strings.Contains(s, "stalled") {
		t.Errorf("quiet active thread must be flagged stalled: %q", s)
	}

	done := &Thread{
		Status:         StatusDone,
		LastActivityAt: time.Now().Add(-10 * time.Minute),
		CreatedAt:      time.Now().Add(-11 * time.Minute),
	}
	if s := describeThreadLiveness(done); strings.Contains(s, "stalled") {
		t.Errorf("terminal thread must not be flagged stalled: %q", s)
	}
}

// TestWaitForThreadsTool_Output verifies the tool renders finished summaries and
// pending heartbeat state.
func TestWaitForThreadsTool_Output(t *testing.T) {
	tm := New()
	finished, _ := tm.Create(CreateParams{SessionID: "s1", AgentID: "Sam", Task: "a"})
	tm.Complete(finished.ID, FinishSummary{
		Summary:       "implemented the feature",
		FilesModified: []string{"a.go"},
		Status:        "completed",
	})
	running, _ := tm.Create(CreateParams{SessionID: "s1", AgentID: "Dave", Task: "b"})
	tm.RecordActivity(running.ID, "thinking (turn 1/50)")

	tool := &WaitForThreadsTool{
		Fn: func(ctx context.Context, ids []string, timeout time.Duration) (WaitReport, error) {
			return tm.WaitForThreads(ctx, "s1", ids, timeout), nil
		},
	}
	res := tool.Execute(context.Background(), map[string]any{
		"thread_ids":      []any{finished.ID, running.ID},
		"timeout_seconds": float64(1),
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	out := res.Output
	if !strings.Contains(out, "implemented the feature") || !strings.Contains(out, "a.go") {
		t.Errorf("output missing finished summary detail: %q", out)
	}
	if !strings.Contains(out, "Still running (1)") || !strings.Contains(out, "Dave") {
		t.Errorf("output missing pending section: %q", out)
	}
	if !strings.Contains(out, "thinking (turn 1/50)") {
		t.Errorf("output missing pending heartbeat activity: %q", out)
	}
	if res.Metadata["timed_out"] != true {
		t.Errorf("metadata timed_out = %v", res.Metadata["timed_out"])
	}
}

// TestWaitForThreadsTool_TimeoutClamp verifies timeout_seconds is capped.
func TestWaitForThreadsTool_TimeoutClamp(t *testing.T) {
	var gotTimeout time.Duration
	tool := &WaitForThreadsTool{
		Fn: func(ctx context.Context, ids []string, timeout time.Duration) (WaitReport, error) {
			gotTimeout = timeout
			return WaitReport{}, nil
		},
	}
	tool.Execute(context.Background(), map[string]any{"timeout_seconds": float64(9999)})
	if gotTimeout != maxWaitTimeout {
		t.Errorf("timeout not clamped: got %s want %s", gotTimeout, maxWaitTimeout)
	}
}

// TestWaitForThreads_MarksCollected verifies that results delivered through
// WaitForThreads are stamped as collected, so the completion notifier can skip
// the duplicate follow-up synthesis.
func TestWaitForThreads_MarksCollected(t *testing.T) {
	tm := New()
	th, _ := tm.Create(CreateParams{SessionID: "s1", AgentID: "Sam", Task: "a"})
	tm.Complete(th.ID, FinishSummary{Summary: "done", Status: "completed"})

	if tm.WasCollected(th.ID) {
		t.Fatal("thread must not be collected before any wait")
	}
	report := tm.WaitForThreads(context.Background(), "s1", []string{th.ID}, time.Second)
	if len(report.Completed) != 1 {
		t.Fatalf("expected 1 completed, got %d", len(report.Completed))
	}
	if !tm.WasCollected(th.ID) {
		t.Error("thread must be marked collected after WaitForThreads returns it")
	}

	// Pending threads at timeout are NOT marked collected.
	pending, _ := tm.Create(CreateParams{SessionID: "s1", AgentID: "Dave", Task: "b"})
	_ = tm.WaitForThreads(context.Background(), "s1", []string{pending.ID}, 300*time.Millisecond)
	if tm.WasCollected(pending.ID) {
		t.Error("pending thread must not be marked collected")
	}
}

// TestWaitForThreads_UncollectedFinished verifies that wait with empty thread_ids
// includes uncollected terminal threads (that finished before wait was called).
// This reproduces the bug where a fast specialist finishes before wait runs,
// and wait incorrectly returned "No matching threads" instead of the result.
func TestWaitForThreads_UncollectedFinished(t *testing.T) {
	tm := New()
	specialist, _ := tm.Create(CreateParams{SessionID: "s1", AgentID: "Reggie", Task: "fast task"})

	// Specialist finishes immediately before wait is called
	tm.Complete(specialist.ID, FinishSummary{Summary: "PONG", Status: "completed"})

	// Wait with empty thread_ids (no thread_ids provided) should still return the finished thread
	report := tm.WaitForThreads(context.Background(), "s1", []string{}, 5*time.Second)
	if len(report.Completed) != 1 || report.Completed[0].ID != specialist.ID {
		t.Errorf("wait with empty ids should return finished specialist, got completed=%d pending=%d",
			len(report.Completed), len(report.Pending))
	}
	if !tm.WasCollected(specialist.ID) {
		t.Error("thread must be marked collected after wait returns it")
	}
}

// TestCompletionNotify_StampsThreadID verifies Notify stamps ThreadID onto the
// summary so FollowUpFn implementations can identify the thread.
func TestCompletionNotify_StampsThreadID(t *testing.T) {
	got := make(chan FinishSummary, 1)
	n := &CompletionNotifier{
		Broadcast: func(string, string, map[string]any) {},
		FollowUpFn: func(_ context.Context, _, _ string, s *FinishSummary) {
			got <- *s
		},
	}
	n.Notify(context.Background(), "s1", "th-42", "Sam", &FinishSummary{Summary: "x", Status: "completed"})
	select {
	case s := <-got:
		if s.ThreadID != "th-42" {
			t.Errorf("ThreadID = %q, want th-42", s.ThreadID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("FollowUpFn never ran")
	}
}

func TestWaitForThreads_PlaceholderIDsFallBackToUncollected(t *testing.T) {
	tm := New()
	specialist, _ := tm.Create(CreateParams{SessionID: "s1", AgentID: "Reggie", Task: "fast"})
	tm.Complete(specialist.ID, FinishSummary{Summary: "PONG", Status: "completed"})

	report := tm.WaitForThreads(context.Background(), "s1", []string{"<thread_id_retrieved_from_delegation>"}, 5*time.Second)
	if len(report.Completed) != 1 || report.Completed[0].ID != specialist.ID {
		t.Fatalf("placeholder wait should fall back to uncollected, got completed=%d pending=%d", len(report.Completed), len(report.Pending))
	}
}
