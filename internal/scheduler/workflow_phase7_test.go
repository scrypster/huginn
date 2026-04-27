package scheduler

import (
	"context"
	"testing"
)

// TestRunner_ForwardsModelOverride verifies the runner copies
// step.ModelOverride into RunOptions.ModelOverride so the agent backend can
// honour it. Without this the per-step model setting is ignored entirely.
func TestRunner_ForwardsModelOverride(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}
	var seenOverrides []string
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		seenOverrides = append(seenOverrides, opts.ModelOverride)
		return "ok", nil
	}
	wf := &Workflow{
		ID: "wf-mo", Name: "model-override",
		Steps: []WorkflowStep{
			{Position: 1, Name: "a", Agent: "x", Prompt: "p", ModelOverride: "haiku"},
			{Position: 2, Name: "b", Agent: "x", Prompt: "p"}, // no override
			{Position: 3, Name: "c", Agent: "x", Prompt: "p", ModelOverride: "opus"},
		},
	}
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	if err := runner(context.Background(), wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
	want := []string{"haiku", "", "opus"}
	if len(seenOverrides) != len(want) {
		t.Fatalf("overrides len = %d, want %d (got %v)", len(seenOverrides), len(want), seenOverrides)
	}
	for i, w := range want {
		if seenOverrides[i] != w {
			t.Errorf("step %d override = %q, want %q", i+1, seenOverrides[i], w)
		}
	}
}

// TestRunner_EmptyOverride_NoOpThroughChain verifies that an absent
// ModelOverride simply leaves the override empty in RunOptions — never
// surfaces a stale value from a prior step. (Backwards-compat test for
// pre-Phase-7 workflows that don't specify the field at all.)
func TestRunner_EmptyOverride_NoOpThroughChain(t *testing.T) {
	t.Parallel()
	store := &mockRunStore{}
	var seenOverride string
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		seenOverride = opts.ModelOverride
		return "ok", nil
	}
	wf := &Workflow{
		ID: "wf-no-override", Name: "no-override",
		Steps: []WorkflowStep{
			{Position: 1, Name: "a", Agent: "x", Prompt: "p"},
		},
	}
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	if err := runner(context.Background(), wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
	if seenOverride != "" {
		t.Errorf("expected empty override, got %q", seenOverride)
	}
}
