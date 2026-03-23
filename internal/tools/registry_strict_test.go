package tools

import (
	"strings"
	"testing"
)

// TestRegistry_StrictRegister_NewTool_OK verifies that registering a brand-new
// tool via StrictRegister succeeds with no error.
func TestRegistry_StrictRegister_NewTool_OK(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockTool{name: "x"})

	if err := reg.StrictRegister(&mockTool{name: "y"}); err != nil {
		t.Errorf("StrictRegister of new tool should succeed; got error: %v", err)
	}
}

// TestRegistry_StrictRegister_Collision_ReturnsError verifies that registering
// the same tool name twice returns an error containing "already registered".
func TestRegistry_StrictRegister_Collision_ReturnsError(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&mockTool{name: "x"})

	err := reg.StrictRegister(&mockTool{name: "x"})
	if err == nil {
		t.Fatal("expected collision error, got nil")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Errorf("error should contain 'already registered': %v", err)
	}
}

// TestRegistry_Register_CollisionStillOverwrites verifies that the original
// Register method still silently overwrites on collision (existing behaviour).
func TestRegistry_Register_CollisionStillOverwrites(t *testing.T) {
	reg := NewRegistry()
	first := &mockTool{name: "x"}
	second := &mockTool{name: "x"}
	reg.Register(first)
	reg.Register(second) // should not panic or error

	tool, ok := reg.Get("x")
	if !ok {
		t.Fatal("tool 'x' should be registered")
	}
	if tool != second {
		t.Error("second Register should have overwritten first")
	}
}
