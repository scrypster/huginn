package agent

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// newTestOrchestrator creates a minimal Orchestrator for vault tests.
func newTestOrchestrator() *Orchestrator {
	return &Orchestrator{
		toolRegistry: tools.NewRegistry(),
	}
}

func TestConnectAgentVault_NilAgent_ReturnsForkedRegistry(t *testing.T) {
	o := newTestOrchestrator()
	parent := tools.NewRegistry()
	parent.Register(&vaultStubTool{name: "parent_tool"})

	vr := o.connectAgentVault(context.Background(), nil, parent)
	defer vr.cancel()

	// Session registry is a valid fork (inherits parent tool).
	if _, ok := vr.sessionReg.Get("parent_tool"); !ok {
		t.Fatal("sessionReg should inherit parent tools even when agent is nil")
	}
	if vr.warning != "" {
		t.Errorf("nil agent should not produce warning, got %q", vr.warning)
	}
	if vr.memoryBlock != "" {
		t.Errorf("nil agent should not produce memoryBlock, got %q", vr.memoryBlock)
	}
}

func TestConnectAgentVault_MemoryDisabled_ReturnsForkedRegistry(t *testing.T) {
	o := newTestOrchestrator()
	parent := tools.NewRegistry()

	ag := &agents.Agent{MemoryEnabled: false, VaultName: "my-vault"}
	vr := o.connectAgentVault(context.Background(), ag, parent)
	defer vr.cancel()

	if vr.sessionReg == nil {
		t.Fatal("sessionReg must never be nil")
	}
	if vr.warning != "" {
		t.Errorf("disabled memory should not warn, got %q", vr.warning)
	}
	if vr.memoryBlock != "" {
		t.Errorf("disabled memory should not produce memoryBlock")
	}
}

func TestConnectAgentVault_NoVaultName_ReturnsForkedRegistry(t *testing.T) {
	o := newTestOrchestrator()
	parent := tools.NewRegistry()

	ag := &agents.Agent{MemoryEnabled: true, VaultName: ""}
	vr := o.connectAgentVault(context.Background(), ag, parent)
	defer vr.cancel()

	if vr.sessionReg == nil {
		t.Fatal("sessionReg must never be nil")
	}
	if vr.warning != "" {
		t.Errorf("no vault name should not warn")
	}
}

func TestConnectAgentVault_NoCfgPath_ProducesWarning(t *testing.T) {
	o := newTestOrchestrator()
	// muninnCfgPath is empty string (zero value) → vault not configured.
	parent := tools.NewRegistry()

	ag := &agents.Agent{MemoryEnabled: true, VaultName: "huginn:agent:default:myagent"}
	vr := o.connectAgentVault(context.Background(), ag, parent)
	defer vr.cancel()

	if vr.warning == "" {
		t.Error("expected warning when muninnCfgPath is empty")
	}
	if vr.memoryBlock != "" {
		t.Error("memoryBlock should be empty when vault connection fails")
	}
	if vr.sessionReg == nil {
		t.Fatal("sessionReg must never be nil even on failure")
	}
}

func TestConnectAgentVault_Fork_DoesNotMutateParent(t *testing.T) {
	o := newTestOrchestrator()
	parent := tools.NewRegistry()
	parent.Register(&vaultStubTool{name: "shared"})

	ag := &agents.Agent{MemoryEnabled: false, VaultName: ""}
	vr := o.connectAgentVault(context.Background(), ag, parent)
	defer vr.cancel()

	// Even when we add to the fork, parent is unchanged.
	vr.sessionReg.Register(&vaultStubTool{name: "session_only"})

	if _, ok := parent.Get("session_only"); ok {
		t.Fatal("adding to sessionReg must not affect parent registry")
	}
	if _, ok := vr.sessionReg.Get("shared"); !ok {
		t.Fatal("sessionReg should inherit parent tools")
	}
}

func TestConnectAgentVault_CancelIsAlwaysSafe(t *testing.T) {
	o := newTestOrchestrator()
	parent := tools.NewRegistry()

	cases := []*agents.Agent{
		nil,
		{MemoryEnabled: false, VaultName: "x"},
		{MemoryEnabled: true, VaultName: ""},
		{MemoryEnabled: true, VaultName: "vault"},
	}
	for _, ag := range cases {
		vr := o.connectAgentVault(context.Background(), ag, parent)
		// Calling cancel multiple times must not panic.
		vr.cancel()
		vr.cancel()
	}
}

func TestConnectAgentVault_ContextCancellation_Handled(t *testing.T) {
	o := newTestOrchestrator()
	o.muninnCfgPath = "/nonexistent/path/muninn.json"
	parent := tools.NewRegistry()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel context

	ag := &agents.Agent{MemoryEnabled: true, VaultName: "vault"}
	// Should not block or panic on pre-cancelled context.
	vr := o.connectAgentVault(ctx, ag, parent)
	defer vr.cancel()

	// Either produces a warning or succeeds gracefully — no panic.
	if vr.sessionReg == nil {
		t.Fatal("sessionReg must never be nil")
	}
}

// vaultStubTool is a minimal Tool for vault tests.
type vaultStubTool struct{ name string }

func (s *vaultStubTool) Name() string        { return s.name }
func (s *vaultStubTool) Description() string { return "vault-stub" }
func (s *vaultStubTool) Permission() tools.PermissionLevel {
	return tools.PermRead
}
func (s *vaultStubTool) Schema() backend.Tool {
	return backend.Tool{Type: "function", Function: backend.ToolFunction{Name: s.name}}
}
func (s *vaultStubTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	return tools.ToolResult{Output: "stub"}
}
