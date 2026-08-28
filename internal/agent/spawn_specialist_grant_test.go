package agent

import (
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/tools"
)

// TestApplyToolbelt_SpawnSpecialistNeverImpliedByWildcard verifies S11/step
// 4b: local_tools: ["*"] (God Mode) must NOT grant spawn_specialist, mirroring
// the existing create_agent exclusion — only an explicit named grant does.
func TestApplyToolbelt_SpawnSpecialistNeverImpliedByWildcard(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&tools.SpawnSpecialistTool{})
	reg.Register(&tools.CreateAgentTool{})

	ag := &agents.Agent{Name: "Winston", LocalTools: []string{"*"}}
	gate := permissions.NewGate(true, nil)
	schemas, _ := applyToolbelt(ag, reg, gate)

	for _, s := range schemas {
		if s.Function.Name == tools.SpawnSpecialistName {
			t.Fatalf("God Mode wildcard must not imply spawn_specialist, got schemas: %+v", schemas)
		}
		if s.Function.Name == tools.CreateAgentName {
			t.Fatalf("God Mode wildcard must not imply create_agent, got schemas: %+v", schemas)
		}
	}
}

// TestApplyToolbelt_SpawnSpecialistGrantedByName verifies the explicit named
// grant does keep the schema (CoS convention).
func TestApplyToolbelt_SpawnSpecialistGrantedByName(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&tools.SpawnSpecialistTool{})

	ag := &agents.Agent{Name: "Reggie", LocalTools: []string{"spawn_specialist"}}
	gate := permissions.NewGate(true, nil)
	schemas, _ := applyToolbelt(ag, reg, gate)

	found := false
	for _, s := range schemas {
		if s.Function.Name == tools.SpawnSpecialistName {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected spawn_specialist schema present for named grant, got schemas: %+v", schemas)
	}
}

// TestApplyToolbelt_BothHiringToolsExcludedIndependently verifies granting
// only one of create_agent/spawn_specialist does not leak the other.
func TestApplyToolbelt_BothHiringToolsExcludedIndependently(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&tools.SpawnSpecialistTool{})
	reg.Register(&tools.CreateAgentTool{})

	ag := &agents.Agent{Name: "Reggie", LocalTools: []string{"create_agent"}}
	gate := permissions.NewGate(true, nil)
	schemas, _ := applyToolbelt(ag, reg, gate)

	hasCreate, hasSpawn := false, false
	for _, s := range schemas {
		if s.Function.Name == tools.CreateAgentName {
			hasCreate = true
		}
		if s.Function.Name == tools.SpawnSpecialistName {
			hasSpawn = true
		}
	}
	if !hasCreate {
		t.Error("expected create_agent present for its own named grant")
	}
	if hasSpawn {
		t.Error("expected spawn_specialist absent when only create_agent is granted")
	}
}
