package threadmgr

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
)

// TestIsBlockedByFailure_DetectsCancelledUpstream verifies the new method.
func TestIsBlockedByFailure_DetectsCancelledUpstream(t *testing.T) {
	tm := New()
	up, _ := tm.Create(CreateParams{SessionID: "sess", AgentID: "a", Task: "up"})
	down, _ := tm.Create(CreateParams{SessionID: "sess", AgentID: "b", Task: "down", DependsOn: []string{up.ID}})

	if tm.IsBlockedByFailure(down.ID) {
		t.Error("should not be blocked before upstream completes")
	}

	tm.Cancel(up.ID)

	if !tm.IsBlockedByFailure(down.ID) {
		t.Error("should be blocked after upstream is cancelled")
	}
}

// TestIsBlockedByFailure_DetectsErrorUpstream verifies error propagation.
func TestIsBlockedByFailure_DetectsErrorUpstream(t *testing.T) {
	tm := New()
	up, _ := tm.Create(CreateParams{SessionID: "sess", AgentID: "a", Task: "up"})
	down, _ := tm.Create(CreateParams{SessionID: "sess", AgentID: "b", Task: "down", DependsOn: []string{up.ID}})

	// Force upstream to error state.
	tm.mu.Lock()
	if thr, ok := tm.threads[up.ID]; ok {
		thr.Status = StatusError
	}
	tm.mu.Unlock()

	if !tm.IsBlockedByFailure(down.ID) {
		t.Error("should be blocked when upstream is in error state")
	}
}

// TestEvaluateDAG_CascadeCancelsOnUpstreamFailure verifies EvaluateDAG fast-fails.
func TestEvaluateDAG_CascadeCancelsOnUpstreamFailure(t *testing.T) {
	tm := New()
	sess := &session.Session{ID: "sess-cascade"}

	up, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "a", Task: "up"})
	down, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "b", Task: "down", DependsOn: []string{up.ID}})

	tm.Cancel(up.ID)

	// EvaluateDAG should cascade-cancel down.
	tm.EvaluateDAG(context.Background(), sess.ID, nil, sess, nil, nil, func(sessID, event string, payload map[string]any) {}, nil)

	// Give it a moment to process.
	time.Sleep(20 * time.Millisecond)

	thr, ok := tm.Get(down.ID)
	if !ok {
		t.Fatal("downstream thread should still exist")
	}
	if thr.Status != StatusCancelled {
		t.Errorf("expected downstream to be StatusCancelled, got %s", thr.Status)
	}
	_ = up
}
