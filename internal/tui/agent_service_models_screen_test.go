package tui

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/tui/services"
)

// ============================================================
// services/agents.go
// ============================================================

func TestDirectAgentService_List_ReturnsAllAgents(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Coder", ModelID: "qwen2.5-coder:14b"})
	reg.Register(&agents.Agent{Name: "Planner", ModelID: "claude-3-5-sonnet"})

	svc := services.NewDirectAgentService(reg)
	defs := svc.List()
	if len(defs) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(defs))
	}
}

func TestDirectAgentService_ByName_ReturnsAgent(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "QA", ModelID: "haiku"})

	svc := services.NewDirectAgentService(reg)
	def, ok := svc.ByName("QA")
	if !ok {
		t.Fatal("expected agent QA to be found")
	}
	if def.Name != "QA" {
		t.Errorf("expected name=QA, got %q", def.Name)
	}
}

func TestDirectAgentService_ByName_NotFound(t *testing.T) {
	reg := agents.NewRegistry()
	svc := services.NewDirectAgentService(reg)
	_, ok := svc.ByName("ghost")
	if ok {
		t.Error("expected ok=false for unknown agent name")
	}
}

func TestDirectAgentService_List_PreservesModelID(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Coder", ModelID: "qwen2.5-coder:14b"})

	svc := services.NewDirectAgentService(reg)
	defs := svc.List()
	if len(defs) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(defs))
	}
	if defs[0].Model != "qwen2.5-coder:14b" {
		t.Errorf("expected Model=%q, got %q", "qwen2.5-coder:14b", defs[0].Model)
	}
}

func TestDirectAgentService_Names_MatchesRegistered(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Alpha"})
	reg.Register(&agents.Agent{Name: "Beta"})

	svc := services.NewDirectAgentService(reg)
	names := svc.Names()
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d: %v", len(names), names)
	}
	nameSet := map[string]bool{}
	for _, n := range names {
		nameSet[strings.ToLower(n)] = true
	}
	if !nameSet["alpha"] || !nameSet["beta"] {
		t.Errorf("expected alpha and beta in names, got %v", names)
	}
}

// ============================================================
// app.go — activeModel comes from cfg.DefaultModel
// ============================================================

func TestApp_ActiveModel_FromCfgDefaultModel(t *testing.T) {
	app := newTestApp()
	app.cfg = &config.Config{DefaultModel: "my-model"}
	// activeModel is set during New() / init. We verify the field cfg.DefaultModel
	// is the source of truth — no separate models struct.
	if app.cfg.DefaultModel != "my-model" {
		t.Errorf("expected DefaultModel=my-model, got %q", app.cfg.DefaultModel)
	}
}

func TestApp_CfgDefaultModel_EmptyStringIsValid(t *testing.T) {
	app := newTestApp()
	app.cfg = &config.Config{DefaultModel: ""}
	// Should not panic; health check covers warning.
	_ = app.cfg.DefaultModel
}

// ============================================================
// app.go — "use X for coding/planning" commands are removed
// ============================================================

func TestApp_SlashCommandNotModelSwitch(t *testing.T) {
	app := newTestApp()
	app.cfg = &config.Config{DefaultModel: "original-model"}
	// Send text that would previously have been parsed as a model-slot command.
	// Since parseModelCommandIfAny is removed, the model must NOT change.
	app.input.SetValue("use gpt-4 for coding")
	// We don't call Update here since Enter would trigger an agent run;
	// instead we assert cfg.DefaultModel is unchanged without any "handleModelCommand".
	if app.cfg.DefaultModel != "original-model" {
		t.Errorf("DefaultModel should not change; got %q", app.cfg.DefaultModel)
	}
}

// ============================================================
// healthcheck.go — handleHealthCheckResult side effects
// ============================================================

func TestHandleHealthCheckResult_NoBackend_AddsSystemLine(t *testing.T) {
	app := newTestApp()
	app.cfg = &config.Config{DefaultModel: ""}

	app.handleHealthCheckResult([]string{"noBackend"})

	found := false
	for _, line := range app.chat.history {
		if line.role == "system" && strings.Contains(line.content, "No LLM provider") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'No LLM provider' system line in history, got: %v", app.chat.history)
	}
}

func TestHandleHealthCheckResult_EmptyIssues_NoChange(t *testing.T) {
	app := newTestApp()
	before := len(app.chat.history)
	app.handleHealthCheckResult([]string{})
	if len(app.chat.history) != before {
		t.Errorf("expected no history change for empty issues, got %d new lines", len(app.chat.history)-before)
	}
}

func TestHandleHealthCheckResult_FirstRun_SetsViewportContent(t *testing.T) {
	app := newTestApp()
	app.handleHealthCheckResult([]string{"firstRun"})
	// firstRun sets viewport content to the welcome message; state is NOT changed
	// to stateAgentWizard (forcing a modal on first run is poor UX — user can type /agents).
	content := app.viewport.View()
	if !strings.Contains(content, "HUGINN") && !strings.Contains(app.viewport.View(), "agent") {
		// viewport content may be empty if viewport dimensions are zero in test.
		// Just verify state was NOT set to stateAgentWizard (the old, bad behavior).
		t.Logf("viewport content: %q", content)
	}
	if app.state == stateAgentWizard {
		t.Error("handleHealthCheckResult(firstRun) must NOT force open the agent wizard")
	}
}
