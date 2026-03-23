package agents_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/modelconfig"
)

// TestLoadAgentsFrom_EmptyFile verifies that an empty (zero-byte) agents.json
// returns defaults rather than a parse error. An accidentally truncated file
// should not prevent the application from starting.
func TestLoadAgentsFrom_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.json")

	// Write a zero-byte file.
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := agents.LoadAgentsFrom(path)
	if err != nil {
		t.Fatalf("LoadAgentsFrom(empty file): unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadAgentsFrom(empty file): expected non-nil config")
	}
	// Defaults should be returned.
	if len(cfg.Agents) == 0 {
		t.Error("LoadAgentsFrom(empty file): expected default agents, got none")
	}
}

// TestBuildRegistry_EmptyAgentsSlice verifies that BuildRegistry with an empty
// AgentsConfig does not panic and returns an empty (but valid) registry.
func TestBuildRegistry_EmptyAgentsSlice(t *testing.T) {
	cfg := &agents.AgentsConfig{Agents: []agents.AgentDef{}}
	models := modelconfig.DefaultModels()

	// Must not panic.
	reg := agents.BuildRegistry(cfg, models)
	if reg == nil {
		t.Fatal("BuildRegistry(empty): expected non-nil registry")
	}
	if len(reg.All()) != 0 {
		t.Errorf("BuildRegistry(empty): expected 0 agents, got %d", len(reg.All()))
	}
	// DefaultAgent on an empty registry must return nil (not panic).
	if da := reg.DefaultAgent(); da != nil {
		t.Errorf("BuildRegistry(empty).DefaultAgent(): expected nil, got %v", da)
	}
}

// TestDeleteAgentDefault_NonExistent verifies that deleting an agent that
// does not exist returns an error rather than panicking.
func TestDeleteAgent_NonExistent(t *testing.T) {
	dir := t.TempDir()

	err := agents.DeleteAgent(dir, "ghost-agent")
	if err == nil {
		t.Error("DeleteAgent(non-existent): expected error, got nil")
	}
}

// TestLoadAgentsFrom_WhitespaceOnly verifies that a file containing only
// whitespace (not valid JSON) is treated as a parse error.
func TestLoadAgentsFrom_WhitespaceOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.json")

	if err := os.WriteFile(path, []byte("   \n  \t  "), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := agents.LoadAgentsFrom(path)
	// Whitespace-only is not valid JSON and should produce a parse error.
	if err == nil {
		t.Error("LoadAgentsFrom(whitespace-only): expected parse error, got nil")
	}
}
