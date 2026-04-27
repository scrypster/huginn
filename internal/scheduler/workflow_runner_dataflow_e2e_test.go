package scheduler

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// TestWorkflowRunner_DataflowE2E_PrevAndNamedInputs verifies that a 3-step
// linear workflow can pipe data forward via BOTH {{prev.output}} and
// {{inputs.alias}}, in the same prompt, on the same step. This is the
// contract the workflow editor docs and the world-class plan promise:
// inter-step data flow is the cornerstone of multi-step agent workflows.
//
// Layout:
//   step-1 (triage)  → emits classification text
//   step-2 (research)→ emits a JSON summary
//   step-3 (compose) → consumes BOTH triage (named input) and research
//                      ({{prev.output}}) in a single prompt
func TestWorkflowRunner_DataflowE2E_PrevAndNamedInputs(t *testing.T) {
	var (
		mu                sync.Mutex
		capturedPrompts   = map[string]string{}
		capturedAgentName = map[string]string{}
	)

	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		capturedPrompts[opts.AgentName] = opts.Prompt
		capturedAgentName[opts.AgentName] = opts.AgentName
		switch opts.AgentName {
		case "triage":
			return "ISSUE-CLASSIFICATION: bug/p1/auth", nil
		case "research":
			return `{"root_cause":"missing CSRF check"}`, nil
		case "compose":
			return "ready", nil
		}
		return "", nil
	}

	store := newTestRunStore()
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)

	w := &Workflow{
		ID:   "wf-e2e",
		Name: "e2e dataflow",
		Steps: []WorkflowStep{
			{
				Position: 0,
				Name:     "triage",
				Agent:    "triage",
				Prompt:   "Triage the report.",
			},
			{
				Position: 1,
				Name:     "research",
				Agent:    "research",
				Prompt:   "Research given triage: {{inputs.t}}",
				Inputs:   []StepInput{{FromStep: "triage", As: "t"}},
			},
			{
				Position: 2,
				Name:     "compose",
				Agent:    "compose",
				Prompt:   "Compose using triage={{inputs.t}} and research={{prev.output}}",
				Inputs:   []StepInput{{FromStep: "triage", As: "t"}},
			},
		},
	}

	if err := runner(context.Background(), w); err != nil {
		t.Fatalf("runner: %v", err)
	}

	// Step 2 should have seen step-1's output via {{inputs.t}}.
	research := capturedPrompts["research"]
	if !strings.Contains(research, "ISSUE-CLASSIFICATION: bug/p1/auth") {
		t.Errorf("research step missing triage via {{inputs.t}}, prompt=%q", research)
	}

	// Step 3 should have seen BOTH:
	//   - {{inputs.t}} → step-1 (triage) output
	//   - {{prev.output}} → step-2 (research) output
	compose := capturedPrompts["compose"]
	if !strings.Contains(compose, "ISSUE-CLASSIFICATION: bug/p1/auth") {
		t.Errorf("compose step missing triage via {{inputs.t}}, prompt=%q", compose)
	}
	if !strings.Contains(compose, `"root_cause":"missing CSRF check"`) {
		t.Errorf("compose step missing research via {{prev.output}}, prompt=%q", compose)
	}

	runs, _ := store.List("wf-e2e", 10)
	if len(runs) == 0 || runs[0].Status != WorkflowRunStatusComplete {
		t.Fatalf("expected complete run, got %+v", runs)
	}
	if len(runs[0].Steps) != 3 {
		t.Fatalf("expected 3 step results, got %d", len(runs[0].Steps))
	}
	for _, sr := range runs[0].Steps {
		if sr.Status != "success" {
			t.Errorf("step %d status = %q, want success", sr.Position, sr.Status)
		}
	}
}

// TestWorkflowRunner_DataflowE2E_UnresolvedPlaceholderFails verifies that a
// step whose prompt still contains an unresolved placeholder (e.g.
// {{inputs.does_not_exist}} pointing to a non-existent prior step) fails
// the step rather than silently passing a useless prompt to the agent.
//
// This is a correctness/safety guarantee: a misconfigured workflow must
// surface its mistake loudly via a failed run, not by quietly producing a
// degraded output.
func TestWorkflowRunner_DataflowE2E_UnresolvedPlaceholderFails(t *testing.T) {
	var agentCalled bool
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		agentCalled = true
		return "ok", nil
	}
	store := newTestRunStore()
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "wf-unresolved",
		Name: "unresolved",
		Steps: []WorkflowStep{
			{Position: 0, Name: "only", Agent: "x",
				Prompt: "Use {{inputs.does_not_exist}} here",
				Inputs: []StepInput{{FromStep: "missing", As: "does_not_exist"}}},
		},
	}
	if err := runner(context.Background(), w); err != nil {
		t.Fatalf("runner: %v", err)
	}
	if agentCalled {
		t.Error("agent should NOT have been called when step has unresolved placeholders")
	}
	runs, _ := store.List("wf-unresolved", 1)
	if len(runs) == 0 {
		t.Fatal("expected at least one run")
	}
	if runs[0].Status != WorkflowRunStatusFailed {
		t.Errorf("status = %q, want %q", runs[0].Status, WorkflowRunStatusFailed)
	}
	if len(runs[0].Steps) == 0 || runs[0].Steps[0].Status != "failed" {
		t.Errorf("expected step to be marked failed, got %+v", runs[0].Steps)
	}
	if runs[0].Steps[0].Error == "" {
		t.Error("expected non-empty error explaining unresolved placeholder")
	}
	if !strings.Contains(strings.ToLower(runs[0].Steps[0].Error), "placeholder") &&
		!strings.Contains(strings.ToLower(runs[0].Steps[0].Error), "unresolved") {
		t.Errorf("error should mention the placeholder issue, got: %q", runs[0].Steps[0].Error)
	}
}
