package agents

import (
	"testing"
)

// TestWithModelOverride_ReturnsCopy verifies WithModelOverride doesn't
// mutate the source agent and that the returned copy carries the override.
// This is the core safety property — two concurrent workflow steps could
// otherwise stomp on each other's models.
func TestWithModelOverride_ReturnsCopy(t *testing.T) {
	t.Parallel()
	a := &Agent{
		Name:     "Alice",
		ModelID:  "claude-haiku",
		Provider: "anthropic",
	}
	cp := a.WithModelOverride("claude-sonnet-4-6")
	if cp == a {
		t.Fatal("WithModelOverride must return a new instance")
	}
	if cp.ModelID != "claude-sonnet-4-6" {
		t.Errorf("override not applied: %q", cp.ModelID)
	}
	if a.ModelID != "claude-haiku" {
		t.Errorf("source mutated: %q", a.ModelID)
	}
	if cp.Name != a.Name {
		t.Errorf("name lost on override: %q", cp.Name)
	}
	if cp.Provider != "anthropic" {
		t.Errorf("provider lost on override: %q", cp.Provider)
	}
}

// TestWithModelOverride_EmptyIsNoOp verifies empty/whitespace strings are
// treated as "no override", returning the source unchanged. Critical for
// the runner — most steps don't override and shouldn't pay the copy cost.
func TestWithModelOverride_EmptyIsNoOp(t *testing.T) {
	t.Parallel()
	a := &Agent{Name: "Bob", ModelID: "claude-haiku"}
	if got := a.WithModelOverride(""); got != a {
		t.Error("empty override must return receiver")
	}
	if got := a.WithModelOverride("   "); got != a {
		t.Error("whitespace override must return receiver")
	}
}

// TestWithModelOverride_PreservesSkillsAndToolbelt verifies the override
// keeps the agent's skill list and toolbelt — without these the cloned
// agent loses its capabilities mid-workflow.
func TestWithModelOverride_PreservesSkillsAndToolbelt(t *testing.T) {
	t.Parallel()
	a := &Agent{
		Name:       "Cara",
		ModelID:    "haiku",
		Skills:     []string{"code-review", "docs"},
		Toolbelt:   []ToolbeltEntry{{Provider: "github", ConnectionID: "gh-1"}},
		LocalTools: []string{"read_file"},
	}
	cp := a.WithModelOverride("sonnet")
	if len(cp.Skills) != 2 || cp.Skills[0] != "code-review" {
		t.Errorf("skills lost: %#v", cp.Skills)
	}
	if len(cp.Toolbelt) != 1 || cp.Toolbelt[0].Provider != "github" {
		t.Errorf("toolbelt lost: %#v", cp.Toolbelt)
	}
	if len(cp.LocalTools) != 1 || cp.LocalTools[0] != "read_file" {
		t.Errorf("local tools lost: %#v", cp.LocalTools)
	}
}
