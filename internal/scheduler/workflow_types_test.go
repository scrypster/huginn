package scheduler

import (
	"testing"
	"time"
)

func TestWorkflow_DefaultOnFailure(t *testing.T) {
	step := WorkflowStep{Prompt: "do something", Position: 10}
	if step.OnFailure != "" {
		t.Errorf("want empty default OnFailure, got %q", step.OnFailure)
	}
	if step.EffectiveOnFailure() != "stop" {
		t.Errorf("EffectiveOnFailure() want stop, got %q", step.EffectiveOnFailure())
	}
}

func TestWorkflow_InlineFields(t *testing.T) {
	w := Workflow{
		ID:       "wf1",
		Slug:     "morning-pipeline",
		Name:     "Morning Pipeline",
		Schedule: "0 9 * * 1-5",
		Tags:     []string{"dev", "ci"},
		Steps: []WorkflowStep{
			{Name: "pr-review", Agent: "Chris", Prompt: "review PRs", Position: 0},
			{Name: "build-health", Agent: "Chris", Prompt: "check build", Position: 1, OnFailure: "continue"},
		},
	}
	if len(w.Steps) != 2 {
		t.Fatalf("want 2 steps, got %d", len(w.Steps))
	}
	if len(w.Tags) != 2 {
		t.Errorf("want 2 tags, got %d", len(w.Tags))
	}
}

func TestWorkflowStep_LegacyRoutineField(t *testing.T) {
	step := WorkflowStep{Routine: "old-slug", Position: 5}
	if step.Routine != "old-slug" {
		t.Errorf("Routine field not preserved: %q", step.Routine)
	}
}

func TestWorkflowRun_Fields(t *testing.T) {
	run := WorkflowRun{
		ID:         "run1",
		WorkflowID: "wf1",
		Status:     WorkflowRunStatusRunning,
		StartedAt:  time.Now().UTC(),
	}
	if run.Status != WorkflowRunStatusRunning {
		t.Error("status mismatch")
	}
}
