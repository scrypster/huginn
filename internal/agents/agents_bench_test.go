package agents

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkAgentConfigLoad benchmarks loading agent definitions from a per-file
// directory with 5 agent JSON files.
func BenchmarkAgentConfigLoad(b *testing.B) {
	dir := b.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		b.Fatalf("mkdir: %v", err)
	}

	// Create 5 agent JSON files.
	for i := 0; i < 5; i++ {
		name := filepath.Join(agentsDir, agentNames[i]+".json")
		data := []byte(`{
			"name": "` + agentNames[i] + `",
			"model": "claude-sonnet-4-6",
			"system_prompt": "You are a helpful assistant specialised in ` + agentNames[i] + `.",
			"color": "#58A6FF",
			"icon": "` + string(rune('A'+i)) + `",
			"memory_enabled": true,
			"vault_name": "huginn:agent:test:` + agentNames[i] + `",
			"memory_mode": "conversational"
		}`)
		if err := os.WriteFile(name, data, 0644); err != nil {
			b.Fatalf("write %s: %v", name, err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cfg, err := LoadAgentsFromBase(dir)
		if err != nil {
			b.Fatalf("LoadAgentsFromBase: %v", err)
		}
		if len(cfg.Agents) != 5 {
			b.Fatalf("expected 5 agents, got %d", len(cfg.Agents))
		}
	}
}

var agentNames = []string{"coder", "planner", "reviewer", "researcher", "analyst"}

// BenchmarkBuildMemoryBlock benchmarks the BuildMemoryBlock function which
// constructs memory system instructions for injection into agent prompts.
func BenchmarkBuildMemoryBlock(b *testing.B) {
	ag := &Agent{
		Name:             "bench-agent",
		MemoryEnabled:    true,
		VaultName:        "huginn:agent:test:bench-agent",
		MemoryMode:       "conversational",
		VaultDescription: "This vault stores architectural decisions and code patterns for the project.",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := BuildMemoryBlock(ag)
		if result == "" {
			b.Fatal("expected non-empty memory block")
		}
	}
}
