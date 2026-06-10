package agents

import (
	"testing"
)

// TestReloadFromConfig_InPlace verifies the registry contents are replaced
// in place — adds, updates, and removals are all reflected through the same
// pointer that long-lived components captured at boot (issue #124).
func TestReloadFromConfig_InPlace(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&Agent{Name: "Old", ModelID: "m1"})
	reg.Register(&Agent{Name: "Keep", ModelID: "m1"})

	cfg := &AgentsConfig{Agents: []AgentDef{
		{Name: "Keep", Model: "m2"},
		{Name: "New", Model: "m1"},
	}}
	reg.ReloadFromConfig(cfg, "tester")

	if _, ok := reg.ByName("Old"); ok {
		t.Error("removed agent must no longer resolve after reload")
	}
	if _, ok := reg.ByName("New"); !ok {
		t.Error("newly added agent must resolve after reload")
	}
	kept, ok := reg.ByName("keep") // case-insensitive
	if !ok {
		t.Fatal("kept agent must still resolve after reload")
	}
	if kept.GetModelID() != "m2" {
		t.Errorf("kept agent must reflect updated model, got %q", kept.GetModelID())
	}
	if kept.VaultName == "" {
		t.Error("reload must resolve the effective vault name like boot-time build")
	}
	if names := reg.Names(); len(names) != 2 {
		t.Errorf("expected exactly 2 agents after reload, got %v", names)
	}
}

// TestReloadFromConfig_MatchesBuild verifies that a reload produces the same
// agents as a fresh BuildRegistryWithUsername over the same config.
func TestReloadFromConfig_MatchesBuild(t *testing.T) {
	cfg := &AgentsConfig{Agents: []AgentDef{
		{Name: "Alpha", Model: "m1", Plasticity: "reference"},
		{Name: "Beta", Model: "m2"},
	}}

	built := BuildRegistryWithUsername(cfg, nil, "tester")
	reloaded := NewRegistry()
	reloaded.Register(&Agent{Name: "Stale", ModelID: "x"})
	reloaded.ReloadFromConfig(cfg, "tester")

	for _, name := range []string{"Alpha", "Beta"} {
		b, _ := built.ByName(name)
		r, ok := reloaded.ByName(name)
		if !ok {
			t.Fatalf("agent %s missing after reload", name)
		}
		if r.GetModelID() != b.GetModelID() || r.VaultName != b.VaultName || r.Plasticity != b.Plasticity {
			t.Errorf("agent %s differs between build and reload: %+v vs %+v", name, r, b)
		}
	}
}
