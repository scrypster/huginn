package agents

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

// mockSpaceChecker implements SpaceChecker for tests.
type mockSpaceChecker struct {
	members []string
	err     error
}

func (m *mockSpaceChecker) SpaceMembers(_ string) ([]string, error) {
	return m.members, m.err
}

// mockBackendConsult is a minimal backend that returns a fixed response.
type mockBackendConsult struct {
	response string
	err      error
}

func (b *mockBackendConsult) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	if req.OnToken != nil && b.response != "" {
		req.OnToken(b.response)
	}
	if b.err != nil {
		return nil, b.err
	}
	return &backend.ChatResponse{Content: b.response}, nil
}

func (b *mockBackendConsult) Health(_ context.Context) error          { return nil }
func (b *mockBackendConsult) Shutdown(_ context.Context) error        { return nil }
func (b *mockBackendConsult) ContextWindow() int                      { return 8192 }

func newTestConsultTool(reg *AgentRegistry, b backend.Backend) *ConsultAgentTool {
	depth := new(int32)
	return NewConsultAgentTool(reg, b, depth, nil, nil)
}

func TestConsultSpaceGuard_MemberAllowed(t *testing.T) {
	reg := NewRegistry()
	target := &Agent{Name: "bob", SystemPrompt: "I am Bob."}
	reg.Register(target)

	tool := newTestConsultTool(reg, &mockBackendConsult{response: "hello"})
	tool.WithSpaceContext("space-1", &mockSpaceChecker{members: []string{"bob"}})

	result := tool.Execute(context.Background(), map[string]any{
		"agent_name": "bob",
		"question":   "what is 2+2?",
	})
	if result.IsError {
		t.Fatalf("expected success for member bob, got error: %s", result.Error)
	}
}

func TestConsultSpaceGuard_NonMemberDenied(t *testing.T) {
	reg := NewRegistry()
	target := &Agent{Name: "eve", SystemPrompt: "I am Eve."}
	reg.Register(target)

	tool := newTestConsultTool(reg, &mockBackendConsult{response: "answer"})
	tool.WithSpaceContext("space-1", &mockSpaceChecker{members: []string{"bob"}})

	result := tool.Execute(context.Background(), map[string]any{
		"agent_name": "eve",
		"question":   "steal secrets?",
	})
	if !result.IsError {
		t.Fatal("expected error for non-member eve")
	}
	if !strings.Contains(result.Error, "not a member") {
		t.Errorf("expected 'not a member' in error, got: %s", result.Error)
	}
}

func TestConsultSpaceGuard_SpaceNotFound_DeniesAll(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&Agent{Name: "alice"})

	tool := newTestConsultTool(reg, &mockBackendConsult{})
	// (nil, nil) = space not found → deny-all
	tool.WithSpaceContext("nonexistent", &mockSpaceChecker{members: nil, err: nil})

	result := tool.Execute(context.Background(), map[string]any{
		"agent_name": "alice",
		"question":   "hello?",
	})
	if !result.IsError {
		t.Fatal("expected deny when space not found")
	}
}

func TestConsultSpaceGuard_CheckerError_Propagated(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&Agent{Name: "alice"})

	tool := newTestConsultTool(reg, &mockBackendConsult{})
	tool.WithSpaceContext("space-1", &mockSpaceChecker{err: errors.New("db down")})

	result := tool.Execute(context.Background(), map[string]any{
		"agent_name": "alice",
		"question":   "hello?",
	})
	if !result.IsError {
		t.Fatal("expected error when checker fails")
	}
	if !strings.Contains(result.Error, "space lookup failed") {
		t.Errorf("expected 'space lookup failed', got: %s", result.Error)
	}
}

func TestConsultSpaceGuard_NoSpaceID_GuardSkipped(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&Agent{Name: "alice", SystemPrompt: "I am Alice."})

	tool := newTestConsultTool(reg, &mockBackendConsult{response: "answer"})
	// No space context set — guard must be skipped entirely.

	result := tool.Execute(context.Background(), map[string]any{
		"agent_name": "alice",
		"question":   "hello?",
	})
	if result.IsError {
		t.Fatalf("expected success when no space context set, got: %s", result.Error)
	}
}

func TestConsultSpaceGuard_CaseInsensitive(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&Agent{Name: "Bob", SystemPrompt: "I am Bob."})

	tool := newTestConsultTool(reg, &mockBackendConsult{response: "answer"})
	// Members list uses "BOB" — matching must be case-insensitive.
	tool.WithSpaceContext("space-1", &mockSpaceChecker{members: []string{"BOB"}})

	result := tool.Execute(context.Background(), map[string]any{
		"agent_name": "Bob",
		"question":   "hello?",
	})
	if result.IsError {
		t.Fatalf("expected case-insensitive match to pass, got: %s", result.Error)
	}
}
