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
	d := agents.AgentDef{Name: "Chris", Model: "claude-sonnet-4-6"}
	if err := d.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAgentDef_Validate_EmptyModel(t *testing.T) {
	d := agents.AgentDef{Name: "Chris", Model: ""}
	if err := d.Validate(); err == nil {
		t.Error("expected error for empty model")
	}
}

func TestAgentDef_Validate_WhitespaceModel(t *testing.T) {
	d := agents.AgentDef{Name: "Chris", Model: "   "}
	if err := d.Validate(); err == nil {
		t.Error("expected error for whitespace-only model")
	}
}

func TestAgentDef_Validate_ModelSet(t *testing.T) {
	d := agents.AgentDef{Name: "Chris", Model: "claude-haiku-4-5-20251001"}
	if err := d.Validate(); err != nil {
		t.Errorf("expected no error when model is set: %v", err)
	}
}

func TestAgentDef_Validate_NameTooLong(t *testing.T) {
	d := agents.AgentDef{Name: string(make([]byte, 129))}
	if err := d.Validate(); err == nil {
		t.Error("expected error for name > 128 chars")
	}
}

func TestAgentDef_Validate_InvalidColor(t *testing.T) {
	d := agents.AgentDef{Name: "Chris", Model: "claude-sonnet-4-6", Color: "notacolor"}
	if err := d.Validate(); err == nil {
		t.Error("expected error for invalid color")
	}
}

func TestAgentDef_Validate_ValidColor(t *testing.T) {
	d := agents.AgentDef{Name: "Chris", Model: "claude-sonnet-4-6", Color: "#58a6ff"}
	if err := d.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAgentDef_Validate_EmptyColorAllowed(t *testing.T) {
	d := agents.AgentDef{Name: "Chris", Model: "claude-sonnet-4-6", Color: ""}
	if err := d.Validate(); err != nil {
		t.Errorf("empty color should be allowed: %v", err)
	}
}

func TestAgentDef_Validate_BasicValid(t *testing.T) {
	d := agents.AgentDef{Name: "Chris", Model: "claude-sonnet-4-6"}
	if err := d.Validate(); err != nil {
		t.Errorf("expected no error for valid agent: %v", err)
	}
}

func TestAgentDef_Validate_InvalidPlasticity(t *testing.T) {
	d := agents.AgentDef{Name: "Chris", Model: "claude-sonnet-4-6", Plasticity: "turbo"}
	if err := d.Validate(); err == nil {
		t.Error("expected error for invalid plasticity")
	}
}

func TestAgentDef_Validate_ValidPlasticity(t *testing.T) {
	for _, p := range []string{"", "default", "knowledge-graph", "reference"} {
		d := agents.AgentDef{Name: "Chris", Model: "claude-sonnet-4-6", Plasticity: p}
		if err := d.Validate(); err != nil {
			t.Errorf("plasticity %q should be valid: %v", p, err)
		}
	}
}

func TestAgentDef_Validate_InvalidMemoryMode(t *testing.T) {
	d := agents.AgentDef{Name: "Chris", Model: "claude-sonnet-4-6", MemoryMode: "aggressive"}
	if err := d.Validate(); err == nil {
		t.Error("expected error for invalid memory_mode")
	}
}

func TestAgentDef_Validate_ValidMemoryMode(t *testing.T) {
	for _, m := range []string{"", "passive", "conversational", "immersive"} {
		d := agents.AgentDef{Name: "Chris", Model: "claude-sonnet-4-6", MemoryMode: m}
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

// ────────────────────────────────────────────────────────────────────────────
// Hardening: Agent name character validation (review/agents-channels-dm-hardening)
// ────────────────────────────────────────────────────────────────────────────

func TestAgentDef_Validate_NameWithSlash(t *testing.T) {
	d := agents.AgentDef{Name: "Agent/Name", Model: "claude-sonnet-4-6"}
	if err := d.Validate(); err == nil {
		t.Error("expected error for agent name containing /")
	}
}

func TestAgentDef_Validate_NameWithBackslash(t *testing.T) {
	d := agents.AgentDef{Name: "Agent\\Name", Model: "claude-sonnet-4-6"}
	if err := d.Validate(); err == nil {
		t.Error("expected error for agent name containing \\")
	}
}

func TestAgentDef_Validate_NameWithColon(t *testing.T) {
	d := agents.AgentDef{Name: "Agent:Name", Model: "claude-sonnet-4-6"}
	if err := d.Validate(); err == nil {
		t.Error("expected error for agent name containing :")
	}
}

func TestAgentDef_Validate_NameWithAngleBrackets(t *testing.T) {
	testCases := []string{
		"Agent<Name>",
		"<Agent>",
		"Agent>Name",
		"Agent<Name",
	}
	for _, name := range testCases {
		d := agents.AgentDef{Name: name, Model: "claude-sonnet-4-6"}
		if err := d.Validate(); err == nil {
			t.Errorf("expected error for agent name %q containing angle brackets", name)
		}
	}
}

func TestAgentDef_Validate_NameWithControlCharacters(t *testing.T) {
	testCases := []struct {
		name string
		char rune
	}{
		{"Agent\x00Name", '\x00'}, // null byte
		{"Agent\nName", '\n'},     // newline
		{"Agent\tName", '\t'},     // tab
		{"Agent\rName", '\r'},     // carriage return
		{"Agent\x01Name", '\x01'}, // other control char
	}
	for _, tc := range testCases {
		d := agents.AgentDef{Name: tc.name, Model: "claude-sonnet-4-6"}
		if err := d.Validate(); err == nil {
			t.Errorf("expected error for agent name containing control character %q", tc.char)
		}
	}
}

func TestAgentDef_Validate_NameStartingWithSpecialChar(t *testing.T) {
	testCases := []string{
		"-Agent",
		"_Agent",
		".Agent",
		" Agent",
		"&Agent",
		"'Agent",
		"\"Agent",
	}
	for _, name := range testCases {
		d := agents.AgentDef{Name: name, Model: "claude-sonnet-4-6"}
		if err := d.Validate(); err == nil {
			t.Errorf("expected error for agent name %q starting with special character", name)
		}
	}
}

func TestAgentDef_Validate_ValidNames(t *testing.T) {
	validNames := []string{
		"Sam",
		"Agent-1",
		"test_bot",
		"My Agent",
		"bot.v2",
		"A",                    // single letter
		"0Agent",               // starting with digit
		"Agent-With-Hyphens",   // hyphens in middle
		"agent_with_underscores", // underscores
		"agent.with.dots",      // dots
		"Agent 123",            // spaces and digits
	}
	for _, name := range validNames {
		d := agents.AgentDef{Name: name, Model: "claude-sonnet-4-6"}
		if err := d.Validate(); err != nil {
			t.Errorf("expected valid agent name %q to pass, got error: %v", name, err)
		}
	}
}

func TestAgentDef_Validate_WhitespaceOnlyName(t *testing.T) {
	d := agents.AgentDef{Name: "   ", Model: "claude-sonnet-4-6"}
	if err := d.Validate(); err == nil {
		t.Error("expected error for whitespace-only name")
	}
}

func TestAgentDef_Validate_NameWithAmpersand(t *testing.T) {
	d := agents.AgentDef{Name: "Agent&Co", Model: "claude-sonnet-4-6"}
	if err := d.Validate(); err == nil {
		t.Error("expected error for agent name containing &")
	}
}

func TestAgentDef_Validate_NameWithQuotes(t *testing.T) {
	testCases := []string{
		`Agent"Name`,
		`Agent'Name`,
	}
	for _, name := range testCases {
		d := agents.AgentDef{Name: name, Model: "claude-sonnet-4-6"}
		if err := d.Validate(); err == nil {
			t.Errorf("expected error for agent name %q containing quotes", name)
		}
	}
}
