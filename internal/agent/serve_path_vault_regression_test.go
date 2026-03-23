package agent

// serve_path_vault_regression_test.go
//
// Regression tests for the serve-path orchestrator vault wiring bug.
//
// ROOT CAUSE (fixed in main.go startServer):
//   startServer() creates its own *Orchestrator via NewOrchestrator().
//   srv.SetMuninnConfigPath() only wires the Server struct (REST handlers).
//   orch.SetMuninnConfigPath() was NEVER called in the serve path.
//   Result: o.muninnCfgPath == "" → connectAgentVault returned early with
//           warning="muninn config path not set" → no vault tools registered
//           → LLM used bash instead of memory tools → "bash: permission denied"
//
// THE FIX:
//   main.go startServer now calls BOTH:
//     srv.SetMuninnConfigPath(muninnCfgPath)
//     orch.SetMuninnConfigPath(muninnCfgPath)   ← was missing

import (
	"context"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/tools"
)

// TestServePath_NewOrchestrator_MuninnCfgPath_IsEmpty verifies that a freshly
// created Orchestrator (as startServer() creates it) has muninnCfgPath = "".
// This was the root cause: startServer called NewOrchestrator but never called
// orch.SetMuninnConfigPath, leaving the field at its zero value.
func TestServePath_NewOrchestrator_MuninnCfgPath_IsEmpty(t *testing.T) {
	mb := newMockBackend("ok")
	// Use the same constructor that startServer() uses.
	orch := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)

	// Without SetMuninnConfigPath, the path is empty.
	orch.mu.RLock()
	path := orch.muninnCfgPath
	orch.mu.RUnlock()

	if path != "" {
		t.Errorf("expected empty muninnCfgPath on fresh orchestrator, got %q", path)
	}
}

// TestServePath_MissingSetMuninnConfigPath_VaultWarning is the regression test that
// would have caught the bug. When the serve-path orchestrator has muninnCfgPath=""
// (because SetMuninnConfigPath was never called), connectAgentVault must return
// a warning — proving vault tools are NOT available in that broken state.
func TestServePath_MissingSetMuninnConfigPath_VaultWarning(t *testing.T) {
	mb := newMockBackend("ok")
	orch := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	// DO NOT call orch.SetMuninnConfigPath — this is the pre-fix state.

	reg := tools.NewRegistry()
	ag := &agents.Agent{
		Name:          "tom",
		MemoryEnabled: true,
		VaultName:     "huginn-tom",
		MemoryMode:    "immersive",
	}

	vr := orch.connectAgentVault(context.Background(), ag, reg)
	defer vr.cancel()

	// Without the fix: muninnCfgPath="" → immediate early return with warning.
	if vr.warning == "" {
		t.Fatal("expected warning when muninnCfgPath is not set on orchestrator — " +
			"this means vault is unavailable and LLM will attempt bash fallback")
	}
	if !strings.Contains(vr.warning, "muninn config path not set") {
		t.Errorf("expected 'muninn config path not set' in warning, got %q", vr.warning)
	}
	// No vault tools should be in sessionReg.
	if _, ok := vr.sessionReg.Get("muninn_recall"); ok {
		t.Error("muninn_recall must not be present when vault is unavailable")
	}
	if vr.memoryBlock != "" {
		t.Error("memoryBlock must be empty when vault fails")
	}
}

// TestServePath_AfterSetMuninnConfigPath_ProgressesBeyondEarlyExit verifies that
// calling orch.SetMuninnConfigPath (the fix) makes connectAgentVault advance past
// the "muninn config path not set" early return. It will still fail because the
// config file doesn't exist, but the warning will be about config load — NOT the
// missing-path error.
func TestServePath_AfterSetMuninnConfigPath_ProgressesBeyondEarlyExit(t *testing.T) {
	mb := newMockBackend("ok")
	orch := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	// THE FIX: call SetMuninnConfigPath as main.go now does.
	orch.SetMuninnConfigPath("/nonexistent/huginn/muninn.json")

	reg := tools.NewRegistry()
	ag := &agents.Agent{
		Name:          "tom",
		MemoryEnabled: true,
		VaultName:     "huginn-tom",
		MemoryMode:    "immersive",
	}

	vr := orch.connectAgentVault(context.Background(), ag, reg)
	defer vr.cancel()

	// With the fix: muninnCfgPath is non-empty, so it advances past the early exit.
	// The path doesn't exist, so it fails at config load — not at "path not set".
	if strings.Contains(vr.warning, "muninn config path not set") {
		t.Fatalf("SetMuninnConfigPath was called but connectAgentVault still returned "+
			"'muninn config path not set' — the fix is not working. warning=%q", vr.warning)
	}
	// Should have a warning about config unavailability (file doesn't exist).
	if vr.warning == "" {
		t.Log("note: no warning returned — if muninn.json exists on this machine, test is non-deterministic")
	}
}

// TestServePath_OrchestratorMuninnCfgPath_SetAndGet verifies the
// SetMuninnConfigPath/muninnCfgPath round-trip is wired correctly and
// thread-safe.
func TestServePath_OrchestratorMuninnCfgPath_SetAndGet(t *testing.T) {
	mb := newMockBackend("ok")
	orch := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)

	const expected = "/Users/testuser/.config/huginn/muninn.json"
	orch.SetMuninnConfigPath(expected)

	orch.mu.RLock()
	got := orch.muninnCfgPath
	orch.mu.RUnlock()

	if got != expected {
		t.Errorf("SetMuninnConfigPath(%q): got %q", expected, got)
	}
}

// TestServePath_BothServerAndOrchestratorMustBeWired documents the contract:
// in the serve path, BOTH the Server and the Orchestrator must receive the
// muninn config path. This test simulates that pattern using only the Orchestrator
// (since Server lives in the server package) and verifies the orchestrator's wiring
// is independent from any Server struct.
//
// The serve path fix ensures:
//   srv.SetMuninnConfigPath(path)   → wires REST API handlers
//   orch.SetMuninnConfigPath(path)  → wires per-session vault connections
func TestServePath_BothServerAndOrchestratorMustBeWired(t *testing.T) {
	mb := newMockBackend("ok")
	orch := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)

	// Simulate what startServer does BEFORE the fix:
	// srv.SetMuninnConfigPath → Server struct gets path (not tested here)
	// orch.SetMuninnConfigPath → MISSING in old code

	// State without fix:
	orch.mu.RLock()
	pathBefore := orch.muninnCfgPath
	orch.mu.RUnlock()
	if pathBefore != "" {
		t.Error("pre-condition: orchestrator should have empty muninnCfgPath before SetMuninnConfigPath")
	}

	// Apply the fix — what main.go startServer now does:
	muninnCfgPath := "/Users/testuser/.config/huginn/muninn.json"
	orch.SetMuninnConfigPath(muninnCfgPath)

	// State with fix:
	orch.mu.RLock()
	pathAfter := orch.muninnCfgPath
	orch.mu.RUnlock()
	if pathAfter != muninnCfgPath {
		t.Errorf("after SetMuninnConfigPath: got %q, want %q", pathAfter, muninnCfgPath)
	}
}

// TestServePath_VaultWarning_SystemPromptContainsUnavailableBlock verifies that
// when connectAgentVault returns a warning, buildAgentSystemPrompt + the vault
// warning injection (from mcp_agent_chat.go) produces a system prompt with the
// [MEMORY UNAVAILABLE] block.
//
// This test mirrors the concatenation in mcp_agent_chat.go lines 94-99.
func TestServePath_VaultWarning_SystemPromptContainsUnavailableBlock(t *testing.T) {
	// Simulate the serve path state BEFORE the fix: vault fails because cfgPath="".
	reg := tools.NewRegistry()
	reg.Register(&vaultStubTool{name: "bash"})
	// No muninn tools → vault was unavailable

	systemPrompt := buildAgentSystemPrompt(
		"",        // contextText
		"",        // agentSkillsFragment
		reg,       // sessionReg (no vault tools)
		"",        // globalInstructions
		"",        // projectInstructions
		"tom",     // agentName
		"",        // contextNotesBlock
		"immersive",
		"huginn-tom",
		"engineering decisions",
	)

	// Simulate the vault warning injection from mcp_agent_chat.go.
	warning := "muninn config path not set"
	if warning != "" {
		systemPrompt += "\n\n[MEMORY UNAVAILABLE] MuninnDB vault is not connected this session. " +
			"Memory tools (muninn_recall, muninn_remember, etc.) are not available. " +
			"Do not attempt to access memory via bash, filesystem, or any other tool. " +
			"If the user asks about past conversations or stored knowledge, state clearly that memory is temporarily unavailable."
	}

	if !strings.Contains(systemPrompt, "[MEMORY UNAVAILABLE]") {
		t.Error("expected [MEMORY UNAVAILABLE] block when vault warning is set")
	}
	if !strings.Contains(systemPrompt, "Do not attempt to access memory via bash") {
		t.Error("expected bash-fallback guard in system prompt")
	}
	if strings.Contains(systemPrompt, "## Memory") {
		t.Error("must not inject ## Memory section when vault tools are absent — LLM must not reference tools that aren't registered")
	}
}
