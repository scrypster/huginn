package server

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/memory"
)

// TestResolveAgent_VaultNameResolved_WhenDefVaultEmpty asserts that an agent
// loaded via resolveAgent has its VaultName populated even when agents.json
// leaves it blank. Without this parity with BuildRegistryWithUsername, the
// orchestrator's connectAgentVault would short-circuit (VaultName=="") and
// no muninn_* tools would be registered for the session — exactly the
// regression that made MuninnDB silent in chat.
func TestResolveAgent_VaultNameResolved_WhenDefVaultEmpty(t *testing.T) {
	cfg := testAgentConfig(
		// VaultName intentionally left blank; expect ResolvedVaultName to fill it.
		testAgent("Stacy", "model-a", "#fff", "S", true),
	)
	s := &Server{
		agentLoader: func() (*agents.AgentsConfig, error) { return cfg, nil },
	}

	ag := s.resolveAgent("session-id-noop")
	if ag == nil {
		t.Fatal("expected non-nil agent from resolveAgent")
	}
	if ag.VaultName == "" {
		t.Fatalf("expected resolveAgent to populate VaultName via ResolvedVaultName; got empty")
	}

	want := agents.AgentDef{Name: "Stacy"}.ResolvedVaultName(memory.ResolveUsername(""))
	if ag.VaultName != want {
		t.Errorf("VaultName parity broken: got %q, want %q (computed via ResolvedVaultName)", ag.VaultName, want)
	}

	if !strings.HasPrefix(ag.VaultName, "huginn:agent:") {
		t.Errorf("expected VaultName to use huginn:agent:<user>:<agent> pattern, got %q", ag.VaultName)
	}
}

// TestResolveAgent_VaultName_ExplicitWins asserts that when agents.json sets
// an explicit VaultName, resolveAgent preserves it (does not overwrite with
// the default huginn:agent:... pattern).
func TestResolveAgent_VaultName_ExplicitWins(t *testing.T) {
	def := testAgent("Bob", "model-a", "#fff", "B", true)
	def.VaultName = "custom:vault:bob"
	cfg := testAgentConfig(def)
	s := &Server{
		agentLoader: func() (*agents.AgentsConfig, error) { return cfg, nil },
	}

	ag := s.resolveAgent("session-id-noop")
	if ag == nil {
		t.Fatal("expected non-nil agent from resolveAgent")
	}
	if ag.VaultName != "custom:vault:bob" {
		t.Errorf("expected explicit VaultName to be preserved; got %q", ag.VaultName)
	}
}
