package scheduler

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestRegisterWorkflow_EnabledEmptySchedule_IsOneShot(t *testing.T) {
	s := New()
	s.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error { return nil })
	w := &Workflow{ID: "oneshot", Enabled: true, Schedule: ""}
	if err := s.RegisterWorkflow(w); err != nil {
		t.Fatalf("one-shot (enabled, empty schedule) must not error: %v", err)
	}
	s.mu.Lock()
	_, ok := s.workflowEntries[w.ID]
	s.mu.Unlock()
	if ok {
		t.Fatal("one-shot must not create a cron entry")
	}
}

func TestRegisterWorkflow_DisabledDoesNotRegisterCron(t *testing.T) {
	s := New()
	s.SetWorkflowRunner(func(ctx context.Context, w *Workflow) error { return nil })
	w := &Workflow{ID: "off", Enabled: false, Schedule: "@every 1s"}
	if err := s.RegisterWorkflow(w); err != nil {
		t.Fatalf("disabled should no-op: %v", err)
	}
	s.mu.Lock()
	_, ok := s.workflowEntries[w.ID]
	s.mu.Unlock()
	if ok {
		t.Fatal("enabled:false must not register cron")
	}
}

func TestWorkflowRunner_SteveThenWinston_OutputFlows(t *testing.T) {
	var mu sync.Mutex
	prompts := map[string]string{}
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		mu.Lock()
		prompts[opts.AgentName] = opts.Prompt
		mu.Unlock()
		switch opts.AgentName {
		case "Steve":
			return "steve-draft", nil
		case "Winston":
			return "winston-review", nil
		}
		return "", nil
	}
	store := newTestRunStore()
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil)
	w := &Workflow{
		ID:   "pipeline",
		Name: "Steve then Winston",
		Steps: []WorkflowStep{
			{Position: 0, Name: "draft", Agent: "Steve", Prompt: "Draft the note."},
			{Position: 1, Name: "review", Agent: "Winston",
				Prompt: "Review {{inputs.draft}}",
				Inputs: []StepInput{{FromStep: "draft", As: "draft"}}},
		},
	}
	if err := runner(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompts["Winston"], "steve-draft") {
		t.Errorf("Winston did not receive Steve's output, prompt=%q", prompts["Winston"])
	}
	runs, _ := store.List("pipeline", 1)
	if len(runs) == 0 || runs[0].Status != WorkflowRunStatusComplete {
		t.Fatalf("want complete, got %+v", runs)
	}
}

func TestWorkflowRunner_CompanyWall_BlocksDeskOnlyAgent(t *testing.T) {
	var woke []string
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		woke = append(woke, opts.AgentName)
		return "ok", nil
	}
	store := newTestRunStore()
	gate := func(companyID, agent string) error {
		if companyID == "lab" && strings.EqualFold(agent, "Reggie") {
			return errCompanyWall("Lab", "Reggie")
		}
		return nil
	}
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil, WithCompanyGate(gate))
	w := &Workflow{
		ID:        "lab-wf",
		Name:      "Lab",
		CompanyID: "lab",
		Steps: []WorkflowStep{
			{Position: 0, Name: "wake", Agent: "Reggie", Prompt: "hello from lab"},
		},
	}
	if err := runner(context.Background(), w); err != nil {
		t.Fatalf("runner itself should not error: %v", err)
	}
	if len(woke) != 0 {
		t.Fatalf("Reggie must not be woken by a Lab workflow, woke=%v", woke)
	}
	runs, _ := store.List("lab-wf", 1)
	if len(runs) == 0 || runs[0].Status != WorkflowRunStatusFailed {
		t.Fatalf("want failed run, got %+v", runs)
	}
	if len(runs[0].Steps) == 0 || !strings.Contains(runs[0].Steps[0].Error, "Reggie") {
		t.Errorf("error should name Reggie, got %+v", runs[0].Steps)
	}
}

func TestWorkflowRunner_CompanyWall_AllowsSeatedTeammates(t *testing.T) {
	var woke []string
	agentFn := func(_ context.Context, opts RunOptions) (string, error) {
		woke = append(woke, opts.AgentName)
		return opts.AgentName + "-out", nil
	}
	store := newTestRunStore()
	gate := func(companyID, agent string) error {
		if companyID == "lab" && (agent == "Steve" || agent == "Winston") {
			return nil
		}
		return errCompanyWall("Lab", agent)
	}
	runner := MakeWorkflowRunner(store, agentFn, nil, nil, nil, nil, "", nil, nil, WithCompanyGate(gate))
	w := &Workflow{
		ID:        "lab-ok",
		CompanyID: "lab",
		Steps: []WorkflowStep{
			{Position: 0, Name: "draft", Agent: "Steve", Prompt: "draft"},
			{Position: 1, Name: "review", Agent: "Winston", Prompt: "review {{prev.output}}"},
		},
	}
	if err := runner(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if strings.Join(woke, ",") != "Steve,Winston" {
		t.Errorf("woke=%v", woke)
	}
}
