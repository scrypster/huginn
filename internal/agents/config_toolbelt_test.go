package agents_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

func TestFromDef_CopiesToolbelt(t *testing.T) {
	def := agents.AgentDef{
		Name: "TestBot",
		Toolbelt: []agents.ToolbeltEntry{
			{ConnectionID: "c1", Provider: "github", ApprovalGate: true},
		},
	}
	ag := agents.FromDef(def)
	if len(ag.Toolbelt) != 1 {
		t.Fatalf("expected 1 toolbelt entry, got %d", len(ag.Toolbelt))
	}
	if ag.Toolbelt[0].ConnectionID != "c1" {
		t.Fatalf("unexpected connection_id: %s", ag.Toolbelt[0].ConnectionID)
	}
	if ag.Toolbelt[0].Provider != "github" {
		t.Fatalf("unexpected provider: %s", ag.Toolbelt[0].Provider)
	}
	if !ag.Toolbelt[0].ApprovalGate {
		t.Fatal("expected ApprovalGate to be true")
	}
}

func TestFromDef_EmptyToolbelt(t *testing.T) {
	def := agents.AgentDef{Name: "TestBot"}
	ag := agents.FromDef(def)
	if ag.Toolbelt != nil {
		t.Fatalf("expected nil toolbelt, got %v", ag.Toolbelt)
	}
}

func TestFromDef_MultipleToolbeltEntries(t *testing.T) {
	def := agents.AgentDef{
		Name: "TestBot",
		Toolbelt: []agents.ToolbeltEntry{
			{ConnectionID: "c1", Provider: "github", ApprovalGate: true},
			{ConnectionID: "c2", Provider: "slack", ApprovalGate: false},
			{ConnectionID: "c3", Provider: "jira", ApprovalGate: true},
		},
	}
	ag := agents.FromDef(def)
	if len(ag.Toolbelt) != 3 {
		t.Fatalf("expected 3 toolbelt entries, got %d", len(ag.Toolbelt))
	}
	// Verify each entry is correctly copied
	if ag.Toolbelt[0].ConnectionID != "c1" || ag.Toolbelt[0].Provider != "github" || !ag.Toolbelt[0].ApprovalGate {
		t.Error("first entry not correctly copied")
	}
	if ag.Toolbelt[1].ConnectionID != "c2" || ag.Toolbelt[1].Provider != "slack" || ag.Toolbelt[1].ApprovalGate {
		t.Error("second entry not correctly copied")
	}
	if ag.Toolbelt[2].ConnectionID != "c3" || ag.Toolbelt[2].Provider != "jira" || !ag.Toolbelt[2].ApprovalGate {
		t.Error("third entry not correctly copied")
	}
}
