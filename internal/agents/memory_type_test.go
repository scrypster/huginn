package agents_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

func boolPtr(b bool) *bool { return &b }

func TestApplyMemoryType_None(t *testing.T) {
	d := agents.AgentDef{MemoryType: "none"}
	if err := d.ApplyMemoryType(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.MemoryEnabled == nil || *d.MemoryEnabled != false {
		t.Error("MemoryEnabled should be false for 'none'")
	}
	if d.ContextNotesEnabled {
		t.Error("ContextNotesEnabled should be false for 'none'")
	}
	if d.MemoryType != "" {
		t.Errorf("MemoryType should be cleared after apply, got %q", d.MemoryType)
	}
}

func TestApplyMemoryType_Context(t *testing.T) {
	d := agents.AgentDef{MemoryType: "context"}
	if err := d.ApplyMemoryType(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.MemoryEnabled == nil || *d.MemoryEnabled != false {
		t.Error("MemoryEnabled should be false for 'context'")
	}
	if !d.ContextNotesEnabled {
		t.Error("ContextNotesEnabled should be true for 'context'")
	}
	if d.MemoryType != "" {
		t.Errorf("MemoryType should be cleared, got %q", d.MemoryType)
	}
}

func TestApplyMemoryType_Muninndb(t *testing.T) {
	d := agents.AgentDef{MemoryType: "muninndb"}
	if err := d.ApplyMemoryType(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.MemoryEnabled == nil || *d.MemoryEnabled != true {
		t.Error("MemoryEnabled should be true for 'muninndb'")
	}
	if d.ContextNotesEnabled {
		t.Error("ContextNotesEnabled should be false for 'muninndb'")
	}
	if d.MemoryType != "" {
		t.Errorf("MemoryType should be cleared, got %q", d.MemoryType)
	}
}

func TestApplyMemoryType_EmptyString_NoOp(t *testing.T) {
	enabled := true
	d := agents.AgentDef{MemoryEnabled: &enabled, ContextNotesEnabled: true, MemoryType: ""}
	if err := d.ApplyMemoryType(); err != nil {
		t.Fatalf("unexpected error for empty MemoryType: %v", err)
	}
	// Canonical fields unchanged.
	if d.MemoryEnabled == nil || *d.MemoryEnabled != true {
		t.Error("MemoryEnabled should be unchanged")
	}
	if !d.ContextNotesEnabled {
		t.Error("ContextNotesEnabled should be unchanged")
	}
}

func TestApplyMemoryType_Unknown_ReturnsError(t *testing.T) {
	d := agents.AgentDef{MemoryType: "invalid_value"}
	if err := d.ApplyMemoryType(); err == nil {
		t.Fatal("expected error for unknown memory_type, got nil")
	}
}

func TestApplyMemoryType_CaseSensitive(t *testing.T) {
	// "MuninnDB" (wrong case) should fail, not silently pass.
	d := agents.AgentDef{MemoryType: "MuninnDB"}
	if err := d.ApplyMemoryType(); err == nil {
		t.Fatal("expected error for wrong-case memory_type")
	}
}

// --- DeriveMemoryType tests ---

func TestDeriveMemoryType_NilEnabled_ReturnsNone(t *testing.T) {
	d := agents.AgentDef{MemoryEnabled: nil, ContextNotesEnabled: false}
	d.DeriveMemoryType()
	if d.MemoryType != "none" {
		t.Errorf("nil MemoryEnabled should derive to 'none', got %q", d.MemoryType)
	}
}

func TestDeriveMemoryType_ExplicitFalse_ReturnsNone(t *testing.T) {
	f := false
	d := agents.AgentDef{MemoryEnabled: &f, ContextNotesEnabled: false}
	d.DeriveMemoryType()
	if d.MemoryType != "none" {
		t.Errorf("false MemoryEnabled should derive to 'none', got %q", d.MemoryType)
	}
}

func TestDeriveMemoryType_TrueEnabled_ReturnsMuninndb(t *testing.T) {
	tr := true
	d := agents.AgentDef{MemoryEnabled: &tr, ContextNotesEnabled: false}
	d.DeriveMemoryType()
	if d.MemoryType != "muninndb" {
		t.Errorf("true MemoryEnabled should derive to 'muninndb', got %q", d.MemoryType)
	}
}

func TestDeriveMemoryType_ContextNotesEnabled_ReturnsContext(t *testing.T) {
	// ContextNotesEnabled takes priority.
	f := false
	d := agents.AgentDef{MemoryEnabled: &f, ContextNotesEnabled: true}
	d.DeriveMemoryType()
	if d.MemoryType != "context" {
		t.Errorf("ContextNotesEnabled=true should derive to 'context', got %q", d.MemoryType)
	}
}

func TestDeriveMemoryType_ContextNotes_TakesPriorityOverMuninndb(t *testing.T) {
	// If both ContextNotesEnabled and MemoryEnabled are true, 'context' wins (checked first).
	tr := true
	d := agents.AgentDef{MemoryEnabled: &tr, ContextNotesEnabled: true}
	d.DeriveMemoryType()
	if d.MemoryType != "context" {
		t.Errorf("ContextNotesEnabled=true should win, got %q", d.MemoryType)
	}
}

// Round-trip: Apply then Derive should be stable.
func TestApplyDeriveRoundTrip(t *testing.T) {
	cases := []string{"none", "context", "muninndb"}
	for _, tc := range cases {
		d := agents.AgentDef{MemoryType: tc}
		if err := d.ApplyMemoryType(); err != nil {
			t.Fatalf("%s: ApplyMemoryType error: %v", tc, err)
		}
		d.DeriveMemoryType()
		if d.MemoryType != tc {
			t.Errorf("%s: round-trip failed, got %q", tc, d.MemoryType)
		}
	}
}
