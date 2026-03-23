package agents_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// mockBackend is a test backend that returns a fixed response.
type mockBackend struct {
	response string
	called   int32
	model    string
	tools    []backend.Tool
}

func (m *mockBackend) ChatCompletion(ctx context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	atomic.AddInt32(&m.called, 1)
	m.model = req.Model
	m.tools = req.Tools
	if req.OnToken != nil {
		req.OnToken(m.response)
	}
	return &backend.ChatResponse{Content: m.response, DoneReason: "stop"}, nil
}
func (m *mockBackend) Health(ctx context.Context) error   { return nil }
func (m *mockBackend) Shutdown(ctx context.Context) error { return nil }
func (m *mockBackend) ContextWindow() int               { return 128_000 }

func TestConsultAgentTool_Interface(t *testing.T) {
	reg := makeTestRegistry()
	var depth int32
	tool := agents.NewConsultAgentTool(reg, &mockBackend{}, &depth, nil, nil)
	// Must implement tools.Tool
	var _ tools.Tool = tool
}

func TestConsultAgentTool_Name(t *testing.T) {
	reg := makeTestRegistry()
	var depth int32
	tool := agents.NewConsultAgentTool(reg, &mockBackend{}, &depth, nil, nil)
	if tool.Name() != "consult_agent" {
		t.Errorf("expected consult_agent, got %s", tool.Name())
	}
}

func TestConsultAgentTool_Execute_Success(t *testing.T) {
	reg := makeTestRegistry()
	mb := &mockBackend{response: "Looks safe to me."}
	var depth int32
	tool := agents.NewConsultAgentTool(reg, mb, &depth, nil, nil)

	result := tool.Execute(context.Background(), map[string]any{
		"agent_name": "Mark",
		"question":   "Is this refactor safe?",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Output == "" {
		t.Error("expected non-empty output")
	}
	// Output should contain Mark's name and response
	if !containsStr(result.Output, "Mark") {
		t.Errorf("output should mention Mark: %s", result.Output)
	}
}

func TestConsultAgentTool_Execute_DepthGuard(t *testing.T) {
	reg := makeTestRegistry()
	mb := &mockBackend{response: "answer"}
	var depth int32
	atomic.StoreInt32(&depth, 1) // already at max depth

	tool := agents.NewConsultAgentTool(reg, mb, &depth, nil, nil)
	result := tool.Execute(context.Background(), map[string]any{
		"agent_name": "Mark",
		"question":   "recursive question",
	})

	if !result.IsError {
		t.Error("expected error when depth >= 1")
	}
	// Backend should NOT have been called
	if atomic.LoadInt32(&mb.called) != 0 {
		t.Error("backend should not be called when depth limit reached")
	}
}

func TestConsultAgentTool_Execute_UnknownAgent(t *testing.T) {
	reg := makeTestRegistry()
	var depth int32
	tool := agents.NewConsultAgentTool(reg, &mockBackend{}, &depth, nil, nil)

	result := tool.Execute(context.Background(), map[string]any{
		"agent_name": "Nobody",
		"question":   "hello",
	})

	if !result.IsError {
		t.Error("expected error for unknown agent")
	}
}

func TestConsultAgentTool_Execute_NoToolsForDelegatee(t *testing.T) {
	reg := makeTestRegistry()
	mb := &mockBackend{response: "answer"}
	var depth int32
	tool := agents.NewConsultAgentTool(reg, mb, &depth, nil, nil)

	tool.Execute(context.Background(), map[string]any{
		"agent_name": "Mark",
		"question":   "Is this right?",
	})

	// The ChatCompletion call must have had nil/empty tools
	if len(mb.tools) != 0 {
		t.Errorf("delegatees must not receive tools, got %d", len(mb.tools))
	}
}

func TestConsultAgentTool_Execute_UsesAgentModel(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Mark", ModelID: "deepseek-r1:14b"})

	mb := &mockBackend{response: "answer"}
	var depth int32
	tool := agents.NewConsultAgentTool(reg, mb, &depth, nil, nil)

	tool.Execute(context.Background(), map[string]any{
		"agent_name": "Mark",
		"question":   "Is this right?",
	})

	if mb.model != "deepseek-r1:14b" {
		t.Errorf("expected deepseek-r1:14b, got %s", mb.model)
	}
}

func TestConsultAgentTool_Execute_DepthResetsAfterCall(t *testing.T) {
	reg := makeTestRegistry()
	mb := &mockBackend{response: "answer"}
	var depth int32

	tool := agents.NewConsultAgentTool(reg, mb, &depth, nil, nil)
	tool.Execute(context.Background(), map[string]any{
		"agent_name": "Mark",
		"question":   "Q1",
	})

	// depth should be back to 0 after the call
	if atomic.LoadInt32(&depth) != 0 {
		t.Errorf("depth not reset after call: %d", atomic.LoadInt32(&depth))
	}

	// Should be able to call again
	result := tool.Execute(context.Background(), map[string]any{
		"agent_name": "Mark",
		"question":   "Q2",
	})
	if result.IsError {
		t.Errorf("second call failed: %s", result.Error)
	}
}

func TestConsultAgentTool_Execute_TUICallbacks(t *testing.T) {
	reg := makeTestRegistry()
	mb := &mockBackend{response: "insight"}
	var depth int32
	var startFrom, startTo, doneFrom, doneTo string
	var tokens []string

	onStart := func(from, to, q string) { startFrom = from; startTo = to }
	onDone := func(from, to, ans string) { doneFrom = from; doneTo = ans }
	onToken := func(agent, tok string) { tokens = append(tokens, tok) }

	tool := agents.NewConsultAgentToolFull(reg, mb, &depth, onStart, onDone, onToken)
	tool.Execute(context.Background(), map[string]any{
		"agent_name":      "Mark",
		"question":        "Review this",
		"context_summary": "refactoring auth",
	})

	if startFrom == "" || startTo != "Mark" {
		t.Errorf("onStart not called correctly: from=%s to=%s", startFrom, startTo)
	}
	if doneFrom == "" || doneTo == "" {
		t.Errorf("onDone not called: from=%s to=%s", doneFrom, doneTo)
	}
	if len(tokens) == 0 {
		t.Error("expected token callbacks")
	}
}

func TestConsultAgentTool_Schema_HasRequiredFields(t *testing.T) {
	reg := makeTestRegistry()
	var depth int32
	tool := agents.NewConsultAgentTool(reg, &mockBackend{}, &depth, nil, nil)
	schema := tool.Schema()

	if schema.Function.Name != "consult_agent" {
		t.Errorf("schema name wrong: %s", schema.Function.Name)
	}
	required := schema.Function.Parameters.Required
	found := false
	for _, r := range required {
		if r == "agent_name" {
			found = true
		}
	}
	if !found {
		t.Error("agent_name must be required")
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
