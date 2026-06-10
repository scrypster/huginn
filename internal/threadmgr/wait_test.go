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
