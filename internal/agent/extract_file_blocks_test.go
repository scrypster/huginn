package agent

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/modelconfig"
	toolspkg "github.com/scrypster/huginn/internal/tools"
)

func TestLoadAgentSummaries_NilMemoryStore(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
	result := o.loadAgentSummaries(context.Background(), "Chris")
	if result != nil {
		t.Errorf("expected nil for nil memory store, got %v", result)
	}
}

func TestDispatch_NilRegistry(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
	handled, err := o.Dispatch(
		context.Background(),
		"hello",
		func(s string) {},
		func(s string, _ string, m map[string]any) {},
		func(s string, _ string, r toolspkg.ToolResult) {},
		func(s string) {},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Error("expected handled=false with nil registry")
	}
}

func TestDispatch_NoDirectiveMatch(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Chris", ModelID: "m1"})
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
	o.SetAgentRegistry(reg)

	// Input that doesn't match any directive pattern
	handled, err := o.Dispatch(
		context.Background(),
		"what is the weather today",
		func(s string) {},
		func(s string, _ string, m map[string]any) {},
		func(s string, _ string, r toolspkg.ToolResult) {},
		func(s string) {},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Error("expected handled=false for non-directive input")
	}
}

func TestOrchestrator_SetMemoryStore_Round11(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
	o.SetMemoryStore(nil) // should not panic
}

func TestOrchestrator_MachineID_Round11(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
	o.WithMachineID("test-machine-123")
	if o.MachineID() != "test-machine-123" {
		t.Errorf("expected 'test-machine-123', got %q", o.MachineID())
	}
}
