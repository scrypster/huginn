package server

import (
	"testing"
	"github.com/scrypster/huginn/internal/scheduler"
)

func TestValidateSubWorkflowCycles_DetectsDirectCycle(t *testing.T) {
	wfA := &scheduler.Workflow{ID: "wf-a", Steps: []scheduler.WorkflowStep{{Name: "s1", SubWorkflow: "wf-b"}}}
	wfB := &scheduler.Workflow{ID: "wf-b", Steps: []scheduler.WorkflowStep{{Name: "s1", SubWorkflow: "wf-a"}}}

	if err := validateSubWorkflowCycles(wfA, []*scheduler.Workflow{wfA, wfB}); err == nil {
		t.Error("expected cycle error for A→B→A, got nil")
	}
}

func TestValidateSubWorkflowCycles_DetectsLongCycle(t *testing.T) {
	wfA := &scheduler.Workflow{ID: "wf-a", Steps: []scheduler.WorkflowStep{{Name: "s1", SubWorkflow: "wf-b"}}}
	wfB := &scheduler.Workflow{ID: "wf-b", Steps: []scheduler.WorkflowStep{{Name: "s1", SubWorkflow: "wf-c"}}}
	wfC := &scheduler.Workflow{ID: "wf-c", Steps: []scheduler.WorkflowStep{{Name: "s1", SubWorkflow: "wf-a"}}}

	if err := validateSubWorkflowCycles(wfA, []*scheduler.Workflow{wfA, wfB, wfC}); err == nil {
		t.Error("expected cycle error for A→B→C→A, got nil")
	}
}

func TestValidateSubWorkflowCycles_AllowsAcyclicTree(t *testing.T) {
	wfA := &scheduler.Workflow{ID: "wf-a", Steps: []scheduler.WorkflowStep{{Name: "s1", SubWorkflow: "wf-b"}}}
	wfB := &scheduler.Workflow{ID: "wf-b", Steps: []scheduler.WorkflowStep{{Name: "s1", SubWorkflow: "wf-c"}}}
	wfC := &scheduler.Workflow{ID: "wf-c", Steps: []scheduler.WorkflowStep{{Name: "s1", Prompt: "leaf step"}}}

	if err := validateSubWorkflowCycles(wfA, []*scheduler.Workflow{wfA, wfB, wfC}); err != nil {
		t.Errorf("expected no error for acyclic A→B→C, got: %v", err)
	}
}

func TestValidateSubWorkflowCycles_AllowsDanglingRef(t *testing.T) {
	wfA := &scheduler.Workflow{ID: "wf-a", Steps: []scheduler.WorkflowStep{{Name: "s1", SubWorkflow: "wf-missing"}}}

	if err := validateSubWorkflowCycles(wfA, []*scheduler.Workflow{wfA}); err != nil {
		t.Errorf("dangling ref should not be flagged as cycle, got: %v", err)
	}
}
