package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/stats"
)

// TestPrepareAgentRuntime_NoRegistries_ReturnsNil asserts that when the
// orchestrator has no tool/agent registry wired, PrepareAgentRuntime returns
// (nil, nil) — threadmgr falls back to its legacy global-toolset path.
func TestPrepareAgentRuntime_NoRegistries_ReturnsNil(t *testing.T) {
	mb := newMockBackend("")
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, stats.NoopCollector{}, nil)

	rt, err := o.PrepareAgentRuntime(context.Background(), "Worker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rt != nil {
		t.Fatalf("expected nil runtime when registries are unset; got %+v", rt)
	}
}

// TestPrepareAgentRuntime_AgentMissing_ReturnsNil asserts that when an agent
// is not in the registry, PrepareAgentRuntime returns (nil, nil) so the
// thread degrades to legacy fallback rather than failing.
func TestPrepareAgentRuntime_AgentMissing_ReturnsNil(t *testing.T) {
	mb := newMockBackend("")
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, stats.NoopCollector{}, nil)

	o.SetTools(newTestToolsReg(), nil)
	o.SetAgentRegistry(agents.NewRegistry())

	rt, err := o.PrepareAgentRuntime(context.Background(), "GhostAgent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rt != nil {
		t.Fatalf("expected nil runtime when agent missing; got %+v", rt)
	}
}

// TestPrepareAgentRuntime_PresentAgent_ReturnsToolbeltSchemasAndCleanup
// asserts that for a registered agent with a configured toolbelt provider,
// PrepareAgentRuntime returns:
//   - schemas including the toolbelt provider's tools
//   - a non-nil ExecuteTool (per-agent gate-wrapped executor)
//   - a non-nil Cleanup that runs without panicking
//
// MuninnDB is not enabled here (no muninn config path set), so ExtraSystem
// is empty by design — graceful degradation when the vault is unavailable.
func TestPrepareAgentRuntime_PresentAgent_ReturnsToolbeltSchemasAndCleanup(t *testing.T) {
	mb := newMockBackend("")
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, stats.NoopCollector{}, nil)

	toolReg := newTestToolsReg()
	o.SetTools(toolReg, nil)

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name: "Worker",
		Toolbelt: []agents.ToolbeltEntry{
			{ConnectionID: "conn-github", Provider: "github"},
		},
	})
	o.SetAgentRegistry(reg)

	rt, err := o.PrepareAgentRuntime(context.Background(), "Worker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rt == nil {
		t.Fatal("expected a runtime for a registered agent with a toolbelt")
	}
	defer rt.Cleanup()

	// Schemas must include the toolbelt provider's tool.
	foundGithub := false
	for _, s := range rt.Schemas {
		if s.Function.Name == "github_list_prs" {
			foundGithub = true
		}
	}
	if !foundGithub {
		names := make([]string, 0, len(rt.Schemas))
		for _, s := range rt.Schemas {
			names = append(names, s.Function.Name)
		}
		t.Fatalf("expected github_list_prs in runtime schemas; got %v", names)
	}

	// ExecuteTool must be wired and dispatch a known-bad tool name into the
	// session-local registry (which returns "unknown tool: ...").
	if rt.ExecuteTool == nil {
		t.Fatal("expected ExecuteTool to be non-nil")
	}
	_, execErr := rt.ExecuteTool(context.Background(), "definitely_not_a_real_tool", nil)
	if execErr == nil || !strings.Contains(execErr.Error(), "unknown tool") {
		t.Errorf("expected unknown-tool error from per-agent executor, got %v", execErr)
	}

	// Cleanup must be non-nil; the deferred call above proves it doesn't
	// panic when invoked. Also assert calling it twice is idempotent —
	// connectAgentVault returns a no-op cancel when the vault is disabled,
	// so duplicate calls must not blow up.
	rt.Cleanup()
}

// mockBackend / newMockBackend are defined in sibling test files
// (e.g. orchestrator_test.go). Importing them here via shared package scope.

// We pin the agent's MemoryEnabled=false so connectAgentVault short-circuits
// without attempting an MCP dial — this test exercises the toolbelt path
// without needing a fake vault server.
func TestPrepareAgentRuntime_MemoryDisabled_SkipsVaultDial(t *testing.T) {
	mb := newMockBackend("")
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, stats.NoopCollector{}, nil)

	o.SetTools(newTestToolsReg(), nil)
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:          "MemOff",
		MemoryEnabled: false,
		VaultName:     "ignored",
	})
	o.SetAgentRegistry(reg)

	rt, err := o.PrepareAgentRuntime(context.Background(), "MemOff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rt == nil {
		t.Fatal("expected runtime even when memory is disabled (toolbelt is empty but runtime still wires the executor)")
	}
	defer rt.Cleanup()

	// ExtraSystem must be empty: no muninn tools registered means no
	// memory_mode instructions injected into the system prompt.
	if rt.ExtraSystem != "" {
		t.Errorf("expected empty ExtraSystem when memory disabled, got %q", rt.ExtraSystem)
	}
	// Schemas should not contain any muninn_* tools.
	for _, s := range rt.Schemas {
		if strings.HasPrefix(s.Function.Name, "muninn_") {
			t.Errorf("did not expect muninn_* schemas with memory disabled, got %q", s.Function.Name)
		}
	}

	var _ backend.Backend = mb // keep mb referenced
}
