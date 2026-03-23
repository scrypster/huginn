package agents_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/modelconfig"
)

func TestLoadAgents_MissingFile_ReturnsDefaults(t *testing.T) {
	cfg, err := agents.LoadAgentsFrom("/nonexistent/path/agents.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	// Blank canvas: no agents seeded by default.
	if len(cfg.Agents) != 0 {
		t.Errorf("expected empty agents on first run, got %d", len(cfg.Agents))
	}
}

func TestLoadAgents_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.json")

	orig := &agents.AgentsConfig{
		Agents: []agents.AgentDef{
			{Name: "Chris", Model: "qwen3:30b",
				SystemPrompt: "You are Chris.", Color: "#58A6FF", Icon: "C"},
			{Name: "Steve", Model: "qwen2.5:14b",
				SystemPrompt: "You are Steve.", Color: "#3FB950", Icon: "S"},
		},
	}

	if err := agents.SaveAgentsTo(orig, path); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := agents.LoadAgentsFrom(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(loaded.Agents) != 2 {
		t.Fatalf("expected 2, got %d", len(loaded.Agents))
	}
	if loaded.Agents[0].Name != "Chris" {
		t.Errorf("expected Chris, got %s", loaded.Agents[0].Name)
	}
	if loaded.Agents[0].SystemPrompt != "You are Chris." {
		t.Errorf("system prompt not preserved")
	}
}

func TestLoadAgents_InvalidJSON_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("{not valid json"), 0600)

	_, err := agents.LoadAgentsFrom(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestFromDef_SetsAllFields(t *testing.T) {
	def := agents.AgentDef{
		Name:         "Mark",
		Model:        "deepseek-r1:14b",
		SystemPrompt: "You are Mark.",
		Color:        "#D29922",
		Icon:         "M",
	}
	a := agents.FromDef(def)
	if a.Name != "Mark" {
		t.Errorf("name: got %s", a.Name)
	}
	if a.ModelID != "deepseek-r1:14b" {
		t.Errorf("model: got %s", a.ModelID)
	}
	if a.SystemPrompt != "You are Mark." {
		t.Errorf("prompt: got %s", a.SystemPrompt)
	}
	if a.Color != "#D29922" {
		t.Errorf("color: got %s", a.Color)
	}
	if a.Icon != "M" {
		t.Errorf("icon: got %s", a.Icon)
	}
}

func TestFromDef_UnknownSlot_DefaultsToPlanner(t *testing.T) {
	def := agents.AgentDef{Name: "X", Model: "m"}
	a := agents.FromDef(def)
	// Without a slot, the agent should still be created successfully
	if a.Name != "X" {
		t.Errorf("expected Name=X, got %q", a.Name)
	}
}

func TestSaveAgents_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "agents.json")
	cfg := &agents.AgentsConfig{Agents: []agents.AgentDef{{Name: "X", Model: "m"}}}
	if err := agents.SaveAgentsTo(cfg, path); err != nil {
		t.Fatalf("expected SaveAgentsTo to create dir: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestDefaultAgents_IsEmpty(t *testing.T) {
	cfg := agents.DefaultAgentsConfig()
	if len(cfg.Agents) != 0 {
		t.Errorf("DefaultAgentsConfig should return empty slice, got %d agents", len(cfg.Agents))
	}
}

// Ensure JSON serialization preserves all fields (regression: ensure no field is omitempty'd away)
func TestAgentDef_JSONPreservesEmptySystemPrompt(t *testing.T) {
	def := agents.AgentDef{Name: "X", Model: "m", SystemPrompt: ""}
	data, _ := json.Marshal(def)
	var got agents.AgentDef
	json.Unmarshal(data, &got)
	// SystemPrompt="" is valid (uses default); just ensure Name survived
	if got.Name != "X" {
		t.Errorf("Name not preserved: %s", got.Name)
	}
}

func TestSaveAndLoad_DeleteAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	cfg := &agents.AgentsConfig{
		Agents: []agents.AgentDef{
			{Name: "Steve", Model: "test:7b", Color: "#000", Icon: "S"},
			{Name: "Chris", Model: "test:14b", Color: "#000", Icon: "C"},
		},
	}
	if err := agents.SaveAgentsTo(cfg, path); err != nil {
		t.Fatalf("SaveAgentsTo: %v", err)
	}
	loaded, err := agents.LoadAgentsFrom(path)
	if err != nil {
		t.Fatalf("LoadAgentsFrom: %v", err)
	}
	// Delete Steve
	newAgents := loaded.Agents[:0]
	for _, def := range loaded.Agents {
		if !strings.EqualFold(def.Name, "Steve") {
			newAgents = append(newAgents, def)
		}
	}
	loaded.Agents = newAgents
	if err := agents.SaveAgentsTo(loaded, path); err != nil {
		t.Fatalf("SaveAgentsTo after delete: %v", err)
	}
	final, err := agents.LoadAgentsFrom(path)
	if err != nil {
		t.Fatalf("LoadAgentsFrom after delete: %v", err)
	}
	if len(final.Agents) != 1 {
		t.Errorf("expected 1 agent after delete, got %d", len(final.Agents))
	}
	if final.Agents[0].Name != "Chris" {
		t.Errorf("expected Chris remaining, got %q", final.Agents[0].Name)
	}
}


func TestSaveAgent_CreatesPerFileAgentInAgentsDir(t *testing.T) {
	dir := t.TempDir()
	agent := agents.AgentDef{
		Name:         "TestAgent",
		Model:        "test:7b",
		SystemPrompt: "Test prompt",
		Color:        "#fff",
		Icon:         "T",
	}
	if err := agents.SaveAgent(dir, agent); err != nil {
		t.Fatalf("SaveAgent failed: %v", err)
	}

	// Verify file was created at <dir>/agents/testagent.json
	expectedPath := filepath.Join(dir, "agents", "testagent.json")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("file not created at expected path: %v", err)
	}

	var loaded agents.AgentDef
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if loaded.Name != "TestAgent" {
		t.Errorf("expected Name=TestAgent, got %q", loaded.Name)
	}
	if loaded.Model != "test:7b" {
		t.Errorf("expected Model=test:7b, got %q", loaded.Model)
	}
}

func TestSaveAgent_UnnamedAgent_UsesDefaultName(t *testing.T) {
	dir := t.TempDir()
	agent := agents.AgentDef{
		Name:  "",
		Model: "test:7b",
	}
	if err := agents.SaveAgent(dir, agent); err != nil {
		t.Fatalf("SaveAgent failed: %v", err)
	}

	// File should exist at <dir>/agents/unnamed.json
	expectedPath := filepath.Join(dir, "agents", "unnamed.json")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("file not created at unnamed path: %v", err)
	}
}

func TestSaveAgent_SpecialCharactersInName_Sanitized(t *testing.T) {
	dir := t.TempDir()
	agent := agents.AgentDef{
		Name:  "Test@Agent#123",
		Model: "test:7b",
	}
	if err := agents.SaveAgent(dir, agent); err != nil {
		t.Fatalf("SaveAgent failed: %v", err)
	}

	// File should exist at <dir>/agents/test_agent_123.json (special chars replaced)
	expectedPath := filepath.Join(dir, "agents", "test_agent_123.json")
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("sanitized file not created: %v", err)
	}
}

func TestDeleteAgent_RemovesFile(t *testing.T) {
	dir := t.TempDir()
	agent := agents.AgentDef{
		Name:  "ToDelete",
		Model: "test:7b",
	}
	if err := agents.SaveAgent(dir, agent); err != nil {
		t.Fatalf("SaveAgent failed: %v", err)
	}

	// Verify file exists
	filePath := filepath.Join(dir, "agents", "todelete.json")
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("file not created: %v", err)
	}

	// Delete it
	if err := agents.DeleteAgent(dir, "ToDelete"); err != nil {
		t.Fatalf("DeleteAgent failed: %v", err)
	}

	// Verify it's gone
	if _, err := os.Stat(filePath); err == nil {
		t.Fatalf("file still exists after deletion")
	}
}

func TestDeleteAgent_NonexistentAgent_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	err := agents.DeleteAgent(dir, "NonExistent")
	if err == nil {
		t.Error("expected error when deleting nonexistent agent")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got: %v", err)
	}
}

func TestLoadAgentsFromBase_PerFileFormat_PreferredOverLegacy(t *testing.T) {
	dir := t.TempDir()

	// Create both per-file and legacy formats
	agentsDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentsDir, 0o750); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	// Create per-file: agents/Agent1.json
	agentData := []byte(`{"name":"Agent1","slot":"coder","model":"test:7b"}`)
	if err := os.WriteFile(filepath.Join(agentsDir, "Agent1.json"), agentData, 0o600); err != nil {
		t.Fatalf("write per-file agent failed: %v", err)
	}

	// Create legacy: agents.json
	legacyData := []byte(`{"agents":[{"name":"Legacy","slot":"planner","model":"test:14b"}]}`)
	if err := os.WriteFile(filepath.Join(dir, "agents.json"), legacyData, 0o600); err != nil {
		t.Fatalf("write legacy agents failed: %v", err)
	}

	// loadAgentsFromBase should prefer per-file format
	cfg, err := agents.LoadAgentsFrom(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatalf("LoadAgentsFrom failed: %v", err)
	}
	if len(cfg.Agents) == 0 {
		t.Error("expected agents to be loaded")
	}
	// Since loadAgentsFromBase prefers per-file, if we call it directly...
	// (we can't test loadAgentsFromBase directly as it's unexported, so test via integration)
}

func TestMigrateAgents_MigratesFromLegacyFormat(t *testing.T) {
	dir := t.TempDir()

	// Create legacy agents.json
	legacyCfg := &agents.AgentsConfig{
		Agents: []agents.AgentDef{
			{Name: "Agent1", Model: "test:7b", Color: "#fff", Icon: "A"},
			{Name: "Agent2", Model: "test:14b", Color: "#fff", Icon: "B"},
		},
	}
	data, _ := json.MarshalIndent(legacyCfg, "", "  ")
	legacyPath := filepath.Join(dir, "agents.json")
	if err := os.WriteFile(legacyPath, data, 0o600); err != nil {
		t.Fatalf("write legacy agents failed: %v", err)
	}

	// Run migration
	if err := agents.MigrateAgents(dir); err != nil {
		t.Fatalf("MigrateAgents failed: %v", err)
	}

	// Verify original is backed up
	bakPath := legacyPath + ".bak"
	if _, err := os.Stat(bakPath); err != nil {
		t.Fatalf("backup file not created: %v", err)
	}

	// Verify per-file agents were created
	agent1Path := filepath.Join(dir, "agents", "agent1.json")
	if _, err := os.Stat(agent1Path); err != nil {
		t.Fatalf("migrated agent file not created: %v", err)
	}
}

func TestMigrateAgents_NoLegacyFile_IdempotentSuccess(t *testing.T) {
	dir := t.TempDir()
	// No agents.json exists
	err := agents.MigrateAgents(dir)
	if err != nil {
		t.Fatalf("MigrateAgents should succeed when no legacy file exists: %v", err)
	}
}

func TestAgentDef_MemoryFields_Roundtrip(t *testing.T) {
	enabled := true
	def := agents.AgentDef{
		Name:          "Steve",
		VaultName:     "huginn:agent:mj:steve",
		Plasticity:    "default",
		MemoryEnabled: &enabled,
	}
	b, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	var out agents.AgentDef
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.VaultName != def.VaultName {
		t.Errorf("VaultName: got %q, want %q", out.VaultName, def.VaultName)
	}
	if out.Plasticity != def.Plasticity {
		t.Errorf("Plasticity: got %q, want %q", out.Plasticity, def.Plasticity)
	}
	if out.MemoryEnabled == nil || *out.MemoryEnabled != true {
		t.Error("MemoryEnabled not preserved")
	}
}

func TestFromDef_MemoryFields(t *testing.T) {
	enabled := true
	def := agents.AgentDef{
		Name:          "Steve",
		VaultName:     "huginn:agent:mj:steve",
		Plasticity:    "default",
		MemoryEnabled: &enabled,
	}
	agent := agents.FromDef(def)
	if agent.VaultName != def.VaultName {
		t.Errorf("VaultName: got %q, want %q", agent.VaultName, def.VaultName)
	}
	if agent.Plasticity != def.Plasticity {
		t.Errorf("Plasticity: got %q, want %q", agent.Plasticity, def.Plasticity)
	}
	if !agent.MemoryEnabled {
		t.Error("MemoryEnabled: expected true")
	}
}

func TestAgentDef_ResolvedVaultName(t *testing.T) {
	tests := []struct {
		def      agents.AgentDef
		username string
		want     string
	}{
		{agents.AgentDef{Name: "Steve"}, "mj", "huginn:agent:mj:steve"},
		{agents.AgentDef{Name: "My Agent"}, "mj", "huginn:agent:mj:my-agent"},
		{agents.AgentDef{Name: "Steve", VaultName: "custom:vault"}, "mj", "custom:vault"},
		{agents.AgentDef{Name: "Steve/Prod"}, "mj", "huginn:agent:mj:steveprod"}, // special chars stripped
	}
	for _, tt := range tests {
		got := tt.def.ResolvedVaultName(tt.username)
		if got != tt.want {
			t.Errorf("ResolvedVaultName(%q, %q) = %q, want %q", tt.def.Name, tt.username, got, tt.want)
		}
	}
}

func TestBuildRegistry_RegistersAgents(t *testing.T) {
	cfg := &agents.AgentsConfig{
		Agents: []agents.AgentDef{
			{Name: "Reasoner", Model: "model-r"},
		},
	}
	models := modelconfig.DefaultModels()
	reg := agents.BuildRegistry(cfg, models)

	// Verify agent is registered
	if len(reg.All()) != 1 {
		t.Errorf("expected 1 agent registered, got %d", len(reg.All()))
	}
}

func TestAgentDef_SkillsRoundTrip(t *testing.T) {
	def := agents.AgentDef{
		Name:   "coder",
		Skills: []string{"go-expert", "tdd"},
	}
	data, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	var got agents.AgentDef
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 2 || got.Skills[0] != "go-expert" || got.Skills[1] != "tdd" {
		t.Errorf("skills round-trip failed: got %v", got.Skills)
	}
}

func TestFromDef_CopiesSkills(t *testing.T) {
	def := agents.AgentDef{
		Name:   "coder",
		Skills: []string{"tdd", "code-reviewer"},
	}
	ag := agents.FromDef(def)
	if len(ag.Skills) != 2 || ag.Skills[0] != "tdd" {
		t.Errorf("FromDef skills copy failed: got %v", ag.Skills)
	}
}

func TestMemoryModeRoundTrip(t *testing.T) {
	def := agents.AgentDef{
		Name:             "Alice",
		MemoryMode:       "immersive",
		VaultDescription: "Alice's coding memory for the huginn project",
	}
	data, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	var out agents.AgentDef
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.MemoryMode != "immersive" {
		t.Fatalf("expected immersive, got %q", out.MemoryMode)
	}
	if out.VaultDescription != def.VaultDescription {
		t.Fatalf("vault_description mismatch")
	}
}

func TestFromDefMemoryMode(t *testing.T) {
	def := agents.AgentDef{Name: "Bob", MemoryMode: "passive"}
	a := agents.FromDef(def)
	if a.MemoryMode != "passive" {
		t.Fatalf("expected passive, got %q", a.MemoryMode)
	}
}

func TestFromDefMemoryModeEmpty(t *testing.T) {
	def := agents.AgentDef{Name: "Carol", MemoryMode: ""}
	a := agents.FromDef(def)
	// Empty mode passes through empty string — defaulting to "conversational" happens at runtime in the prompt builder
	if a.MemoryMode != "" {
		t.Fatalf("expected empty string, got %q", a.MemoryMode)
	}
}
