package agents_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

// TestSaveAgent_ConcurrentWrites verifies that concurrent SaveAgent calls for
// distinct agents do not race or lose files (each uses an atomic rename).
func TestSaveAgent_ConcurrentWrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	const n = 10
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			agent := agents.AgentDef{
				Name:  fmt.Sprintf("agent-%d", idx),
				Model: "test-model",
			}
			if err := agents.SaveAgent(dir, agent); err != nil {
				t.Errorf("SaveAgent %d: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	// All n agent files must exist.
	pattern := filepath.Join(dir, "agents", "*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(matches) != n {
		t.Errorf("expected %d agent files, found %d", n, len(matches))
	}
}

// TestMigrateAgents_PartiallyCorruptLegacy verifies that MigrateAgents returns
// an error (rather than panicking) when agents.json contains invalid JSON.
func TestMigrateAgents_PartiallyCorruptLegacy(t *testing.T) {
	dir := t.TempDir()

	// Write syntactically invalid JSON.
	corrupt := `[{"name":"valid","slot":"coder"},{"name":}]`
	if err := os.WriteFile(filepath.Join(dir, "agents.json"), []byte(corrupt), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := agents.MigrateAgents(dir)
	// Invalid JSON must produce an error (not a panic).
	if err == nil {
		t.Error("expected error from MigrateAgents with corrupt legacy file, got nil")
	}
}

// TestSaveAgent_OverwriteExisting verifies that saving an agent with the same
// name overwrites the previous file cleanly (no stale data).
func TestSaveAgent_OverwriteExisting(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	first := agents.AgentDef{Name: "overwrite-me", Model: "model-v1"}
	if err := agents.SaveAgent(dir, first); err != nil {
		t.Fatalf("first SaveAgent: %v", err)
	}

	second := agents.AgentDef{Name: "overwrite-me", Model: "model-v2"}
	if err := agents.SaveAgent(dir, second); err != nil {
		t.Fatalf("second SaveAgent: %v", err)
	}

	path := filepath.Join(dir, "agents", "overwrite-me.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got agents.AgentDef
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Model != "model-v2" {
		t.Errorf("expected model-v2 after overwrite, got %q", got.Model)
	}
}

// TestSaveAgent_SpecialCharsInName verifies that agent names with spaces and
// special characters are sanitized to a safe filename without panicking.
func TestSaveAgent_SpecialCharsInName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	agent := agents.AgentDef{
		Name:  "My Agent! #1",
		Model: "test",
	}
	if err := agents.SaveAgent(dir, agent); err != nil {
		t.Fatalf("SaveAgent with special chars: %v", err)
	}

	// The agents/ directory must contain exactly one file with a safe name.
	pattern := filepath.Join(dir, "agents", "*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(matches) != 1 {
		t.Errorf("expected 1 agent file, got %d: %v", len(matches), matches)
	}

	// No .tmp file should be left behind.
	tmpPattern := filepath.Join(dir, "agents", "*.json.tmp")
	tmps, _ := filepath.Glob(tmpPattern)
	if len(tmps) > 0 {
		t.Errorf(".tmp files left behind: %v", tmps)
	}
}

// TestDeleteAgent_AfterConcurrentSave verifies DeleteAgent succeeds after
// SaveAgent completes under concurrent saves of other agents.
func TestDeleteAgent_AfterConcurrentSave(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Pre-create the target agent.
	target := agents.AgentDef{Name: "target-delete", Model: "m"}
	if err := agents.SaveAgent(dir, target); err != nil {
		t.Fatalf("SaveAgent target: %v", err)
	}

	// Concurrently save unrelated agents.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = agents.SaveAgent(dir, agents.AgentDef{
				Name:  fmt.Sprintf("concurrent-%d", idx),
				Model: "m",
			})
		}(i)
	}
	wg.Wait()

	// Delete the target.
	if err := agents.DeleteAgent(dir, "target-delete"); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}

	deletedPath := filepath.Join(dir, "agents", "target-delete.json")
	if _, err := os.Stat(deletedPath); err == nil {
		t.Error("expected target-delete.json to be gone after DeleteAgent")
	}
}
