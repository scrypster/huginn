package agents

import (
	"errors"
	"testing"
)

func TestTryRegister_NewAgent_Succeeds(t *testing.T) {
	reg := NewRegistry()
	ag := &Agent{Name: "alice"}
	if err := reg.TryRegister(ag); err != nil {
		t.Fatalf("expected success for new agent, got: %v", err)
	}
	if _, ok := reg.ByName("alice"); !ok {
		t.Error("agent should be registered after TryRegister")
	}
}

func TestTryRegister_DuplicateName_ReturnsError(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&Agent{Name: "alice"})

	err := reg.TryRegister(&Agent{Name: "alice"})
	if err == nil {
		t.Fatal("expected ErrDuplicateAgentName, got nil")
	}
	if !errors.Is(err, ErrDuplicateAgentName) {
		t.Errorf("expected ErrDuplicateAgentName, got: %v", err)
	}
}

func TestTryRegister_DuplicateName_CaseInsensitive(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&Agent{Name: "Alice"})

	err := reg.TryRegister(&Agent{Name: "alice"})
	if err == nil {
		t.Fatal("expected ErrDuplicateAgentName for case-insensitive duplicate")
	}
	if !errors.Is(err, ErrDuplicateAgentName) {
		t.Errorf("expected ErrDuplicateAgentName, got: %v", err)
	}
}

func TestTryRegister_VaultCollision_LoggedAndCounted(t *testing.T) {
	before := VaultCollisionCount()
	reg := NewRegistry()
	reg.Register(&Agent{Name: "agent-a", VaultName: "vault-x"})

	// Different name, same vault — should register but increment collision counter.
	err := reg.TryRegister(&Agent{Name: "agent-b", VaultName: "vault-x"})
	if err != nil {
		t.Fatalf("TryRegister should succeed (name is different), got: %v", err)
	}
	if VaultCollisionCount() <= before {
		t.Error("vault collision count should have increased")
	}
}

func TestTryRegister_Register_NoInterference(t *testing.T) {
	reg := NewRegistry()
	// Register (original) should still overwrite freely.
	reg.Register(&Agent{Name: "alice"})
	reg.Register(&Agent{Name: "alice"}) // overwrite — no error expected

	// TryRegister should now fail.
	err := reg.TryRegister(&Agent{Name: "alice"})
	if err == nil {
		t.Fatal("TryRegister should fail after Register")
	}
}
