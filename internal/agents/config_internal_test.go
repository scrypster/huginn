package agents

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSaveAgentsTo_MkdirError covers the MkdirAll error path in SaveAgentsTo.
func TestSaveAgentsTo_MkdirError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permission checks")
	}
	dir := t.TempDir()
	// Make the dir unwritable so MkdirAll for a nested path fails.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755) //nolint:errcheck

	path := filepath.Join(dir, "nested", "agents.json")
	cfg := &AgentsConfig{Agents: []AgentDef{{Name: "X", Model: "m"}}}
	err := SaveAgentsTo(cfg, path)
	if err == nil {
		t.Error("expected error for unwritable directory, got nil")
	}
}

// TestSaveAgent_MkdirError covers the MkdirAll error path in SaveAgent.
func TestSaveAgent_MkdirError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permission checks")
	}
	dir := t.TempDir()
	// Make the base dir unwritable so creating "agents" subdir fails.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755) //nolint:errcheck

	def := AgentDef{Name: "TestX", Model: "m"}
	err := SaveAgent(dir, def)
	if err == nil {
		t.Error("expected error for unwritable base dir, got nil")
	}
}

// TestLoadAgentsFromBase_SkipsUnreadableFile verifies that loadAgentsFromBase
// skips individual agent files that cannot be read.
func TestLoadAgentsFromBase_SkipsUnreadableFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permission checks")
	}
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentsDir, 0o750); err != nil {
		t.Fatal(err)
	}

	// Write an unreadable file.
	unreadable := filepath.Join(agentsDir, "noread.json")
	if err := os.WriteFile(unreadable, []byte(`{"name":"noread","slot":"coder","model":"m"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(unreadable, 0o644) //nolint:errcheck

	// Write a readable valid agent file.
	readable := filepath.Join(agentsDir, "readable.json")
	if err := os.WriteFile(readable, []byte(`{"name":"Readable","slot":"planner","model":"m"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadAgentsFromBase(dir)
	if err != nil {
		t.Fatalf("loadAgentsFromBase: %v", err)
	}
	// Should have exactly 1 agent (the readable one).
	if len(cfg.Agents) != 1 {
		t.Errorf("expected 1 agent (skipping unreadable), got %d", len(cfg.Agents))
	}
	if cfg.Agents[0].Name != "Readable" {
		t.Errorf("expected Readable, got %q", cfg.Agents[0].Name)
	}
}

// TestLoadAgentsFromBase_SkipsDraftFile verifies that loadAgentsFromBase
// skips .draft.json files.
func TestLoadAgentsFromBase_SkipsDraftFile(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentsDir, 0o750); err != nil {
		t.Fatal(err)
	}

	// Write a .draft.json file that should be skipped.
	draft := filepath.Join(agentsDir, ".draft.json")
	if err := os.WriteFile(draft, []byte(`{"name":"Draft","slot":"coder","model":"m"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Write a valid agent file.
	valid := filepath.Join(agentsDir, "valid.json")
	if err := os.WriteFile(valid, []byte(`{"name":"Valid","slot":"planner","model":"m"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadAgentsFromBase(dir)
	if err != nil {
		t.Fatalf("loadAgentsFromBase: %v", err)
	}
	// .draft.json should be skipped; only Valid should be returned.
	for _, a := range cfg.Agents {
		if a.Name == "Draft" {
			t.Error("expected .draft.json to be skipped, but found Draft agent")
		}
	}
}

// TestLoadAgentsFromBase_SkipsCorruptFile verifies that loadAgentsFromBase
// skips individual agent files that contain invalid JSON.
func TestLoadAgentsFromBase_SkipsCorruptFile(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentsDir, 0o750); err != nil {
		t.Fatal(err)
	}

	// Write a corrupt (invalid JSON) agent file.
	corrupt := filepath.Join(agentsDir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte(`not valid json {`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Write a valid agent file.
	valid := filepath.Join(agentsDir, "valid.json")
	if err := os.WriteFile(valid, []byte(`{"name":"Valid","slot":"planner","model":"m"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadAgentsFromBase(dir)
	if err != nil {
		t.Fatalf("loadAgentsFromBase: %v", err)
	}
	if len(cfg.Agents) != 1 {
		t.Errorf("expected 1 agent (skipping corrupt), got %d", len(cfg.Agents))
	}
	if cfg.Agents[0].Name != "Valid" {
		t.Errorf("expected Valid, got %q", cfg.Agents[0].Name)
	}
}

// TestLoadAgentsFromBase_AllFilesCorrupt verifies that loadAgentsFromBase falls
// back to the legacy agents.json path when all per-file agents are unreadable.
func TestLoadAgentsFromBase_AllFilesCorrupt(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentsDir, 0o750); err != nil {
		t.Fatal(err)
	}

	// Only corrupt files in agents/.
	corrupt := filepath.Join(agentsDir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte(`{bad json`), 0o600); err != nil {
		t.Fatal(err)
	}

	// No legacy agents.json either — should return defaults.
	cfg, err := loadAgentsFromBase(dir)
	if err != nil {
		t.Fatalf("loadAgentsFromBase: %v", err)
	}
	// Corrupt files → empty config is fine (blank canvas).
	if cfg == nil {
		t.Error("expected non-nil config when all files corrupt")
	}
}
