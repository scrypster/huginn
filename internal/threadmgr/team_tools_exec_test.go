package threadmgr

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ─── list_team_status ─────────────────────────────────────────────────────────

func TestListTeamStatusTool_Execute_IncludesLiveness(t *testing.T) {
	tm := New()
	running, _ := tm.Create(CreateParams{SessionID: "s1", AgentID: "Sam", Task: "investigate the flaky relay"})
	tm.RecordActivity(running.ID, "thinking (turn 3/50)")

	stalled, _ := tm.Create(CreateParams{SessionID: "s1", AgentID: "Dave", Task: "slow job"})
	// Backdate the heartbeat past the stall threshold.
	tm.mu.Lock()
	tm.threads[stalled.ID].LastActivityAt = time.Now().Add(-5 * time.Minute)
	tm.threads[stalled.ID].CurrentActivity = `running tool "bash"`
	tm.mu.Unlock()

	tool := &ListTeamStatusTool{
		Fn: func(ctx context.Context) ([]*Thread, error) {
			return tm.ListBySession("s1"), nil
		},
	}
	res := tool.Execute(context.Background(), nil)
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	out := res.Output
	if !strings.Contains(out, "Sam") || !strings.Contains(out, "thinking (turn 3/50)") {
		t.Errorf("output missing running thread's activity: %q", out)
	}
	if !strings.Contains(out, "last activity") {
		t.Errorf("output missing last-activity recency: %q", out)
	}
	if !strings.Contains(out, "possibly stalled") {
		t.Errorf("output missing stall warning for quiet thread: %q", out)
	}
	if !strings.Contains(out, "wait_for_threads") {
		t.Errorf("output missing wait_for_threads tip: %q", out)
	}
}

func TestListTeamStatusTool_Execute_Empty(t *testing.T) {
	tool := &ListTeamStatusTool{
		Fn: func(ctx context.Context) ([]*Thread, error) { return nil, nil },
	}
	res := tool.Execute(context.Background(), nil)
	if res.IsError || !strings.Contains(res.Output, "No delegated threads") {
		t.Errorf("unexpected result for empty session: %+v", res)
	}
}

func TestListTeamStatusTool_Execute_Errors(t *testing.T) {
	unconfigured := &ListTeamStatusTool{}
	if res := unconfigured.Execute(context.Background(), nil); !res.IsError {
		t.Error("expected error when Fn is nil")
	}
	failing := &ListTeamStatusTool{
		Fn: func(ctx context.Context) ([]*Thread, error) { return nil, fmt.Errorf("boom") },
	}
	if res := failing.Execute(context.Background(), nil); !res.IsError || !strings.Contains(res.Error, "boom") {
		t.Errorf("expected propagated error, got %+v", res)
	}
}

// ─── recall_thread_result ─────────────────────────────────────────────────────

func TestRecallThreadResultTool_Execute(t *testing.T) {
	tm := New()
	th, _ := tm.Create(CreateParams{SessionID: "s1", AgentID: "Sam", Task: "x"})
	tm.Complete(th.ID, FinishSummary{
		Summary:       "fixed the bug",
		FilesModified: []string{"ws.go"},
		KeyDecisions:  []string{"used a ring buffer"},
		Status:        "completed",
	})

	tool := &RecallThreadResultTool{
		Fn: func(ctx context.Context, threadID string) (*Thread, error) {
			got, ok := tm.Get(threadID)
			if !ok {
				return nil, fmt.Errorf("thread %q not found", threadID)
			}
			return got, nil
		},
	}

	res := tool.Execute(context.Background(), map[string]any{"thread_id": th.ID})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Error)
	}
	for _, want := range []string{"fixed the bug", "ws.go", "used a ring buffer", "completed"} {
		if !strings.Contains(res.Output, want) {
			t.Errorf("output missing %q: %q", want, res.Output)
		}
	}

	// Still-running thread → "no result yet", not an error.
	pending, _ := tm.Create(CreateParams{SessionID: "s1", AgentID: "Dave", Task: "y"})
	res = tool.Execute(context.Background(), map[string]any{"thread_id": pending.ID})
	if res.IsError || !strings.Contains(res.Output, "no result yet") {
		t.Errorf("expected 'no result yet' for running thread, got %+v", res)
	}

	// Missing/invalid args.
	if res := tool.Execute(context.Background(), map[string]any{}); !res.IsError {
		t.Error("expected error when thread_id missing")
	}
	if res := tool.Execute(context.Background(), map[string]any{"thread_id": "nope"}); !res.IsError {
		t.Error("expected error for unknown thread")
	}
}

// ─── ActiveThreadIDs ──────────────────────────────────────────────────────────

func TestActiveThreadIDs(t *testing.T) {
	tm := New()
	a, _ := tm.Create(CreateParams{SessionID: "s1", AgentID: "Sam", Task: "a"})
	b, _ := tm.Create(CreateParams{SessionID: "s1", AgentID: "Dave", Task: "b"})
	done, _ := tm.Create(CreateParams{SessionID: "s1", AgentID: "Eve", Task: "c"})
	tm.Complete(done.ID, FinishSummary{Summary: "done", Status: "completed"})
	cancelled, _ := tm.Create(CreateParams{SessionID: "s1", AgentID: "Mallory", Task: "d"})
	tm.Cancel(cancelled.ID)
	_, _ = tm.Create(CreateParams{SessionID: "other", AgentID: "Sam", Task: "e"})

	ids := tm.ActiveThreadIDs("s1")
	if len(ids) != 2 {
		t.Fatalf("expected 2 active threads, got %d: %v", len(ids), ids)
	}
	got := map[string]bool{ids[0]: true, ids[1]: true}
	if !got[a.ID] || !got[b.ID] {
		t.Errorf("expected active IDs %s and %s, got %v", a.ID, b.ID, ids)
	}
	if ids := tm.ActiveThreadIDs(""); ids != nil {
		t.Errorf("empty session must return nil, got %v", ids)
	}
}
