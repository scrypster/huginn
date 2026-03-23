package agents_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
)

func TestLoadAgents_DefaultPathViaHOME(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HUGINN_HOME", "") // clear package-level override so HOME takes effect
	huginnDir := filepath.Join(dir, ".huginn")
	if err := os.MkdirAll(huginnDir, 0750); err != nil {
		t.Fatal(err)
	}
	cfg := &agents.AgentsConfig{
		Agents: []agents.AgentDef{
			{Name: "TestAgent", Model: "test:7b", Color: "#000", Icon: "T"},
		},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(huginnDir, "agents.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", dir)

	loaded, err := agents.LoadAgents()
	if err != nil {
		t.Fatalf("LoadAgents: %v", err)
	}
	if len(loaded.Agents) != 1 || loaded.Agents[0].Name != "TestAgent" {
		t.Errorf("expected TestAgent, got %v", loaded.Agents)
	}
}

func TestLoadAgents_MissingFileReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HUGINN_HOME", "") // clear package-level override so HOME takes effect
	t.Setenv("HOME", dir)

	loaded, err := agents.LoadAgents()
	if err != nil {
		t.Fatalf("LoadAgents: %v", err)
	}
	if len(loaded.Agents) == 0 {
		t.Error("expected default agents when file missing")
	}
}

func TestSaveAgents_DefaultPathViaHOME(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HUGINN_HOME", "") // clear package-level override so HOME takes effect
	t.Setenv("HOME", dir)

	cfg := &agents.AgentsConfig{
		Agents: []agents.AgentDef{
			{Name: "Saved", Model: "saved:7b", Color: "#FFF", Icon: "S"},
		},
	}
	if err := agents.SaveAgents(cfg); err != nil {
		t.Fatalf("SaveAgents: %v", err)
	}

	// SaveAgents now writes per-file: ~/.huginn/agents/<name>.json
	perFilePath := filepath.Join(dir, ".huginn", "agents", "saved.json")
	if _, err := os.Stat(perFilePath); err != nil {
		t.Fatalf("expected per-file agent at %q: %v", perFilePath, err)
	}

	loaded, err := agents.LoadAgents()
	if err != nil {
		t.Fatalf("LoadAgents after save: %v", err)
	}
	if len(loaded.Agents) != 1 || loaded.Agents[0].Name != "Saved" {
		t.Errorf("round-trip failed: %v", loaded.Agents)
	}
}

func TestSaveAgentsTo_InvalidPath(t *testing.T) {
	cfg := &agents.AgentsConfig{
		Agents: []agents.AgentDef{{Name: "X", Model: "m"}},
	}
	err := agents.SaveAgentsTo(cfg, "/dev/null/subdir/agents.json")
	if err == nil {
		t.Fatal("expected error for invalid path, got nil")
	}
}

func TestLoadAgentsFrom_ReadError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.json")
	// Create a directory where a file is expected
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	_, err := agents.LoadAgentsFrom(path)
	if err == nil {
		t.Fatal("expected error when agents path is a directory")
	}
}

func TestMemoryStore_NilStore_SaveSummary(t *testing.T) {
	ms := agents.NewMemoryStore(nil, "test-machine")
	err := ms.SaveSummary(context.Background(), agents.SessionSummary{
		SessionID: "s1",
		AgentName: "Chris",
		Summary:   "test",
	})
	if err != nil {
		t.Fatalf("expected nil error for nil store, got %v", err)
	}
}

func TestMemoryStore_NilStore_LoadSummaries(t *testing.T) {
	ms := agents.NewMemoryStore(nil, "test-machine")
	summaries, err := ms.LoadRecentSummaries(context.Background(), "Chris", 5)
	if err != nil {
		t.Fatalf("expected nil error for nil store, got %v", err)
	}
	if summaries != nil {
		t.Errorf("expected nil summaries for nil store, got %v", summaries)
	}
}

func TestMemoryStore_NilStore_AppendDelegation(t *testing.T) {
	ms := agents.NewMemoryStore(nil, "test-machine")
	err := ms.AppendDelegation(context.Background(), agents.DelegationEntry{
		From:     "Chris",
		To:       "Steve",
		Question: "test?",
		Answer:   "yes",
	})
	if err != nil {
		t.Fatalf("expected nil error for nil store, got %v", err)
	}
}

func TestMemoryStore_NilStore_LoadDelegations(t *testing.T) {
	ms := agents.NewMemoryStore(nil, "test-machine")
	entries, err := ms.LoadRecentDelegations(context.Background(), "Chris", "Steve", 5)
	if err != nil {
		t.Fatalf("expected nil error for nil store, got %v", err)
	}
	if entries != nil {
		t.Errorf("expected nil entries for nil store, got %v", entries)
	}
}

func TestBuildPersonaPromptWithMemory_WithDecisions(t *testing.T) {
	ag := &agents.Agent{
		Name:         "Chris",
		SystemPrompt: "You are Chris.",
	}
	summaries := []agents.SessionSummary{
		{
			Summary:       "Worked on auth",
			FilesTouched:  []string{"auth.go"},
			Decisions:     []string{"Use JWT"},
			OpenQuestions: []string{"Token expiry?"},
		},
	}
	result := agents.BuildPersonaPromptWithMemory(ag, "ctx", summaries)
	if len(result) == 0 {
		t.Fatal("expected non-empty prompt")
	}
	for _, c := range []string{"Decisions: Use JWT", "Open questions: Token expiry?", "Files: auth.go"} {
		if !strings.Contains(result, c) {
			t.Errorf("expected %q in prompt", c)
		}
	}
}

func TestSnapshotHistory_ExactN(t *testing.T) {
	ag := &agents.Agent{
		Name: "Test",
		History: []backend.Message{
			{Role: "user", Content: "a"},
			{Role: "assistant", Content: "b"},
			{Role: "user", Content: "c"},
		},
	}
	snap := ag.SnapshotHistory(3)
	if len(snap) != 3 {
		t.Errorf("expected 3 messages, got %d", len(snap))
	}
}
