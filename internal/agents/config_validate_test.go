package agents_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

func TestAgentDef_Validate_EmptyName(t *testing.T) {
	d := agents.AgentDef{Name: ""}
	if err := d.Validate(); err == nil {
		t.Error("expected error for empty name")
	}
}

func TestAgentDef_Validate_ValidName(t *testing.T) {
	d := agents.AgentDef{Name: "Chris"}
	if err := d.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAgentDef_Validate_NameTooLong(t *testing.T) {
	d := agents.AgentDef{Name: string(make([]byte, 129))}
	if err := d.Validate(); err == nil {
		t.Error("expected error for name > 128 chars")
	}
}

func TestAgentDef_Validate_InvalidColor(t *testing.T) {
	d := agents.AgentDef{Name: "Chris", Color: "notacolor"}
	if err := d.Validate(); err == nil {
		t.Error("expected error for invalid color")
	}
}

func TestAgentDef_Validate_ValidColor(t *testing.T) {
	d := agents.AgentDef{Name: "Chris", Color: "#58a6ff"}
	if err := d.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAgentDef_Validate_EmptyColorAllowed(t *testing.T) {
	d := agents.AgentDef{Name: "Chris", Color: ""}
	if err := d.Validate(); err != nil {
		t.Errorf("empty color should be allowed: %v", err)
	}
}

func TestAgentDef_Validate_BasicValid(t *testing.T) {
	d := agents.AgentDef{Name: "Chris"}
	if err := d.Validate(); err != nil {
		t.Errorf("expected no error for valid agent: %v", err)
	}
}

func TestAgentDef_Validate_InvalidPlasticity(t *testing.T) {
	d := agents.AgentDef{Name: "Chris", Plasticity: "turbo"}
	if err := d.Validate(); err == nil {
		t.Error("expected error for invalid plasticity")
	}
}

func TestAgentDef_Validate_ValidPlasticity(t *testing.T) {
	for _, p := range []string{"", "default", "knowledge-graph", "reference"} {
		d := agents.AgentDef{Name: "Chris", Plasticity: p}
		if err := d.Validate(); err != nil {
			t.Errorf("plasticity %q should be valid: %v", p, err)
		}
	}
}

func TestAgentDef_Validate_InvalidMemoryMode(t *testing.T) {
	d := agents.AgentDef{Name: "Chris", MemoryMode: "aggressive"}
	if err := d.Validate(); err == nil {
		t.Error("expected error for invalid memory_mode")
	}
}

func TestAgentDef_Validate_ValidMemoryMode(t *testing.T) {
	for _, m := range []string{"", "passive", "conversational", "immersive"} {
		d := agents.AgentDef{Name: "Chris", MemoryMode: m}
		if err := d.Validate(); err != nil {
			t.Errorf("memory_mode %q should be valid: %v", m, err)
		}
	}
}

func TestCheckVaultNameCollision_NoCollision(t *testing.T) {
	existing := []agents.AgentDef{
		{Name: "Alpha"},
		{Name: "Beta"},
	}
	incoming := agents.AgentDef{Name: "Gamma"}
	if err := agents.CheckVaultNameCollision(incoming, "", "", existing); err != nil {
		t.Errorf("expected no collision: %v", err)
	}
}

func TestCheckVaultNameCollision_DetectsExplicitVaultCollision(t *testing.T) {
	// Alpha has explicit vault_name "shared-vault".
	// Beta is also being updated to use "shared-vault" → collision.
	existing := []agents.AgentDef{
		{Name: "Alpha", VaultName: "shared-vault"},
	}
	incoming := agents.AgentDef{Name: "Beta", VaultName: "shared-vault"}
	if err := agents.CheckVaultNameCollision(incoming, "Beta", "", existing); err == nil {
		t.Error("expected vault name collision: both agents use vault_name 'shared-vault'")
	}
}

func TestCheckVaultNameCollision_SkipsSelf(t *testing.T) {
	existing := []agents.AgentDef{
		{Name: "Chris"},
	}
	// During an update, the agent being updated should be excluded from the check.
	// Pass "Chris" as excludeName so Chris does not collide with itself.
	incoming := agents.AgentDef{Name: "Chris"}
	if err := agents.CheckVaultNameCollision(incoming, "Chris", "", existing); err != nil {
		t.Errorf("expected no collision when agent is same name: %v", err)
	}
}
