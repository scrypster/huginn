package agents_test

import (
	"encoding/json"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

// TestFromDef_NilSkillsPreservesNil verifies that an AgentDef with nil Skills
// produces a runtime Agent with nil Skills (global fallback semantics).
func TestFromDef_NilSkillsPreservesNil(t *testing.T) {
	def := agents.AgentDef{Name: "test", Skills: nil}
	ag := agents.FromDef(def)
	if ag.Skills != nil {
		t.Errorf("nil Skills in def should stay nil in agent, got: %v", ag.Skills)
	}
}

// TestFromDef_EmptySkillsPreservesEmpty verifies that an AgentDef with []string{}
// produces a runtime Agent with a non-nil, zero-length Skills slice.
func TestFromDef_EmptySkillsPreservesEmpty(t *testing.T) {
	def := agents.AgentDef{Name: "test", Skills: []string{}}
	ag := agents.FromDef(def)
	if ag.Skills == nil {
		t.Error("[]string{} Skills in def must not become nil in agent")
	}
	if len(ag.Skills) != 0 {
		t.Errorf("expected len 0, got %d", len(ag.Skills))
	}
}

// TestSkillsJSON_AbsentFieldDeserializesNil verifies that JSON without a "skills"
// key deserializes to nil (triggering global fallback).
func TestSkillsJSON_AbsentFieldDeserializesNil(t *testing.T) {
	data := []byte(`{"name":"test","slot":"planner"}`)
	var def agents.AgentDef
	if err := json.Unmarshal(data, &def); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if def.Skills != nil {
		t.Errorf("absent 'skills' field must deserialize to nil, got: %v", def.Skills)
	}
}

// TestSkillsJSON_EmptyArrayDeserializesEmpty verifies that JSON "skills": []
// deserializes to a non-nil, zero-length slice (explicit no-skills).
func TestSkillsJSON_EmptyArrayDeserializesEmpty(t *testing.T) {
	data := []byte(`{"name":"test","slot":"planner","skills":[]}`)
	var def agents.AgentDef
	if err := json.Unmarshal(data, &def); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if def.Skills == nil {
		t.Error("'skills': [] must deserialize to non-nil empty slice")
	}
	if len(def.Skills) != 0 {
		t.Errorf("expected len 0, got %d", len(def.Skills))
	}
}

// TestSkillsJSON_RoundTripPreservesEmpty verifies that marshaling a def with
// Skills: []string{} and unmarshaling the result preserves the non-nil empty slice.
func TestSkillsJSON_RoundTripPreservesEmpty(t *testing.T) {
	def := agents.AgentDef{Name: "test", Skills: []string{}}
	data, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var roundTripped agents.AgentDef
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if roundTripped.Skills == nil {
		t.Errorf("empty Skills should survive round-trip as non-nil; marshaled to: %s", data)
	}
}
