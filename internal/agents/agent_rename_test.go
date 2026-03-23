package agents_test

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/tools"
)

// ---------------------------------------------------------------------------
// Agent.Rename
// ---------------------------------------------------------------------------

func TestAgent_Rename(t *testing.T) {
	a := &agents.Agent{Name: "OldName"}
	a.Rename(nil, "NewName")
	if a.Name != "NewName" {
		t.Errorf("expected NewName, got %s", a.Name)
	}
}

func TestAgent_Rename_Concurrent(t *testing.T) {
	a := &agents.Agent{Name: "Start"}
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			a.Rename(nil, "ConcurrentName")
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	if a.Name != "ConcurrentName" {
		t.Errorf("expected ConcurrentName, got %s", a.Name)
	}
}

// ---------------------------------------------------------------------------
// Agent.SnapshotHistory edge cases
// ---------------------------------------------------------------------------

func TestAgent_SnapshotHistory_ZeroN(t *testing.T) {
	a := &agents.Agent{Name: "A"}
	a.AppendHistory(backend.Message{Role: "user", Content: "msg1"}, backend.Message{Role: "assistant", Content: "resp1"})
	// n <= 0 returns all
	snap := a.SnapshotHistory(0)
	if len(snap) != 2 {
		t.Errorf("expected 2, got %d", len(snap))
	}
}

func TestAgent_SnapshotHistory_NegativeN(t *testing.T) {
	a := &agents.Agent{Name: "A"}
	a.AppendHistory(backend.Message{Role: "user", Content: "msg1"})
	snap := a.SnapshotHistory(-1)
	if len(snap) != 1 {
		t.Errorf("expected 1, got %d", len(snap))
	}
}

func TestAgent_SnapshotHistory_NExceedsLen(t *testing.T) {
	a := &agents.Agent{Name: "A"}
	a.AppendHistory(backend.Message{Role: "user", Content: "msg1"})
	snap := a.SnapshotHistory(100)
	if len(snap) != 1 {
		t.Errorf("expected 1, got %d", len(snap))
	}
}

func TestAgent_SnapshotHistory_Empty(t *testing.T) {
	a := &agents.Agent{Name: "A"}
	snap := a.SnapshotHistory(5)
	if len(snap) != 0 {
		t.Errorf("expected 0, got %d", len(snap))
	}
}


// ---------------------------------------------------------------------------
// ConsultAgentTool.Permission
// ---------------------------------------------------------------------------

func TestConsultAgentTool_Permission(t *testing.T) {
	reg := makeTestRegistry()
	var depth int32
	tool := agents.NewConsultAgentTool(reg, &mockBackend{}, &depth, nil, nil)
	if tool.Permission() != tools.PermRead {
		t.Errorf("expected PermRead, got %d", tool.Permission())
	}
}

// ---------------------------------------------------------------------------
// ConsultAgentTool.Execute with missing args
// ---------------------------------------------------------------------------

func TestConsultAgentTool_Execute_MissingAgentName(t *testing.T) {
	reg := makeTestRegistry()
	var depth int32
	tool := agents.NewConsultAgentTool(reg, &mockBackend{response: "ans"}, &depth, nil, nil)
	result := tool.Execute(context.Background(), map[string]any{
		"question": "hello",
	})
	if !result.IsError {
		t.Error("expected error for missing agent_name")
	}
}

func TestConsultAgentTool_Execute_MissingQuestion(t *testing.T) {
	reg := makeTestRegistry()
	var depth int32
	tool := agents.NewConsultAgentTool(reg, &mockBackend{response: "ans"}, &depth, nil, nil)
	result := tool.Execute(context.Background(), map[string]any{
		"agent_name": "Mark",
	})
	if !result.IsError {
		t.Error("expected error for missing question")
	}
}

func TestConsultAgentTool_Execute_EmptyArgs(t *testing.T) {
	reg := makeTestRegistry()
	var depth int32
	tool := agents.NewConsultAgentTool(reg, &mockBackend{response: "ans"}, &depth, nil, nil)
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for empty args")
	}
}

// ---------------------------------------------------------------------------
// RegisterConsultTool
// ---------------------------------------------------------------------------

func TestRegisterConsultTool_NilRegistry(t *testing.T) {
	reg := tools.NewRegistry()
	// agentReg is nil -> should be a no-op, not a panic.
	agents.RegisterConsultTool(reg, nil, &mockBackend{}, new(int32), nil, nil, nil)
	if len(reg.AllSchemas()) != 0 {
		t.Error("expected no tools registered when agentReg is nil")
	}
}

func TestRegisterConsultTool_Success(t *testing.T) {
	toolReg := tools.NewRegistry()
	agentReg := makeTestRegistry()
	var depth int32
	agents.RegisterConsultTool(toolReg, agentReg, &mockBackend{}, &depth, nil, nil, nil)
	schemas := toolReg.AllSchemas()
	found := false
	for _, s := range schemas {
		if s.Function.Name == "consult_agent" {
			found = true
		}
	}
	if !found {
		t.Error("expected consult_agent tool to be registered")
	}
}

// ---------------------------------------------------------------------------
// BuildRegistry
// ---------------------------------------------------------------------------

func TestBuildRegistry_SyncsModels(t *testing.T) {
	cfg := &agents.AgentsConfig{
		Agents: []agents.AgentDef{
			{Name: "X", Model: "custom-reasoner:7b"},
		},
	}
	models := modelconfig.DefaultModels()
	reg := agents.BuildRegistry(cfg, models)
	if _, ok := reg.ByName("X"); !ok {
		t.Error("expected agent X in registry")
	}
}

// ---------------------------------------------------------------------------
// normalizeAction via ParseDirective (exercising edge cases)
// ---------------------------------------------------------------------------

func TestParseDirective_UnknownAction_DefaultsToChat(t *testing.T) {
	reg := makeTestRegistry()
	// "dance" is not in actionAliases → defaults to "chat"
	d := agents.ParseDirective("Have Chris dance around the problem", reg)
	if d == nil {
		t.Fatal("expected directive")
	}
	if d.Steps[0].Action != "chat" {
		t.Errorf("expected chat for unknown action, got %q", d.Steps[0].Action)
	}
}

func TestParseDirective_MoreAliases(t *testing.T) {
	reg := makeTestRegistry()
	cases := []struct {
		input  string
		action string
	}{
		{"Have Mark think about the problem", "reason"},
		{"Have Mark evaluate the solution", "reason"},
		{"Have Mark assess the risk", "reason"},
		{"Have Mark verify the output", "reason"},
		{"Have Steve write the handler", "code"},
		{"Have Steve build the module", "code"},
	}
	for _, c := range cases {
		d := agents.ParseDirective(c.input, reg)
		if d == nil {
			t.Errorf("input %q: expected directive, got nil", c.input)
			continue
		}
		if d.Steps[0].Action != c.action {
			t.Errorf("input %q: expected %q, got %q", c.input, c.action, d.Steps[0].Action)
		}
	}
}

// ---------------------------------------------------------------------------
// AgentRegistry.Names
// ---------------------------------------------------------------------------

func TestAgentRegistry_Names(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Alpha"})
	reg.Register(&agents.Agent{Name: "Beta"})
	names := reg.Names()
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d", len(names))
	}
}

// ---------------------------------------------------------------------------
// NewConsultAgentToolWithMemory
// ---------------------------------------------------------------------------

func TestNewConsultAgentToolWithMemory_NoNilPanic(t *testing.T) {
	reg := makeTestRegistry()
	var depth int32
	// All optional callbacks nil.
	tool := agents.NewConsultAgentToolWithMemory(reg, &mockBackend{response: "ans"}, &depth,
		nil, nil, nil, nil, "from-agent")
	if tool == nil {
		t.Fatal("expected non-nil tool")
	}
	if tool.Name() != "consult_agent" {
		t.Errorf("expected consult_agent, got %s", tool.Name())
	}
}

// ---------------------------------------------------------------------------
// DelegationContext empty history
// ---------------------------------------------------------------------------

func TestAgent_DelegationContext_EmptyHistory(t *testing.T) {
	a := &agents.Agent{Name: "Empty"}
	ctx := a.DelegationContext()
	if len(ctx) != 0 {
		t.Errorf("expected empty, got %d", len(ctx))
	}
}

// ---------------------------------------------------------------------------
// ConsultAgentTool.Execute with context_summary
// ---------------------------------------------------------------------------

func TestConsultAgentTool_Execute_WithContextSummary(t *testing.T) {
	reg := makeTestRegistry()
	mb := &mockBackend{response: "I see the context"}
	var depth int32
	tool := agents.NewConsultAgentTool(reg, mb, &depth, nil, nil)

	result := tool.Execute(context.Background(), map[string]any{
		"agent_name":      "Mark",
		"question":        "Is this safe?",
		"context_summary": "We are refactoring the auth module",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
}

// ---------------------------------------------------------------------------
// ConsultAgentTool.Execute - agent with no system prompt
// ---------------------------------------------------------------------------

func TestConsultAgentTool_Execute_NoSystemPrompt(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Blank", ModelID: "m"})

	mb := &mockBackend{response: "answer from blank"}
	var depth int32
	tool := agents.NewConsultAgentTool(reg, mb, &depth, nil, nil)

	result := tool.Execute(context.Background(), map[string]any{
		"agent_name": "Blank",
		"question":   "Hello?",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
}

