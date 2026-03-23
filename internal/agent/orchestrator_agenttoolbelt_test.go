package agent

import (
	"testing"

	"github.com/scrypster/huginn/internal/agent/session"
	"github.com/scrypster/huginn/internal/agents"
)

func TestAgentToolbelt_NilReturnsNil(t *testing.T) {
	ag := &agents.Agent{
		Toolbelt: nil,
	}
	result := agentToolbelt(ag)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestAgentToolbelt_EmptyReturnsNil(t *testing.T) {
	ag := &agents.Agent{
		Toolbelt: []agents.ToolbeltEntry{},
	}
	result := agentToolbelt(ag)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestAgentToolbelt_SingleEntryWithProfile(t *testing.T) {
	ag := &agents.Agent{
		Toolbelt: []agents.ToolbeltEntry{
			{Provider: "aws", Profile: "prod"},
		},
	}
	result := agentToolbelt(ag)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}
	if result[0].Provider != "aws" {
		t.Errorf("expected provider 'aws', got %q", result[0].Provider)
	}
	if result[0].Profile != "prod" {
		t.Errorf("expected profile 'prod', got %q", result[0].Profile)
	}
}

func TestAgentToolbelt_MultipleEntries(t *testing.T) {
	ag := &agents.Agent{
		Toolbelt: []agents.ToolbeltEntry{
			{Provider: "aws", Profile: "prod", ApprovalGate: true},
			{Provider: "gcloud", Profile: "staging"},
		},
	}
	result := agentToolbelt(ag)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}

	// Verify first entry
	if result[0].Provider != "aws" {
		t.Errorf("entry 0: expected provider 'aws', got %q", result[0].Provider)
	}
	if result[0].Profile != "prod" {
		t.Errorf("entry 0: expected profile 'prod', got %q", result[0].Profile)
	}
	// Verify ApprovalGate is NOT copied (session.ToolbeltEntry has no such field)
	if result[0] != (session.ToolbeltEntry{Provider: "aws", Profile: "prod"}) {
		t.Errorf("entry 0: unexpected fields in converted entry: %+v", result[0])
	}

	// Verify second entry
	if result[1].Provider != "gcloud" {
		t.Errorf("entry 1: expected provider 'gcloud', got %q", result[1].Provider)
	}
	if result[1].Profile != "staging" {
		t.Errorf("entry 1: expected profile 'staging', got %q", result[1].Profile)
	}
	if result[1] != (session.ToolbeltEntry{Provider: "gcloud", Profile: "staging"}) {
		t.Errorf("entry 1: unexpected fields in converted entry: %+v", result[1])
	}
}
