package agents_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

func TestMigrateEmptyToolbelt_BackfillsWildcard(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentsDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Write agent with empty toolbelt (old default-allow behavior)
	def := agents.AgentDef{Name: "old-agent", Toolbelt: nil}
	data, _ := json.MarshalIndent(def, "", "  ")
	os.WriteFile(filepath.Join(agentsDir, "old-agent.json"), data, 0o600)

	// Write agent with explicit connections (should be untouched)
	defWithConn := agents.AgentDef{
		Name:     "conn-agent",
		Toolbelt: []agents.ToolbeltEntry{{ConnectionID: "conn-1", Provider: "aws"}},
	}
	data2, _ := json.MarshalIndent(defWithConn, "", "  ")
	os.WriteFile(filepath.Join(agentsDir, "conn-agent.json"), data2, 0o600)

	if err := agents.MigrateEmptyToolbeltToWildcard(dir); err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// old-agent should now have wildcard toolbelt
	data3, _ := os.ReadFile(filepath.Join(agentsDir, "old-agent.json"))
	var migrated agents.AgentDef
	json.Unmarshal(data3, &migrated)
	if len(migrated.Toolbelt) != 1 || migrated.Toolbelt[0].Provider != "*" {
		t.Errorf("expected wildcard toolbelt, got %v", migrated.Toolbelt)
	}

	// conn-agent should be untouched
	data4, _ := os.ReadFile(filepath.Join(agentsDir, "conn-agent.json"))
	var untouched agents.AgentDef
	json.Unmarshal(data4, &untouched)
	if len(untouched.Toolbelt) != 1 || untouched.Toolbelt[0].Provider == "*" {
		t.Errorf("conn-agent toolbelt should be unchanged, got %v", untouched.Toolbelt)
	}
}
