package agents

import (
	"strings"
	"testing"
)

func TestAgentIsCoS(t *testing.T) {
	cos := &Agent{Name: "Reggie", LocalTools: []string{"create_agent", "bash"}}
	if !AgentIsCoS(cos) {
		t.Errorf("expected agent with create_agent grant to be CoS")
	}
	specialistSpawner := &Agent{Name: "Reggie2", LocalTools: []string{"spawn_specialist"}}
	if !AgentIsCoS(specialistSpawner) {
		t.Errorf("expected agent with spawn_specialist grant to be CoS")
	}
	nonCoS := &Agent{Name: "Winston", LocalTools: []string{"bash", "*"}}
	if AgentIsCoS(nonCoS) {
		t.Errorf("wildcard/other tools must not imply CoS status")
	}
	if AgentIsCoS(nil) {
		t.Errorf("nil agent must not be CoS")
	}
}

func TestAppendAvailableModels_OnlyForCoS(t *testing.T) {
	block := "Available models (for spawn_specialist model choice):\n- claude-opus-4-6: $5.00/$25.00"
	cos := &Agent{Name: "Reggie", LocalTools: []string{"create_agent"}}
	out := AppendAvailableModels("BASE", cos, block)
	if out == "BASE" {
		t.Errorf("expected models block appended for CoS")
	}
	if !strings.Contains(out, "BASE") || !strings.Contains(out, block) {
		t.Errorf("expected both base and block present, got %q", out)
	}

	notCoS := &Agent{Name: "Sam", LocalTools: []string{"bash"}}
	out2 := AppendAvailableModels("BASE", notCoS, block)
	if out2 != "BASE" {
		t.Errorf("expected no-op for non-CoS agent, got %q", out2)
	}
}

func TestAppendAvailableModels_EmptyBlockIsNoop(t *testing.T) {
	cos := &Agent{Name: "Reggie", LocalTools: []string{"create_agent"}}
	out := AppendAvailableModels("BASE", cos, "")
	if out != "BASE" {
		t.Errorf("expected no-op for empty block, got %q", out)
	}
}
