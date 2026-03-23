package agents_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

// ---------------------------------------------------------------------------
// SaveAgent / LoadAgents (per-file)
// ---------------------------------------------------------------------------

func TestSaveAndLoadAgent(t *testing.T) {
	dir := t.TempDir()
	agent := agents.AgentDef{
		Name:  "test-agent",
		
		Model: "test-model",
	}

	if err := agents.SaveAgent(dir, agent); err != nil {
		t.Fatalf("SaveAgent: %v", err)
	}

	// Verify file exists at expected path (hyphens are preserved by sanitizer).
	expectedPath := filepath.Join(dir, "agents", "test-agent.json")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("expected file at %s: %v", expectedPath, err)
	}

	// LoadAgents should find the per-file agent.
	loaded, err := loadAgentsFromDir(dir)
	if err != nil {
		t.Fatalf("LoadAgents: %v", err)
	}
	if len(loaded) != 1 {
		t.Errorf("expected 1 agent, got %d", len(loaded))
	}
	if loaded[0].Name != "test-agent" {
		t.Errorf("name mismatch: %q", loaded[0].Name)
	}
}

func TestLoadAgents_Empty(t *testing.T) {
	dir := t.TempDir()
	// No agents.json, no agents/ dir → should return defaults (not error).
	cfg, err := agents.LoadAgentsFrom(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatalf("LoadAgentsFrom empty: %v", err)
	}
	// Blank canvas: empty config is valid when no file exists.
	if cfg == nil {
		t.Errorf("expected non-nil config when file missing")
	}
}

func TestSaveAgent_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	agent := agents.AgentDef{Name: "atomic-test", }

	if err := agents.SaveAgent(dir, agent); err != nil {
		t.Fatal(err)
	}

	// No .tmp file should remain (hyphens preserved, spaces become underscores).
	tmpPath := filepath.Join(dir, "agents", "atomic-test.json.tmp")
	if _, err := os.Stat(tmpPath); err == nil {
		t.Error(".tmp file left behind after SaveAgent")
	}
}

// ---------------------------------------------------------------------------
// DeleteAgent
// ---------------------------------------------------------------------------

func TestDeleteAgent(t *testing.T) {
	dir := t.TempDir()
	if err := agents.SaveAgent(dir, agents.AgentDef{Name: "to-delete", }); err != nil {
		t.Fatal(err)
	}

	if err := agents.DeleteAgent(dir, "to-delete"); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}

	expectedPath := filepath.Join(dir, "agents", "to-delete.json")
	if _, err := os.Stat(expectedPath); err == nil {
		t.Error("expected file to be removed after DeleteAgent")
	}
}

func TestDeleteAgent_NotFound(t *testing.T) {
	dir := t.TempDir()
	if err := agents.DeleteAgent(dir, "nonexistent"); err == nil {
		t.Error("expected error deleting nonexistent agent")
	}
}

// ---------------------------------------------------------------------------
// MigrateAgents
// ---------------------------------------------------------------------------

func TestMigrateAgents(t *testing.T) {
	dir := t.TempDir()

	// Write legacy agents.json.
	legacy := agents.AgentsConfig{
		Agents: []agents.AgentDef{
			{Name: "steve", Model: "gpt-4"},
			{Name: "alice", Model: "claude-3"},
		},
	}
	data, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := agents.MigrateAgents(dir); err != nil {
		t.Fatalf("MigrateAgents: %v", err)
	}

	// agents.json should be renamed to .bak.
	if _, err := os.Stat(filepath.Join(dir, "agents.json")); err == nil {
		t.Error("agents.json should have been renamed to .bak")
	}
	if _, err := os.Stat(filepath.Join(dir, "agents.json.bak")); err != nil {
		t.Error("agents.json.bak not found")
	}

	// Per-file agents should exist.
	perFileAgents, err := loadAgentsFromDir(dir)
	if err != nil {
		t.Fatalf("loadAgentsFromDir after migration: %v", err)
	}
	if len(perFileAgents) != 2 {
		t.Errorf("expected 2 agents after migration, got %d", len(perFileAgents))
	}
}

func TestMigrateAgents_Idempotent(t *testing.T) {
	dir := t.TempDir()
	// No legacy file — should not error.
	if err := agents.MigrateAgents(dir); err != nil {
		t.Errorf("MigrateAgents with no legacy file: %v", err)
	}
	if err := agents.MigrateAgents(dir); err != nil {
		t.Errorf("MigrateAgents second call: %v", err)
	}
}

func TestMigrateAgents_AlreadyMigrated(t *testing.T) {
	dir := t.TempDir()

	// Create per-file agents directly.
	if err := agents.SaveAgent(dir, agents.AgentDef{Name: "existing", }); err != nil {
		t.Fatal(err)
	}

	// No legacy file — MigrateAgents is a no-op.
	if err := agents.MigrateAgents(dir); err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Existing per-file agent should still be intact.
	path := filepath.Join(dir, "agents", "existing.json")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("per-file agent missing after MigrateAgents no-op: %v", err)
	}
}

// ---------------------------------------------------------------------------
// AgentDef new fields
// ---------------------------------------------------------------------------

func TestAgentDef_NewFields_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	agent := agents.AgentDef{
		Name:      "new-fields-agent",
		Model:     "some-model",
		ID:        "uuid-1234",
		CreatedAt: "2026-03-04T00:00:00Z",
	}

	if err := agents.SaveAgent(dir, agent); err != nil {
		t.Fatalf("SaveAgent: %v", err)
	}

	path := filepath.Join(dir, "agents", "new-fields-agent.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var got agents.AgentDef
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.ID != "uuid-1234" {
		t.Errorf("ID not preserved: %q", got.ID)
	}
	if got.CreatedAt != "2026-03-04T00:00:00Z" {
		t.Errorf("CreatedAt not preserved: %q", got.CreatedAt)
	}
}

func TestAgentDef_OmitEmptyNewFields(t *testing.T) {
	// When ID and CreatedAt are empty, they should be omitted from JSON.
	agent := agents.AgentDef{Name: "plain", Model: "m"}
	data, err := json.Marshal(agent)
	if err != nil {
		t.Fatal(err)
	}

	// Should not contain "id" or "created_at" keys when empty.
	raw := string(data)
	if containsKey(raw, `"id"`) {
		t.Errorf("expected id to be omitted when empty, got: %s", raw)
	}
	if containsKey(raw, `"created_at"`) {
		t.Errorf("expected created_at to be omitted when empty, got: %s", raw)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// loadAgentsFromDir loads per-file agents from <dir>/agents/*.json directly
// by reading the directory without relying on LoadAgents (which uses home dir).
func loadAgentsFromDir(dir string) ([]agents.AgentDef, error) {
	pattern := filepath.Join(dir, "agents", "*.json")
	entries, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	var result []agents.AgentDef
	for _, path := range entries {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var a agents.AgentDef
		if err := json.Unmarshal(data, &a); err != nil {
			continue
		}
		result = append(result, a)
	}
	return result, nil
}

// containsKey checks if a JSON string contains a given key literal.
func containsKey(jsonStr, key string) bool {
	for i := 0; i <= len(jsonStr)-len(key); i++ {
		if jsonStr[i:i+len(key)] == key {
			return true
		}
	}
	return false
}
