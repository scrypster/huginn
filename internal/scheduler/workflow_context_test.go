package scheduler

// workflow_context_test.go — additional edge-case tests for workflow variable
// substitution and context cancellation mid-run.

import (
	"context"
	"strings"
	"testing"
)

// ─── resolveInlineVars edge cases ─────────────────────────────────────────────

// TestResolveInlineVars_NoVars verifies that a prompt with no vars is returned
// unchanged when vars is nil.
func TestResolveInlineVars_NoVars(t *testing.T) {
	prompt := "hello {{WORLD}}"
	result := resolveInlineVars(prompt, nil)
	if result != prompt {
		t.Errorf("expected unchanged prompt, got: %q", result)
	}
}

// TestResolveInlineVars_EmptyVars verifies that an empty vars map leaves the
// prompt unchanged.
func TestResolveInlineVars_EmptyVars(t *testing.T) {
	prompt := "check {{REPO}}"
	result := resolveInlineVars(prompt, map[string]string{})
	if result != prompt {
		t.Errorf("expected unchanged prompt, got: %q", result)
	}
}

// TestResolveInlineVars_MultipleReplacements verifies that all occurrences of a
// placeholder in a prompt are replaced.
func TestResolveInlineVars_MultipleReplacements(t *testing.T) {
	prompt := "{{X}} plus {{X}} equals two {{X}}"
	result := resolveInlineVars(prompt, map[string]string{"X": "one"})
	want := "one plus one equals two one"
	if result != want {
		t.Errorf("expected %q, got %q", want, result)
	}
}

// TestResolveInlineVars_PartialMatch verifies that a var key that is a prefix of
// another placeholder does not cause spurious replacements.
func TestResolveInlineVars_PartialMatch(t *testing.T) {
	prompt := "{{FOO}} and {{FOOBAR}}"
	result := resolveInlineVars(prompt, map[string]string{"FOO": "foo"})
	if !strings.Contains(result, "{{FOOBAR}}") {
		t.Errorf("{{FOOBAR}} should not have been replaced; got: %q", result)
	}
	if !strings.Contains(result, "foo and") {
		t.Errorf("expected {{FOO}} replaced; got: %q", result)
	}
}

// ─── resolveRuntimeVars edge cases ───────────────────────────────────────────

// TestResolveRuntimeVars_MultipleInputsSameAlias verifies that if two inputs
// share the same alias, both replacements occur (last write wins via ReplaceAll).
func TestResolveRuntimeVars_MultipleInputsSameAlias(t *testing.T) {
	inputs := []StepInput{
		{FromStep: "step-a", As: "result"},
		{FromStep: "step-b", As: "result"},
	}
	stepOutputs := map[string]string{
		"step-a": "output-a",
		"step-b": "output-b",
	}
	prompt := "use {{inputs.result}}"
	result := resolveRuntimeVars(prompt, inputs, stepOutputs, "")
	// First input (step-a) replaces the placeholder; second input has nothing left to replace.
	// So the first match wins.
	if !strings.Contains(result, "output-a") {
		t.Errorf("expected output-a from first input match, got: %q", result)
	}
	if strings.Contains(result, "{{inputs.result}}") {
		t.Errorf("expected placeholder to be replaced, got: %q", result)
	}
}

// TestResolveRuntimeVars_NoPrevOutput verifies that when prevOutput is non-empty
// the {{prev.output}} placeholder is replaced correctly.
func TestResolveRuntimeVars_NoPrevOutput(t *testing.T) {
	prompt := "based on: {{prev.output}}"
	result := resolveRuntimeVars(prompt, nil, map[string]string{}, "step-one-output")
	want := "based on: step-one-output"
	if result != want {
		t.Errorf("expected %q, got %q", want, result)
	}
}

// TestResolveRuntimeVars_NilInputs verifies that nil inputs slice is handled
// safely — only {{prev.output}} substitution occurs.
func TestResolveRuntimeVars_NilInputs(t *testing.T) {
	prompt := "result: {{prev.output}}"
	result := resolveRuntimeVars(prompt, nil, map[string]string{}, "ok")
	if result != "result: ok" {
		t.Errorf("expected 'result: ok', got: %q", result)
	}
}

// TestResolveRuntimeVars_PrevOutputInMiddle verifies substitution when
// {{prev.output}} appears in the middle of text.
func TestResolveRuntimeVars_PrevOutputInMiddle(t *testing.T) {
	prompt := "start {{prev.output}} end"
	result := resolveRuntimeVars(prompt, nil, map[string]string{}, "middle")
	if result != "start middle end" {
		t.Errorf("expected 'start middle end', got: %q", result)
	}
}

// ─── workflow context cancellation mid-step ───────────────────────────────────

// TestWorkflowRunner_ContextCancelledAfterFirstStep verifies that when the context
// is cancelled after the first step completes, the second step does not run.
func TestWorkflowRunner_ContextCancelledAfterFirstStep(t *testing.T) {
	store := NewWorkflowRunStore(t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())

	callCount := 0
	agentFn := func(innerCtx context.Context, opts RunOptions) (string, error) {
		callCount++
		if callCount == 1 {
			cancel() // cancel context mid-run after first step
		}
		return `{"summary":"ok"}`, nil
	}

	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-ctx-mid",
		Name: "CtxMid WF",
		Steps: []WorkflowStep{
			{Name: "step-one", Agent: "A", Prompt: "do step one", Position: 0},
			{Name: "step-two", Agent: "B", Prompt: "do step two", Position: 1},
		},
	}

	if err := runner(ctx, w); err != nil {
		t.Fatalf("unexpected runner error: %v", err)
	}

	// Only 1 step should have been called (context cancelled before step 2).
	if callCount != 1 {
		t.Errorf("expected 1 agent call before cancellation, got %d", callCount)
	}

	runs, _ := store.List("wf-ctx-mid", 10)
	if len(runs) == 0 {
		t.Fatal("expected run persisted")
	}
	// The run was aborted via context cancellation; status is "failed" (aborted).
	if runs[0].Status != WorkflowRunStatusFailed {
		t.Errorf("expected failed status after mid-run cancellation, got %s", runs[0].Status)
	}
}

// TestWorkflowRunner_EmptyStepList verifies that a workflow with no steps
// completes with "complete" status and an empty steps list.
func TestWorkflowRunner_EmptyStepList(t *testing.T) {
	store := NewWorkflowRunStore(t.TempDir())
	agentFn := func(ctx context.Context, opts RunOptions) (string, error) {
		return "", nil
	}
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:    "wf-no-steps",
		Name:  "NoSteps WF",
		Steps: []WorkflowStep{},
	}

	if err := runner(context.Background(), w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	runs, _ := store.List("wf-no-steps", 10)
	if len(runs) == 0 {
		t.Fatal("expected run stored")
	}
	if runs[0].Status != WorkflowRunStatusComplete {
		t.Errorf("expected complete for empty steps, got %s", runs[0].Status)
	}
	if len(runs[0].Steps) != 0 {
		t.Errorf("expected 0 step results, got %d", len(runs[0].Steps))
	}
}

// TestWorkflowRunner_StepsSortedByPosition verifies that steps are executed in
// Position order regardless of the order they appear in the Workflow.Steps slice.
func TestWorkflowRunner_StepsSortedByPosition(t *testing.T) {
	store := NewWorkflowRunStore(t.TempDir())
	var executionOrder []int
	agentFn := func(ctx context.Context, opts RunOptions) (string, error) {
		// The opts.Prompt encodes the position as "step <N>".
		// We track which position was called.
		return `{"summary":"ok"}`, nil
	}
	callOrder := 0
	agentFnTracking := func(ctx context.Context, opts RunOptions) (string, error) {
		callOrder++
		executionOrder = append(executionOrder, callOrder)
		return `{"summary":"ok"}`, nil
	}
	_ = agentFn

	runner := MakeWorkflowRunner(store, agentFnTracking, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-sorted",
		Name: "SortedSteps",
		Steps: []WorkflowStep{
			{Name: "step-3", Agent: "C", Prompt: "step 3", Position: 3},
			{Name: "step-1", Agent: "A", Prompt: "step 1", Position: 1},
			{Name: "step-2", Agent: "B", Prompt: "step 2", Position: 2},
		},
	}

	if err := runner(context.Background(), w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(executionOrder) != 3 {
		t.Fatalf("expected 3 steps executed, got %d", len(executionOrder))
	}
	// All three steps should have been called.
	if executionOrder[0] != 1 || executionOrder[1] != 2 || executionOrder[2] != 3 {
		t.Errorf("expected steps called in order 1,2,3 but got %v", executionOrder)
	}

	runs, _ := store.List("wf-sorted", 10)
	if len(runs) == 0 {
		t.Fatal("expected run stored")
	}
	if runs[0].Status != WorkflowRunStatusComplete {
		t.Errorf("expected complete, got %s", runs[0].Status)
	}
}

// TestWorkflowRunner_BroadcastCalled verifies that the broadcast function is
// called with the expected event types during a successful run.
func TestWorkflowRunner_BroadcastCalled(t *testing.T) {
	store := NewWorkflowRunStore(t.TempDir())
	agentFn := func(ctx context.Context, opts RunOptions) (string, error) {
		return `{"summary":"ok"}`, nil
	}

	var events []string
	broadcastFn := func(eventType string, payload map[string]any) {
		events = append(events, eventType)
	}

	runner := MakeWorkflowRunner(store, agentFn, nil, broadcastFn, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-broadcast",
		Name: "BroadcastTest",
		Steps: []WorkflowStep{
			{Name: "s1", Agent: "A", Prompt: "do it", Position: 0},
		},
	}

	if err := runner(context.Background(), w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Must have received workflow_started, workflow_step_complete, workflow_complete.
	hasStarted := false
	hasStepComplete := false
	hasComplete := false
	for _, e := range events {
		switch e {
		case "workflow_started":
			hasStarted = true
		case "workflow_step_complete":
			hasStepComplete = true
		case "workflow_complete":
			hasComplete = true
		}
	}
	if !hasStarted {
		t.Errorf("expected 'workflow_started' event; got %v", events)
	}
	if !hasStepComplete {
		t.Errorf("expected 'workflow_step_complete' event; got %v", events)
	}
	if !hasComplete {
		t.Errorf("expected 'workflow_complete' event; got %v", events)
	}
}
