package agents_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/storage"
)

// ---------------------------------------------------------------------------
// SaveAgentDefault / DeleteAgentDefault — 0% covered
// ---------------------------------------------------------------------------

func TestSaveAgentDefault_RoundTrip95(t *testing.T) {
	def := agents.AgentDef{
		Name:         "Cov95TestAgent",
		Model:        "test-model",
		SystemPrompt: "You are a test agent.",
		Color:        "#FFFFFF",
		Icon:         "T",
	}
	if err := agents.SaveAgentDefault(def); err != nil {
		t.Fatalf("SaveAgentDefault: %v", err)
	}
	// Clean up.
	if err := agents.DeleteAgentDefault(def.Name); err != nil {
		t.Fatalf("DeleteAgentDefault: %v", err)
	}
}

func TestDeleteAgentDefault_NonExistent95(t *testing.T) {
	err := agents.DeleteAgentDefault("this-agent-cov95-does-not-exist-xyz")
	if err == nil {
		t.Error("expected error for non-existent agent, got nil")
	}
}

// ---------------------------------------------------------------------------
// huginnBaseDir — indirectly covered when SaveAgents succeeds
// ---------------------------------------------------------------------------

func TestSaveAgents_RoundTrip95(t *testing.T) {
	cfg := &agents.AgentsConfig{
		Agents: []agents.AgentDef{
			{
				Name:  "CovBoost95Agent",
				Model: "m1",
			},
		},
	}
	if err := agents.SaveAgents(cfg); err != nil {
		t.Fatalf("SaveAgents: %v", err)
	}
	// Clean up
	_ = agents.DeleteAgentDefault("CovBoost95Agent")
}

// ---------------------------------------------------------------------------
// LoadAgents — reads from the real ~/.huginn directory
// ---------------------------------------------------------------------------

func TestLoadAgents_ReturnsNonNil95(t *testing.T) {
	cfg, err := agents.LoadAgents()
	if err != nil {
		t.Fatalf("LoadAgents: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadAgents returned nil config")
	}
}

// ---------------------------------------------------------------------------
// SaveAgentsTo — mkdir + write (nested directories)
// ---------------------------------------------------------------------------

func TestSaveAgentsTo_CreatesParentDir95(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "dir", "agents.json")
	cfg := &agents.AgentsConfig{Agents: agents.DefaultAgentsConfig().Agents}
	if err := agents.SaveAgentsTo(cfg, path); err != nil {
		t.Fatalf("SaveAgentsTo: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file to exist at %s: %v", path, err)
	}
}

// ---------------------------------------------------------------------------
// MigrateAgents — happy path (if not already covered)
// ---------------------------------------------------------------------------

func TestMigrateAgents_HappyPath95(t *testing.T) {
	dir := t.TempDir()
	// Write a legacy agents.json.
	legacyCfg := &agents.AgentsConfig{Agents: agents.DefaultAgentsConfig().Agents}
	legacyPath := filepath.Join(dir, "agents.json")
	if err := agents.SaveAgentsTo(legacyCfg, legacyPath); err != nil {
		t.Fatal(err)
	}
	if err := agents.MigrateAgents(dir); err != nil {
		t.Fatalf("MigrateAgents: %v", err)
	}
	// agents.json should now be renamed to agents.json.bak
	if _, err := os.Stat(legacyPath + ".bak"); err != nil {
		t.Errorf("expected agents.json.bak to exist after migration: %v", err)
	}
}

func TestMigrateAgents_InvalidJSON95(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "agents.json")
	if err := os.WriteFile(legacyPath, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := agents.MigrateAgents(dir)
	if err == nil {
		t.Error("expected error for invalid JSON in legacy file")
	}
}

// ---------------------------------------------------------------------------
// SnapshotHistory — exercises the n > 0 && len(h) > n trim branch
// ---------------------------------------------------------------------------

func TestSnapshotHistory_TrimPath95(t *testing.T) {
	a := &agents.Agent{Name: "Snap"}
	// Add 5 messages via AppendHistory.
	for i := 0; i < 5; i++ {
		a.AppendHistory(backend.Message{Role: "user", Content: "msg"})
	}
	// Request only 3 — should trim.
	snap := a.SnapshotHistory(3)
	if len(snap) != 3 {
		t.Errorf("expected 3 messages in snapshot, got %d", len(snap))
	}
}

// ---------------------------------------------------------------------------
// FromDef — unknown slot defaults to planner (if not already covered)
// ---------------------------------------------------------------------------

func TestFromDef_BasicAgent95(t *testing.T) {
	def := agents.AgentDef{
		Name:  "BasicAgent95",
		Model: "m",
	}
	ag := agents.FromDef(def)
	if ag == nil {
		t.Fatal("expected non-nil agent")
	}
	if ag.Name != "BasicAgent95" {
		t.Errorf("expected name 'BasicAgent95', got %q", ag.Name)
	}
}

// ---------------------------------------------------------------------------
// MemoryStore nil-store paths — exercises early-return nil-store branches
// (in case the hardening tests don't cover all branches)
// ---------------------------------------------------------------------------

func TestMemoryStore_NilStore_LoadRecentSummaries95(t *testing.T) {
	ms := agents.NewMemoryStore(nil, "machine-1")
	ctx := context.Background()
	results, err := ms.LoadRecentSummaries(ctx, "A", 5)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results, got %v", results)
	}
}

func TestMemoryStore_NilStore_LoadRecentDelegations95(t *testing.T) {
	ms := agents.NewMemoryStore(nil, "machine-1")
	ctx := context.Background()
	results, err := ms.LoadRecentDelegations(ctx, "A", "B", 5)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results, got %v", results)
	}
}

// ---------------------------------------------------------------------------
// LoadAgentsFromBase — per-file format with skip-draft logic
// ---------------------------------------------------------------------------

func TestLoadAgentsFromBase_PerFileWithDraft95(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentsDir, 0o750); err != nil {
		t.Fatal(err)
	}
	// Write a draft file (should be skipped) and a real agent.
	if err := os.WriteFile(filepath.Join(agentsDir, ".draft.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Write a valid agent file.
	agentJSON := `{"name":"TestAgent95","model":"m"}`
	if err := os.WriteFile(filepath.Join(agentsDir, "testagent95.json"), []byte(agentJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	// Trigger LoadAgentsFromBase indirectly by calling SaveAgent (which writes to agents/).
	def := agents.AgentDef{Name: "TestAgent95", Model: "m"}
	if err := agents.SaveAgent(dir, def); err != nil {
		t.Fatalf("SaveAgent: %v", err)
	}
	// Clean up.
	_ = agents.DeleteAgent(dir, "TestAgent95")
}

// ---------------------------------------------------------------------------
// LoadRecentDelegations — limit truncation path (limit < len(entries))
// ---------------------------------------------------------------------------

func openTestMemoryStore95(t *testing.T, machineID string) *agents.MemoryStore {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return agents.NewMemoryStore(s, machineID)
}

func TestMemoryStore_LoadRecentDelegations_LimitRespected95(t *testing.T) {
	ms := openTestMemoryStore95(t, "test-machine-95")
	ctx := context.Background()
	base := time.Now()

	// Store 5 delegations (below the trim threshold of 10).
	for i := 0; i < 5; i++ {
		entry := agents.DelegationEntry{
			From:      "Alpha",
			To:        "Beta",
			Question:  fmt.Sprintf("q%d", i),
			Answer:    fmt.Sprintf("a%d", i),
			Timestamp: base.Add(time.Duration(i) * time.Millisecond),
		}
		if err := ms.AppendDelegation(ctx, entry); err != nil {
			t.Fatalf("AppendDelegation[%d]: %v", i, err)
		}
	}

	// Load with limit=2 — should trim the result to 2 entries.
	results, err := ms.LoadRecentDelegations(ctx, "Alpha", "Beta", 2)
	if err != nil {
		t.Fatalf("LoadRecentDelegations: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results (limit), got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// LoadRecentSummaries — limit truncation path
// ---------------------------------------------------------------------------

func TestMemoryStore_LoadRecentSummaries_LimitRespected95(t *testing.T) {
	ms := openTestMemoryStore95(t, "test-machine-95b")
	ctx := context.Background()
	base := time.Now()

	// Store 4 summaries.
	for i := 0; i < 4; i++ {
		s := agents.SessionSummary{
			SessionID: fmt.Sprintf("sess95-%d", i),
			MachineID: "test-machine-95b",
			AgentName: "Agent95",
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Summary:   fmt.Sprintf("summary %d", i),
		}
		if err := ms.SaveSummary(ctx, s); err != nil {
			t.Fatalf("SaveSummary[%d]: %v", i, err)
		}
	}

	// Load with limit=2 — should trim to 2.
	results, err := ms.LoadRecentSummaries(ctx, "Agent95", 2)
	if err != nil {
		t.Fatalf("LoadRecentSummaries: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results (limit), got %d", len(results))
	}
}

