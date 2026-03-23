package threadmgr

import (
	"context"
	"fmt"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
)

// stubBackend is a minimal Backend stub for testing AutoHelpResolver.
type stubBackend struct {
	resp *backend.ChatResponse
	err  error
}

func (s *stubBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	// Fire OnToken if provided, to exercise the streaming path.
	if req.OnToken != nil && s.resp != nil {
		req.OnToken(s.resp.Content)
	}
	return s.resp, nil
}

func (s *stubBackend) Health(_ context.Context) error        { return nil }
func (s *stubBackend) Shutdown(_ context.Context) error      { return nil }
func (s *stubBackend) ContextWindow() int                    { return 4096 }
func (s *stubBackend) Models(_ context.Context) ([]string, error) { return nil, nil }

// helpBroadcastRecorder records all broadcasts for assertion in AutoHelpResolver tests.
type helpBroadcastRecorder struct {
	events []helpBroadcastEvent
}

type helpBroadcastEvent struct {
	sessionID string
	msgType   string
	payload   map[string]any
}

func (c *helpBroadcastRecorder) fn() BroadcastFn {
	return func(sessionID, msgType string, payload map[string]any) {
		c.events = append(c.events, helpBroadcastEvent{sessionID, msgType, payload})
	}
}

func (c *helpBroadcastRecorder) types() []string {
	var out []string
	for _, e := range c.events {
		out = append(out, e.msgType)
	}
	return out
}

func TestAutoHelpResolver_Resolve_success(t *testing.T) {
	reg := agents.NewRegistry()
	mark := &agents.Agent{Name: "Mark", SystemPrompt: "You are Mark."}
	reg.Register(mark)

	bc := &helpBroadcastRecorder{}
	stub := &stubBackend{resp: &backend.ChatResponse{Content: "  Here is my answer  "}}

	r := &AutoHelpResolver{
		Backend:  stub,
		AgentReg: reg,
		Store:    nil,
		Broadcast: bc.fn(),
		PrimaryAgent: func(sessionID string) *agents.Agent {
			return mark
		},
	}

	answer, err := r.Resolve(context.Background(), "sess1", "thread1", "Steve", "I need help with X")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if answer != "Here is my answer" {
		t.Errorf("expected trimmed answer, got %q", answer)
	}

	types := bc.types()
	if len(types) < 2 {
		t.Fatalf("expected at least 2 broadcasts, got %d: %v", len(types), types)
	}
	if types[0] != "thread_help_resolving" {
		t.Errorf("first broadcast should be thread_help_resolving, got %q", types[0])
	}
	if types[len(types)-1] != "thread_help_resolved" {
		t.Errorf("last broadcast should be thread_help_resolved, got %q", types[len(types)-1])
	}
}

func TestAutoHelpResolver_Resolve_nilPrimaryAgent(t *testing.T) {
	reg := agents.NewRegistry()
	bc := &helpBroadcastRecorder{}
	stub := &stubBackend{resp: &backend.ChatResponse{Content: "answer"}}

	r := &AutoHelpResolver{
		Backend:  stub,
		AgentReg: reg,
		Store:    nil,
		Broadcast: bc.fn(),
		PrimaryAgent: func(sessionID string) *agents.Agent {
			return nil
		},
	}

	_, err := r.Resolve(context.Background(), "sess1", "thread1", "Steve", "help me")
	if err == nil {
		t.Fatal("expected error when PrimaryAgent returns nil, got nil")
	}
}

func TestAutoHelpResolver_Resolve_fallsBackOnLLMError(t *testing.T) {
	reg := agents.NewRegistry()
	mark := &agents.Agent{Name: "Mark", SystemPrompt: "You are Mark."}
	reg.Register(mark)

	bc := &helpBroadcastRecorder{}
	stub := &stubBackend{err: fmt.Errorf("LLM down")}

	r := &AutoHelpResolver{
		Backend:  stub,
		AgentReg: reg,
		Store:    nil,
		Broadcast: bc.fn(),
		PrimaryAgent: func(sessionID string) *agents.Agent {
			return mark
		},
	}

	_, err := r.Resolve(context.Background(), "sess1", "thread1", "Steve", "help me")
	if err == nil {
		t.Fatal("expected error from LLM, got nil")
	}

	// thread_help_resolved must NOT be broadcast when LLM fails
	for _, ev := range bc.events {
		if ev.msgType == "thread_help_resolved" {
			t.Error("thread_help_resolved must not be broadcast on LLM error")
		}
	}
}
