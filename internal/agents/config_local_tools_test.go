package agents_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

func TestFromDef_LocalToolsPassedThrough(t *testing.T) {
	def := agents.AgentDef{
		Name:       "tester",
		LocalTools: []string{"read_file", "git_status"},
	}
	ag := agents.FromDef(def)
	if len(ag.LocalTools) != 2 {
		t.Fatalf("expected 2 LocalTools, got %d", len(ag.LocalTools))
	}
	if ag.LocalTools[0] != "read_file" {
		t.Errorf("expected read_file, got %q", ag.LocalTools[0])
	}
}

func TestFromDef_LocalToolsNilByDefault(t *testing.T) {
	def := agents.AgentDef{Name: "tester"}
	ag := agents.FromDef(def)
	if ag.LocalTools != nil {
		t.Errorf("expected nil LocalTools, got %v", ag.LocalTools)
	}
}

func TestFromDef_LocalToolsWildcard(t *testing.T) {
	def := agents.AgentDef{
		Name:       "tester",
		LocalTools: []string{"*"},
	}
	ag := agents.FromDef(def)
	if len(ag.LocalTools) != 1 || ag.LocalTools[0] != "*" {
		t.Errorf("expected [\"*\"], got %v", ag.LocalTools)
	}
}
