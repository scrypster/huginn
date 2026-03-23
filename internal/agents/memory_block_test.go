package agents_test

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

func TestBuildMemoryBlock_MemoryDisabled_ReturnsEmpty(t *testing.T) {
	ag := &agents.Agent{MemoryEnabled: false, VaultName: "my-vault"}
	if block := agents.BuildMemoryBlock(ag); block != "" {
		t.Errorf("expected empty block when MemoryEnabled=false, got %q", block)
	}
}

func TestBuildMemoryBlock_NoVaultName_ReturnsEmpty(t *testing.T) {
	ag := &agents.Agent{MemoryEnabled: true, VaultName: ""}
	if block := agents.BuildMemoryBlock(ag); block != "" {
		t.Errorf("expected empty block when VaultName empty, got %q", block)
	}
}

func TestBuildMemoryBlock_BothDisabled_ReturnsEmpty(t *testing.T) {
	ag := &agents.Agent{MemoryEnabled: false, VaultName: ""}
	if block := agents.BuildMemoryBlock(ag); block != "" {
		t.Errorf("expected empty when both disabled, got %q", block)
	}
}

func TestBuildMemoryBlock_PassiveMode(t *testing.T) {
	ag := &agents.Agent{MemoryEnabled: true, VaultName: "test-vault", MemoryMode: "passive"}
	block := agents.BuildMemoryBlock(ag)
	if block == "" {
		t.Fatal("expected non-empty block")
	}
	if !strings.Contains(block, "passive") {
		t.Errorf("block should mention passive mode, got: %q", block)
	}
	if !strings.Contains(block, "## Memory System") {
		t.Errorf("block should have Memory System header")
	}
}

func TestBuildMemoryBlock_ImmersiveMode(t *testing.T) {
	ag := &agents.Agent{MemoryEnabled: true, VaultName: "test-vault", MemoryMode: "immersive"}
	block := agents.BuildMemoryBlock(ag)
	if !strings.Contains(block, "immersive") {
		t.Errorf("block should mention immersive mode")
	}
}

func TestBuildMemoryBlock_DefaultMode_IsConversational(t *testing.T) {
	ag := &agents.Agent{MemoryEnabled: true, VaultName: "test-vault", MemoryMode: ""}
	block := agents.BuildMemoryBlock(ag)
	if block == "" {
		t.Fatal("expected non-empty block for empty mode")
	}
	if !strings.Contains(block, "conversational") {
		t.Errorf("empty mode should default to conversational, got: %q", block)
	}
}

func TestBuildMemoryBlock_ConversationalMode_Explicit(t *testing.T) {
	ag := &agents.Agent{MemoryEnabled: true, VaultName: "test-vault", MemoryMode: "conversational"}
	block := agents.BuildMemoryBlock(ag)
	if !strings.Contains(block, "conversational") {
		t.Errorf("explicit conversational mode not reflected in block")
	}
}

func TestBuildMemoryBlock_VaultDescription_Injected(t *testing.T) {
	ag := &agents.Agent{
		MemoryEnabled:    true,
		VaultName:        "test-vault",
		VaultDescription: "Stores engineering decisions",
	}
	block := agents.BuildMemoryBlock(ag)
	if !strings.Contains(block, "Stores engineering decisions") {
		t.Errorf("vault description not injected into block")
	}
	if !strings.Contains(block, "test-vault") {
		t.Errorf("vault name not injected into block")
	}
}

func TestBuildMemoryBlock_NoVaultDescription_NoDescriptionLine(t *testing.T) {
	ag := &agents.Agent{MemoryEnabled: true, VaultName: "test-vault", VaultDescription: ""}
	block := agents.BuildMemoryBlock(ag)
	// Should have header and mode but no "purpose:" line.
	if strings.Contains(block, "purpose:") {
		t.Errorf("should not inject description when empty")
	}
}

// Verify that BuildMemoryBlock only fires on successful vault setup.
// When vault is available (MemoryEnabled=true, VaultName set), block is non-empty.
// When vault is unavailable (MemoryEnabled=false OR VaultName=""), block is empty.
// This prevents the LLM from referencing memory tools that aren't registered.
func TestBuildMemoryBlock_NeverInjectsWhenVaultUnavailable(t *testing.T) {
	cases := []agents.Agent{
		{MemoryEnabled: false, VaultName: "x"},
		{MemoryEnabled: true, VaultName: ""},
		{MemoryEnabled: false, VaultName: ""},
	}
	for i := range cases {
		if block := agents.BuildMemoryBlock(&cases[i]); block != "" {
			t.Errorf("case %d: expected empty block for unavailable vault", i)
		}
	}
}
