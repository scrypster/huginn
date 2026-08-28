package agent

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/tools"
)

// TestConnectAgentVault_NegativeCache_SkipsRetryWithinTTL verifies that a
// failed/unconfigured vault connect for an agent is not re-attempted on the
// very next call within the negative-cache TTL window. We prove "no second
// attempt happened" indirectly: between the two calls we mutate muninnCfgPath
// to a value that would produce a *different* warning if connectAgentVault
// actually re-ran its config-load logic. A cached hit must still return the
// first call's warning unchanged.
func TestConnectAgentVault_NegativeCache_SkipsRetryWithinTTL(t *testing.T) {
	o := newTestOrchestrator()
	parent := tools.NewRegistry()
	ag := &agents.Agent{
		Name:          "cache-test-agent-" + t.Name(),
		MemoryEnabled: true,
		VaultName:     "huginn:agent:default:cachetestagent",
	}

	// First call: muninnCfgPath is empty → "muninn config path not set".
	vr1 := o.connectAgentVault(context.Background(), ag, parent)
	vr1.cancel()
	if vr1.warning == "" {
		t.Fatal("expected a warning on first (unconfigured) call")
	}

	// Point cfgPath at a nonexistent file. If connectAgentVault actually
	// re-attempted config load here, it would hit the "config unavailable"
	// branch and produce a different warning string.
	o.mu.Lock()
	o.muninnCfgPath = "/nonexistent/definitely/not/here/muninn.json"
	o.mu.Unlock()

	vr2 := o.connectAgentVault(context.Background(), ag, parent)
	vr2.cancel()
	if vr2.warning != vr1.warning {
		t.Fatalf("expected cached warning %q to be reused within TTL, got %q (indicates a second connect attempt occurred)", vr1.warning, vr2.warning)
	}
}

// TestConnectAgentVault_NegativeCache_KeyedPerAgent verifies the negative
// cache does not cross-contaminate between agents: a cached failure for one
// agent must not suppress the retry attempt for a different agent.
func TestConnectAgentVault_NegativeCache_KeyedPerAgent(t *testing.T) {
	o := newTestOrchestrator()
	parent := tools.NewRegistry()

	agA := &agents.Agent{Name: "agent-a-" + t.Name(), MemoryEnabled: true, VaultName: "huginn:agent:default:agenta"}
	agB := &agents.Agent{Name: "agent-b-" + t.Name(), MemoryEnabled: true, VaultName: "huginn:agent:default:agentb"}

	vrA := o.connectAgentVault(context.Background(), agA, parent)
	vrA.cancel()
	if vrA.warning == "" {
		t.Fatal("expected a warning for agent A")
	}

	// Change cfgPath so agent B, if it actually attempts a fresh connect,
	// gets a different (config-unavailable) warning than agent A's
	// (config-path-not-set) warning.
	o.mu.Lock()
	o.muninnCfgPath = "/nonexistent/definitely/not/here/muninn.json"
	o.mu.Unlock()

	vrB := o.connectAgentVault(context.Background(), agB, parent)
	vrB.cancel()
	if vrB.warning == "" {
		t.Fatal("expected a warning for agent B")
	}
	if vrB.warning == vrA.warning {
		t.Fatalf("agent B's connect attempt appears to have been suppressed by agent A's cached failure (got identical warning %q)", vrB.warning)
	}
}

// TestConnectAgentVault_NegativeCache_ScopedPerOrchestrator verifies the
// cache is per-Orchestrator instance, not a shared package-level global —
// two different orchestrators (e.g. two tests, or two server instances) must
// never leak cached vault failures into each other even when an agent name
// collides.
func TestConnectAgentVault_NegativeCache_ScopedPerOrchestrator(t *testing.T) {
	const agentName = "shared-name-agent"
	parent := tools.NewRegistry()
	ag := &agents.Agent{Name: agentName, MemoryEnabled: true, VaultName: "huginn:agent:default:x"}

	o1 := newTestOrchestrator()
	vr1 := o1.connectAgentVault(context.Background(), ag, parent)
	vr1.cancel()
	if vr1.warning == "" {
		t.Fatal("expected a warning from o1")
	}

	// A fresh orchestrator with the same agent name and a different cfgPath
	// must attempt its own connect, not inherit o1's cached failure.
	o2 := newTestOrchestrator()
	o2.mu.Lock()
	o2.muninnCfgPath = "/nonexistent/definitely/not/here/muninn.json"
	o2.mu.Unlock()

	vr2 := o2.connectAgentVault(context.Background(), ag, parent)
	vr2.cancel()
	if vr2.warning == vr1.warning {
		t.Fatalf("o2's connect attempt appears to have inherited o1's cached failure (got identical warning %q)", vr2.warning)
	}
}

// TestConnectAgentVault_NegativeCache_GetSetClear is a focused unit test of
// the cache helpers themselves.
func TestConnectAgentVault_NegativeCache_GetSetClear(t *testing.T) {
	const agentName = "unit-cache-agent"
	o := newTestOrchestrator()

	if _, ok := o.vaultNegativeCacheGet(agentName); ok {
		t.Fatal("expected cache miss before any Set")
	}

	o.vaultNegativeCacheSet(agentName, "boom")
	warn, ok := o.vaultNegativeCacheGet(agentName)
	if !ok || warn != "boom" {
		t.Fatalf("expected cached warning %q, got (%q, %v)", "boom", warn, ok)
	}

	o.vaultNegativeCacheClear(agentName)
	if _, ok := o.vaultNegativeCacheGet(agentName); ok {
		t.Fatal("expected cache miss after Clear")
	}
}
