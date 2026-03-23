package agents_test

import (
	"context"
	"errors"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
)

// --- mock backend for fallback tests ---

type mockFallbackBackend struct {
	response string
	err      error
}

func (m *mockFallbackBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	if req.OnToken != nil {
		req.OnToken(m.response)
	}
	return &backend.ChatResponse{Content: m.response, DoneReason: "stop"}, nil
}

func (m *mockFallbackBackend) Health(_ context.Context) error   { return nil }
func (m *mockFallbackBackend) Shutdown(_ context.Context) error { return nil }
func (m *mockFallbackBackend) ContextWindow() int               { return 128_000 }

// buildTestRegistryWithSteve returns a registry containing a single agent named "Steve".
func buildTestRegistryWithSteve() *agents.AgentRegistry {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Steve", ModelID: "m2"})
	return reg
}

func makeTestRegistry() *agents.AgentRegistry {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Chris", ModelID: "m1"})
	reg.Register(&agents.Agent{Name: "Steve", ModelID: "m2"})
	reg.Register(&agents.Agent{Name: "Mark", ModelID: "m3"})
	return reg
}

func TestParseDirective_HaveChrisPlan(t *testing.T) {
	reg := makeTestRegistry()
	d := agents.ParseDirective("Have Chris plan the auth module refactor", reg)
	if d == nil {
		t.Fatal("expected directive, got nil")
	}
	if len(d.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(d.Steps))
	}
	if d.Steps[0].AgentName != "Chris" {
		t.Errorf("expected Chris, got %s", d.Steps[0].AgentName)
	}
	if d.Steps[0].Action != "plan" {
		t.Errorf("expected plan, got %s", d.Steps[0].Action)
	}
	if d.Steps[0].Payload == "" {
		t.Error("expected non-empty payload")
	}
}

func TestParseDirective_AskMarkToReview(t *testing.T) {
	reg := makeTestRegistry()
	d := agents.ParseDirective("Ask Mark to review the edge cases", reg)
	if d == nil {
		t.Fatal("expected directive")
	}
	if d.Steps[0].AgentName != "Mark" {
		t.Errorf("expected Mark, got %s", d.Steps[0].AgentName)
	}
	if d.Steps[0].Action != "reason" {
		t.Errorf("expected reason, got %s", d.Steps[0].Action)
	}
}

func TestParseDirective_TellSteveToImplement(t *testing.T) {
	reg := makeTestRegistry()
	d := agents.ParseDirective("Tell Steve to implement the payment module", reg)
	if d == nil {
		t.Fatal("expected directive")
	}
	if d.Steps[0].AgentName != "Steve" {
		t.Errorf("expected Steve, got %s", d.Steps[0].AgentName)
	}
	if d.Steps[0].Action != "code" {
		t.Errorf("expected code, got %s", d.Steps[0].Action)
	}
}

func TestParseDirective_ChainedPlanThenImplement(t *testing.T) {
	reg := makeTestRegistry()
	d := agents.ParseDirective("Have Chris plan this then have Steve implement it", reg)
	if d == nil {
		t.Fatal("expected directive")
	}
	if len(d.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(d.Steps))
	}
	if d.Steps[0].AgentName != "Chris" || d.Steps[0].Action != "plan" {
		t.Errorf("step 0: got %+v", d.Steps[0])
	}
	if d.Steps[1].AgentName != "Steve" || d.Steps[1].Action != "code" {
		t.Errorf("step 1: got %+v", d.Steps[1])
	}
}

func TestParseDirective_UnknownAgent_ReturnsNil(t *testing.T) {
	reg := makeTestRegistry()
	d := agents.ParseDirective("Have Nobody plan this", reg)
	if d != nil {
		t.Error("expected nil for unknown agent")
	}
}

func TestParseDirective_NormalMessage_ReturnsNil(t *testing.T) {
	reg := makeTestRegistry()
	d := agents.ParseDirective("refactor the auth module", reg)
	if d != nil {
		t.Errorf("expected nil for normal message, got %+v", d)
	}
}

func TestParseDirective_CaseInsensitiveVerb(t *testing.T) {
	reg := makeTestRegistry()
	d := agents.ParseDirective("HAVE Chris plan this", reg)
	if d == nil {
		t.Fatal("expected directive for uppercase HAVE")
	}
}

func TestParseDirective_ActionAliases(t *testing.T) {
	reg := makeTestRegistry()
	cases := []struct {
		input  string
		action string
	}{
		{"Have Steve code the handler", "code"},
		{"Have Steve implement the handler", "code"},
		{"Have Steve refactor the handler", "code"},
		{"Have Mark review the changes", "reason"},
		{"Have Mark analyze the changes", "reason"},
		{"Have Mark check the changes", "reason"},
		{"Have Chris plan the approach", "plan"},
	}
	for _, c := range cases {
		d := agents.ParseDirective(c.input, reg)
		if d == nil {
			t.Errorf("input %q: expected directive, got nil", c.input)
			continue
		}
		if d.Steps[0].Action != c.action {
			t.Errorf("input %q: expected action %q, got %q", c.input, c.action, d.Steps[0].Action)
		}
	}
}

func TestContainsAgentName(t *testing.T) {
	reg := makeTestRegistry()
	if !agents.ContainsAgentName("check with Mark about this", reg) {
		t.Error("expected true for 'Mark'")
	}
	if agents.ContainsAgentName("refactor the module", reg) {
		t.Error("expected false for no agent names")
	}
}

func TestParseDirective_EmptyInput(t *testing.T) {
	reg := makeTestRegistry()
	if agents.ParseDirective("", reg) != nil {
		t.Error("expected nil for empty input")
	}
	if agents.ParseDirective("   ", reg) != nil {
		t.Error("expected nil for whitespace-only input")
	}
}

// TestParseDirectiveFallback_ValidJSON verifies the fallback parses a valid JSON response.
func TestParseDirectiveFallback_ValidJSON(t *testing.T) {
	reg := buildTestRegistryWithSteve()
	mb := &mockFallbackBackend{
		response: `{"agent":"Steve","action":"code","payload":"implement the payment module"}`,
	}
	d := agents.ParseDirectiveFallback(context.Background(), "yo Steve code the payment module", reg, mb, "cheap-model")
	if d == nil {
		t.Fatal("expected non-nil directive from valid JSON response")
	}
	if len(d.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(d.Steps))
	}
	if d.Steps[0].AgentName != "Steve" {
		t.Errorf("expected AgentName Steve, got %q", d.Steps[0].AgentName)
	}
	if d.Steps[0].Action != "code" {
		t.Errorf("expected action code, got %q", d.Steps[0].Action)
	}
	if d.Steps[0].Payload != "implement the payment module" {
		t.Errorf("unexpected payload: %q", d.Steps[0].Payload)
	}
}

// TestParseDirectiveFallback_MalformedJSON verifies that a non-JSON LLM response returns nil.
func TestParseDirectiveFallback_MalformedJSON(t *testing.T) {
	reg := buildTestRegistryWithSteve()
	mb := &mockFallbackBackend{
		response: "sure thing! I'll send Steve over right away.",
	}
	d := agents.ParseDirectiveFallback(context.Background(), "yo Steve code the payment module", reg, mb, "cheap-model")
	if d != nil {
		t.Errorf("expected nil for malformed JSON, got %+v", d)
	}
}

// TestParseDirectiveFallback_UnknownAgent verifies that JSON referencing an unregistered agent returns nil.
func TestParseDirectiveFallback_UnknownAgent(t *testing.T) {
	reg := buildTestRegistryWithSteve()
	mb := &mockFallbackBackend{
		response: `{"agent":"Nobody","action":"code","payload":"something"}`,
	}
	d := agents.ParseDirectiveFallback(context.Background(), "nobody should do this", reg, mb, "cheap-model")
	if d != nil {
		t.Errorf("expected nil for unregistered agent, got %+v", d)
	}
}

// TestParseDirectiveFallback_NilBackend verifies that a nil backend returns nil.
func TestParseDirectiveFallback_NilBackend(t *testing.T) {
	reg := buildTestRegistryWithSteve()
	d := agents.ParseDirectiveFallback(context.Background(), "yo Steve code this", reg, nil, "cheap-model")
	if d != nil {
		t.Errorf("expected nil for nil backend, got %+v", d)
	}
}

// TestParseDirectiveFallback_BackendError verifies that a backend error returns nil.
func TestParseDirectiveFallback_BackendError(t *testing.T) {
	reg := buildTestRegistryWithSteve()
	mb := &mockFallbackBackend{err: errors.New("connection refused")}
	d := agents.ParseDirectiveFallback(context.Background(), "yo Steve code this", reg, mb, "cheap-model")
	if d != nil {
		t.Errorf("expected nil for backend error, got %+v", d)
	}
}

// TestParseDirectiveFallback_CodeFencedJSON verifies that JSON wrapped in
// markdown code fences (commonly emitted by local models) is still parsed correctly.
func TestParseDirectiveFallback_CodeFencedJSON(t *testing.T) {
	reg := buildTestRegistryWithSteve()
	b := &mockFallbackBackend{response: "```json\n{\"agent\":\"Steve\",\"action\":\"code\",\"payload\":\"refactor auth\"}\n```"}
	got := agents.ParseDirectiveFallback(context.Background(), "Steve refactor auth", reg, b, "test-model")
	if got == nil {
		t.Fatal("expected non-nil ChainedDirective for code-fenced JSON response")
	}
	if len(got.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(got.Steps))
	}
	step := got.Steps[0]
	if step.AgentName != "Steve" {
		t.Errorf("expected AgentName 'Steve', got %q", step.AgentName)
	}
	if step.Action != "code" {
		t.Errorf("expected Action 'code', got %q", step.Action)
	}
}

// TestParseDirectiveFallback_InvalidAction verifies that an unknown action in JSON returns nil.
func TestParseDirectiveFallback_InvalidAction(t *testing.T) {
	reg := buildTestRegistryWithSteve()
	mb := &mockFallbackBackend{
		response: `{"agent":"Steve","action":"fly","payload":"something"}`,
	}
	d := agents.ParseDirectiveFallback(context.Background(), "yo Steve fly somewhere", reg, mb, "cheap-model")
	if d != nil {
		t.Errorf("expected nil for invalid action, got %+v", d)
	}
}
