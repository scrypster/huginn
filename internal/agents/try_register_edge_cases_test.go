package agents

import (
	"errors"
	"testing"
)

// TestTryRegister_NilAgent_ReturnsError verifies no panic on nil input.
func TestTryRegister_NilAgent_ReturnsError(t *testing.T) {
	r := NewRegistry()
	err := r.TryRegister(nil)
	if err == nil {
		t.Fatal("expected error for nil agent, got nil")
	}
}

// TestTryRegister_EmptyName_ReturnsError verifies that an empty agent name is rejected.
func TestTryRegister_EmptyName_ReturnsError(t *testing.T) {
	r := NewRegistry()
	err := r.TryRegister(&Agent{Name: ""})
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
}

// TestTryRegister_WhitespaceName_ReturnsError verifies that a whitespace-only
// name is rejected as effectively empty.
func TestTryRegister_WhitespaceName_ReturnsError(t *testing.T) {
	r := NewRegistry()
	err := r.TryRegister(&Agent{Name: "   "})
	if err == nil {
		t.Fatal("expected error for whitespace-only name, got nil")
	}
}

// TestTryRegister_DuplicateName_ReturnsErrDuplicateAgentName verifies the
// sentinel error is returned (not just any error).
func TestTryRegister_DuplicateName_ReturnsSentinel(t *testing.T) {
	r := NewRegistry()
	r.Register(&Agent{Name: "Alpha"})
	err := r.TryRegister(&Agent{Name: "alpha"})
	if !errors.Is(err, ErrDuplicateAgentName) {
		t.Fatalf("expected ErrDuplicateAgentName, got %v", err)
	}
}

// TestTryRegister_Success_AgentVisible verifies a successful registration makes
// the agent retrievable by name.
func TestTryRegister_Success_AgentVisible(t *testing.T) {
	r := NewRegistry()
	a := &Agent{Name: "Beta"}
	if err := r.TryRegister(a); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := r.ByName("Beta")
	if !ok || got != a {
		t.Errorf("agent not found after TryRegister")
	}
}

// TestResetVaultCollisionCount verifies the test helper zeroes the counter.
func TestResetVaultCollisionCount(t *testing.T) {
	resetVaultCollisionCount()
	if VaultCollisionCount() != 0 {
		t.Fatalf("expected 0 after reset, got %d", VaultCollisionCount())
	}
}
