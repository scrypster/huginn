package agent

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/tools"
)

// TestVaultToSystemPrompt_VaultConnected_InjectsMemorySection verifies the
// end-to-end path: when connectAgentVault succeeds (vault tools present in
// the session fork), buildAgentSystemPrompt injects the ## Memory section
// and the parent registry is never mutated.
func TestVaultToSystemPrompt_VaultConnected_InjectsMemorySection(t *testing.T) {
	// Simulate a successful vault connection: fork parent, register muninn tools.
	parent := tools.NewRegistry()
	parent.Register(&vaultStubTool{name: "bash"})

	sessionReg := parent.Fork()
	sessionReg.Register(&vaultStubTool{name: "muninn_recall"})
	sessionReg.Register(&vaultStubTool{name: "muninn_remember"})

	ag := &agents.Agent{
		Name:             "test-agent",
		MemoryEnabled:    true,
		VaultName:        "test-vault",
		MemoryMode:       "conversational",
		VaultDescription: "Engineering decisions",
	}

	prompt := buildAgentSystemPrompt(
		"",          // contextText
		"",          // agentSkillsFragment
		sessionReg,  // session-forked registry with vault tools
		"",          // globalInstructions
		"",          // projectInstructions
		ag.Name,
		"",          // contextNotesBlock
		ag.MemoryMode,
		ag.VaultName,
		ag.VaultDescription,
	)

	if !strings.Contains(prompt, "## Memory") {
		t.Error("expected ## Memory section when muninn_recall is in sessionReg")
	}
	if !strings.Contains(prompt, "Engineering decisions") {
		t.Error("expected vault description injected into system prompt")
	}
	if !strings.Contains(prompt, "At the start of each conversation") {
		t.Error("expected conversational mode instructions in system prompt")
	}

	// Parent registry must not be mutated by vault tool registration.
	if _, ok := parent.Get("muninn_recall"); ok {
		t.Fatal("parent registry must not contain vault tools")
	}
	if _, ok := parent.Get("muninn_remember"); ok {
		t.Fatal("parent registry must not contain vault tools")
	}

	// Session fork must still inherit parent tools.
	if _, ok := sessionReg.Get("bash"); !ok {
		t.Fatal("session fork must inherit parent tools")
	}
}

// TestVaultToSystemPrompt_VaultFailed_NoMemorySection verifies graceful
// degradation: when the vault connection fails (no muninn tools in the fork),
// no ## Memory section is injected — the LLM is never told to use tools that
// aren't registered.
func TestVaultToSystemPrompt_VaultFailed_NoMemorySection(t *testing.T) {
	parent := tools.NewRegistry()
	parent.Register(&vaultStubTool{name: "bash"})

	// Fork with no vault tools — simulates a failed connectAgentVault.
	sessionReg := parent.Fork()

	prompt := buildAgentSystemPrompt(
		"", "", sessionReg, "", "", "test-agent", "",
		"conversational", "test-vault", "desc",
	)

	if strings.Contains(prompt, "## Memory") {
		t.Error("expected NO ## Memory section when vault tools are absent (degraded mode)")
	}
}

// TestVaultToSystemPrompt_PassiveMode_ReflectedInPrompt verifies that the
// memory mode is correctly threaded from the agent config into the system prompt.
func TestVaultToSystemPrompt_PassiveMode_ReflectedInPrompt(t *testing.T) {
	sessionReg := tools.NewRegistry()
	sessionReg.Register(&vaultStubTool{name: "muninn_recall"})

	prompt := buildAgentSystemPrompt(
		"", "", sessionReg, "", "", "agent", "",
		"passive", "vault", "",
	)

	if !strings.Contains(prompt, "explicitly asks you to remember") {
		t.Errorf("passive mode instructions not in prompt, got: %q", prompt)
	}
}

// TestVaultToSystemPrompt_ImmersiveMode_ReflectedInPrompt ensures immersive
// instructions (muninn_where_left_off) are mentioned when mode is immersive.
func TestVaultToSystemPrompt_ImmersiveMode_ReflectedInPrompt(t *testing.T) {
	sessionReg := tools.NewRegistry()
	sessionReg.Register(&vaultStubTool{name: "muninn_where_left_off"})

	prompt := buildAgentSystemPrompt(
		"", "", sessionReg, "", "", "agent", "",
		"immersive", "vault", "",
	)

	if !strings.Contains(prompt, "muninn_where_left_off") {
		t.Errorf("immersive mode instructions not in prompt, got: %q", prompt)
	}
}

// TestVaultToSystemPrompt_NilRegistry_NoPanic verifies that a nil registry
// (no tools at all) does not panic and produces no Memory section.
func TestVaultToSystemPrompt_NilRegistry_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("buildAgentSystemPrompt panicked with nil registry: %v", r)
		}
	}()

	prompt := buildAgentSystemPrompt(
		"", "", nil, "", "", "agent", "",
		"conversational", "vault", "desc",
	)

	if strings.Contains(prompt, "## Memory") {
		t.Error("expected no ## Memory section with nil registry")
	}
}
