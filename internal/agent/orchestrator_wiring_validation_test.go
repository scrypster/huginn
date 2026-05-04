package agent

import (
	"context"
	"strings"
	"testing"
)

func TestOrchestratorValidateWiring_MissingRequiredDependencies(t *testing.T) {
	o := newBareOrchestrator()

	err := o.ValidateWiring()
	if err == nil {
		t.Fatal("expected wiring validation error")
	}
	msg := err.Error()
	for _, want := range []string{"backend", "context_builder"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected error to mention %q, got %q", want, msg)
		}
	}
}

func TestOrchestratorValidateWiring_DefaultSessionInvariant(t *testing.T) {
	o := newBareOrchestrator()
	o.backend = newMockBackend("ok")
	o.contextBuilder = NewContextBuilder(nil, nil, nil)
	o.defaultSessionID = "missing-session-id"

	err := o.ValidateWiring()
	if err == nil {
		t.Fatal("expected default session invariant error")
	}
	if !strings.Contains(err.Error(), "default_session") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChat_ReturnsWiringErrorWhenInvalid(t *testing.T) {
	o := newBareOrchestrator()
	err := o.Chat(context.Background(), "hello", nil, nil)
	if err == nil {
		t.Fatal("expected chat to fail when orchestrator is not wired")
	}
	if !strings.Contains(err.Error(), "orchestrator wiring invalid") {
		t.Fatalf("unexpected error: %v", err)
	}
}
