package scheduler

import (
	"context"
	"testing"
	"time"
)

// TestWorkflowRunner_LatencyCaptured verifies the runner records a non-zero
// latency on every step (Phase 4 observability). Without this, dashboards
// can't distinguish a 50ms step from a 50s step and slow-step alerts are
// impossible.
func TestWorkflowRunner_LatencyCaptured(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}
	agentFn := func(_ context.Context, _ RunOptions) (string, error) {
		// Sleep just long enough to register a non-zero latency on every
		// platform without making the test slow.
		time.Sleep(20 * time.Millisecond)
		return "ok", nil
	}
	wf := &Workflow{
		ID: "wf-lat", Name: "lat",
		Steps: []WorkflowStep{
			{Position: 1, Name: "a", Agent: "x", Prompt: "go"},
			{Position: 2, Name: "b", Agent: "x", Prompt: "go"},
		},
	}
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	if err := runner(context.Background(), wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
	if len(store.runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(store.runs))
	}
	for _, sr := range store.runs[0].Steps {
		if sr.LatencyMs <= 0 {
			t.Errorf("step %q latency = %d ms, want > 0", sr.Slug, sr.LatencyMs)
		}
		if sr.StartedAt.IsZero() {
			t.Errorf("step %q StartedAt is zero", sr.Slug)
		}
		if sr.CompletedAt == nil || sr.CompletedAt.IsZero() {
			t.Errorf("step %q CompletedAt is nil/zero", sr.Slug)
		}
	}
}

// TestWorkflowRunner_LatencyBroadcast verifies the workflow_step_complete WS
// payload carries latency_ms so the live UI can render it without an extra
// fetch.
func TestWorkflowRunner_LatencyBroadcast(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}
	agentFn := func(_ context.Context, _ RunOptions) (string, error) {
		time.Sleep(5 * time.Millisecond)
		return "ok", nil
	}

	var sawLatency bool
	bcast := func(event string, payload map[string]any) {
		if event != "workflow_step_complete" {
			return
		}
		if v, ok := payload["latency_ms"].(int64); ok && v > 0 {
			sawLatency = true
		}
	}
	wf := &Workflow{
		ID: "wf-lb", Name: "lb",
		Steps: []WorkflowStep{{Position: 1, Name: "a", Agent: "x", Prompt: "go"}},
	}
	runner := MakeWorkflowRunner(store, agentFn, nil, bcast, nil, nil, "", nil, nil)
	if err := runner(context.Background(), wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
	if !sawLatency {
		t.Fatal("expected workflow_step_complete payload to include positive latency_ms")
	}
}
