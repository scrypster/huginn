package scheduler

import (
	"context"
	"strings"
	"testing"
)

// TestWorkflowRunner_UnresolvedPlaceholder_FailsStep verifies that a step whose
// prompt still contains an unresolved {{inputs.nonexistent}} placeholder after
// variable resolution is failed immediately with a clear error message instead
// of forwarding the garbled prompt to the agent.
func TestWorkflowRunner_UnresolvedPlaceholder_FailsStep(t *testing.T) {
	store := NewWorkflowRunStore(t.TempDir())
	agentCalls := 0
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		agentCalls++
		return `{"summary":"ok"}`, nil
	}

	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-placeholder-fail",
		Name: "Placeholder Fail WF",
		Steps: []WorkflowStep{
			{
				Name:     "bad-step",
				Agent:    "TestAgent",
				Position: 0,
				// Inputs declares "result" but from_step "nonexistent" does not exist,
				// so {{inputs.result}} will remain unreplaced after resolution.
				Prompt: "Analyze: {{inputs.result}}",
				Inputs: []StepInput{
					{FromStep: "nonexistent-step", As: "result"},
				},
			},
		},
	}

	if err := runner(context.Background(), w); err != nil {
		t.Fatalf("runner returned unexpected error: %v", err)
	}

	// The agent must NOT have been called — the step should fail before that.
	if agentCalls != 0 {
		t.Errorf("agent was called %d times; expected 0 (step should fail before reaching agent)", agentCalls)
	}

	runs, err := store.List("wf-placeholder-fail", 10)
	if err != nil || len(runs) == 0 {
		t.Fatal("expected run to be stored")
	}
	run := runs[0]

	if len(run.Steps) == 0 {
		t.Fatal("expected step result in run")
	}
	step := run.Steps[0]

	if step.Status != "failed" {
		t.Errorf("expected step status=failed, got %q", step.Status)
	}
	if !strings.Contains(step.Error, errUnresolvedPlaceholder) {
		t.Errorf("expected error to contain %q, got: %q", errUnresolvedPlaceholder, step.Error)
	}
	if !strings.Contains(step.Error, "{{inputs.result}}") {
		t.Errorf("expected error to list the unresolved placeholder, got: %q", step.Error)
	}
}

// TestWorkflowRunner_UnresolvedPlaceholder_OnFailureContinue verifies that
// when the step with unresolved placeholders has on_failure:continue, the
// runner continues to the next step rather than aborting.
func TestWorkflowRunner_UnresolvedPlaceholder_OnFailureContinue(t *testing.T) {
	store := NewWorkflowRunStore(t.TempDir())
	agentCalls := 0
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		agentCalls++
		return `{"summary":"ok"}`, nil
	}

	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-placeholder-continue",
		Name: "Placeholder Continue WF",
		Steps: []WorkflowStep{
			{
				Name:      "bad-step",
				Agent:     "TestAgent",
				Position:  0,
				OnFailure: "continue",
				Prompt:    "bad: {{inputs.missing}}",
				Inputs:    []StepInput{{FromStep: "nowhere", As: "missing"}},
			},
			{
				Name:     "good-step",
				Agent:    "TestAgent",
				Position: 1,
				Prompt:   "do something normal",
			},
		},
	}

	if err := runner(context.Background(), w); err != nil {
		t.Fatalf("runner error: %v", err)
	}

	// The second (good) step should still run.
	if agentCalls != 1 {
		t.Errorf("expected 1 agent call (for the good step), got %d", agentCalls)
	}

	runs, _ := store.List("wf-placeholder-continue", 10)
	if len(runs) == 0 {
		t.Fatal("expected run to be stored")
	}
	if runs[0].Status != WorkflowRunStatusPartial {
		t.Errorf("expected status partial (one failed, one succeeded), got %s", runs[0].Status)
	}
}

// TestWorkflowRunner_AllPlaceholdersResolved_Succeeds verifies that a step
// whose prompt has all placeholders correctly resolved reaches the agent
// without any placeholder-related failure.
func TestWorkflowRunner_AllPlaceholdersResolved_Succeeds(t *testing.T) {
	store := NewWorkflowRunStore(t.TempDir())
	var capturedPrompt string
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		capturedPrompt = opts.Prompt
		return `{"summary":"ok"}`, nil
	}

	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-placeholder-resolved",
		Name: "Placeholder Resolved WF",
		Steps: []WorkflowStep{
			{
				Name:     "first-step",
				Agent:    "TestAgent",
				Position: 0,
				Prompt:   "step one output",
			},
			{
				Name:     "second-step",
				Agent:    "TestAgent",
				Position: 1,
				Prompt:   "use: {{inputs.result}}",
				Inputs:   []StepInput{{FromStep: "first-step", As: "result"}},
			},
		},
	}

	if err := runner(context.Background(), w); err != nil {
		t.Fatalf("runner error: %v", err)
	}

	if capturedPrompt == "" {
		t.Fatal("second step was not called")
	}
	// The placeholder must be gone — no literal {{ should remain in the final prompt.
	if strings.Contains(capturedPrompt, "{{") {
		t.Errorf("prompt still contains unresolved placeholder: %q", capturedPrompt)
	}

	runs, _ := store.List("wf-placeholder-resolved", 10)
	if len(runs) == 0 {
		t.Fatal("expected run to be stored")
	}
	if runs[0].Status != WorkflowRunStatusComplete {
		t.Errorf("expected complete, got %s", runs[0].Status)
	}
}
