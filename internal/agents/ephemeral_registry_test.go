package agents

import (
	"testing"

	"github.com/scrypster/huginn/internal/modelconfig"
)

// TestEphemeralOverlay_SurvivesReload verifies that a specialist registered
// into the ephemeral overlay stays resolvable via ByName across a
// ReloadFromConfig call, but never shows up in All() (roster invisibility
// is structural, not a display-time filter).
func TestEphemeralOverlay_SurvivesReload(t *testing.T) {
	reg := NewRegistry()
	cfg := &AgentsConfig{Agents: []AgentDef{
		{Name: "Winston", Model: "sonnet"},
	}}
	reg.ReloadFromConfig(cfg, "")

	specialist := &Agent{Name: "Rust Audit Specialist", ModelID: "sonnet"}
	if err := reg.RegisterEphemeral(specialist); err != nil {
		t.Fatalf("RegisterEphemeral: %v", err)
	}

	// resolvable pre-reload
	if _, ok := reg.ByName("Rust Audit Specialist"); !ok {
		t.Fatalf("expected specialist resolvable via ByName before reload")
	}
	// case-insensitive
	if _, ok := reg.ByName("rust audit specialist"); !ok {
		t.Fatalf("expected specialist resolvable case-insensitively")
	}

	// Reload mid-thread (e.g. a hire elsewhere triggers a full config reload).
	cfg2 := &AgentsConfig{Agents: []AgentDef{
		{Name: "Winston", Model: "sonnet"},
		{Name: "Newhire", Model: "sonnet"},
	}}
	reg.ReloadFromConfig(cfg2, "")

	got, ok := reg.ByName("Rust Audit Specialist")
	if !ok {
		t.Fatalf("expected specialist to survive ReloadFromConfig")
	}
	if got.Name != "Rust Audit Specialist" {
		t.Fatalf("unexpected agent returned: %+v", got)
	}

	// All() must never include the ephemeral specialist.
	for _, a := range reg.All() {
		if a.Name == "Rust Audit Specialist" {
			t.Fatalf("All() must never return ephemeral specialists, got %+v", a)
		}
	}
	if len(reg.All()) != 2 {
		t.Fatalf("expected All() to contain exactly the 2 seated agents, got %d", len(reg.All()))
	}

	// Names() must also stay clean (roster/mention-autocomplete surface).
	for _, n := range reg.Names() {
		if n == "rust audit specialist" {
			t.Fatalf("Names() must never include ephemeral specialists")
		}
	}
}

func TestEphemeralOverlay_CollisionGuard(t *testing.T) {
	reg := NewRegistry()
	cfg := &AgentsConfig{Agents: []AgentDef{
		{Name: "Winston", Model: "sonnet"},
	}}
	reg.ReloadFromConfig(cfg, "")

	// Cannot register an ephemeral agent that collides with a seated agent.
	if err := reg.RegisterEphemeral(&Agent{Name: "winston"}); err == nil {
		t.Fatalf("expected collision error registering ephemeral over seated agent name")
	}

	if err := reg.RegisterEphemeral(&Agent{Name: "Go Specialist"}); err != nil {
		t.Fatalf("RegisterEphemeral: %v", err)
	}
	// Cannot register a second ephemeral with the same name.
	if err := reg.RegisterEphemeral(&Agent{Name: "go specialist"}); err == nil {
		t.Fatalf("expected collision error for duplicate ephemeral registration")
	}
	// Cannot TryRegister (seat) an agent whose name collides with an active specialist.
	if err := reg.TryRegister(&Agent{Name: "Go Specialist"}); err == nil {
		t.Fatalf("expected collision error seating an agent over an active ephemeral specialist")
	}
}

func TestEphemeralOverlay_UnregisterAndIsEphemeral(t *testing.T) {
	reg := NewRegistry()
	specialist := &Agent{Name: "Data Specialist"}
	if err := reg.RegisterEphemeral(specialist); err != nil {
		t.Fatalf("RegisterEphemeral: %v", err)
	}
	if !reg.IsEphemeral("Data Specialist") {
		t.Fatalf("expected IsEphemeral true")
	}
	reg.UnregisterEphemeral("Data Specialist")
	if _, ok := reg.ByName("Data Specialist"); ok {
		t.Fatalf("expected specialist gone after UnregisterEphemeral")
	}
	if reg.IsEphemeral("Data Specialist") {
		t.Fatalf("expected IsEphemeral false after unregister")
	}
}

func TestEphemeralOverlay_BuildRegistryUnaffected(t *testing.T) {
	// Sanity: BuildRegistryWithUsername still works and returns a registry
	// with a usable (non-nil) ephemeral overlay.
	cfg := &AgentsConfig{Agents: []AgentDef{{Name: "Solo", Model: "sonnet"}}}
	reg := BuildRegistryWithUsername(cfg, &modelconfig.Models{}, "")
	if err := reg.RegisterEphemeral(&Agent{Name: "One Off"}); err != nil {
		t.Fatalf("RegisterEphemeral on built registry: %v", err)
	}
	if _, ok := reg.ByName("One Off"); !ok {
		t.Fatalf("expected resolvable")
	}
}
