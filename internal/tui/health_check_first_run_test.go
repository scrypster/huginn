package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/config"
)

// ============================================================
// healthcheck.go — DefaultModel-based noBackend detection
// ============================================================

func TestHealthCheck_NoAgents_FirstRunIssue(t *testing.T) {
	app := newTestApp()
	app.agentReg = agents.NewRegistry() // empty
	cmd := app.runStartupHealthCheck()
	if cmd == nil {
		t.Fatal("expected non-nil cmd when no agents are registered")
	}
	msg := cmd()
	hcm, ok := msg.(healthCheckResultMsg)
	if !ok {
		t.Fatalf("expected healthCheckResultMsg, got %T", msg)
	}
	found := false
	for _, issue := range hcm.issues {
		if issue == "firstRun" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'firstRun' in issues, got %v", hcm.issues)
	}
}

func TestHealthCheck_AgentsPresent_NoModel_NoBackendIssue(t *testing.T) {
	app := newTestApp()
	app.cfg = &config.Config{DefaultModel: ""}
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Coder"})
	app.agentReg = reg

	cmd := app.runStartupHealthCheck()
	if cmd == nil {
		t.Fatal("expected non-nil cmd when model is empty")
	}
	msg := cmd()
	hcm, ok := msg.(healthCheckResultMsg)
	if !ok {
		t.Fatalf("expected healthCheckResultMsg, got %T", msg)
	}
	found := false
	for _, issue := range hcm.issues {
		if issue == "noBackend" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'noBackend' in issues, got %v", hcm.issues)
	}
}

func TestHealthCheck_AgentsPresent_ModelSet_NoIssues(t *testing.T) {
	app := newTestApp()
	app.cfg = &config.Config{DefaultModel: "qwen2.5-coder:14b"}
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Coder"})
	app.agentReg = reg

	cmd := app.runStartupHealthCheck()
	if cmd != nil {
		t.Error("expected nil cmd (no issues) when agents and model are configured")
	}
}

func TestHealthCheck_NilConfig_NoAgents_NoBackendNotEmitted(t *testing.T) {
	app := newTestApp()
	app.cfg = nil
	app.agentReg = agents.NewRegistry() // empty — firstRun masks noBackend

	cmd := app.runStartupHealthCheck()
	if cmd == nil {
		t.Fatal("expected cmd for nil config + no agents")
	}
	msg := cmd()
	hcm := msg.(healthCheckResultMsg)
	for _, issue := range hcm.issues {
		if issue == "noBackend" {
			t.Error("noBackend must not appear when firstRun masks it")
		}
	}
}

func TestHealthCheck_NilConfig_AgentsPresent_NoBackend(t *testing.T) {
	app := newTestApp()
	app.cfg = nil
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Planner"})
	app.agentReg = reg

	cmd := app.runStartupHealthCheck()
	if cmd == nil {
		t.Fatal("expected cmd when cfg is nil and agents exist")
	}
	msg := cmd()
	hcm := msg.(healthCheckResultMsg)
	found := false
	for _, issue := range hcm.issues {
		if issue == "noBackend" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'noBackend' when cfg is nil, got %v", hcm.issues)
	}
}

func TestHealthWarning_ContainsTitleAndDetail(t *testing.T) {
	result := healthWarning("No LLM provider", "Edit config.json")
	if !strings.Contains(result, "No LLM provider") {
		t.Errorf("expected title in healthWarning, got %q", result)
	}
	if !strings.Contains(result, "Edit config.json") {
		t.Errorf("expected detail in healthWarning, got %q", result)
	}
	if !strings.Contains(result, "⚠") {
		t.Errorf("expected warning icon in healthWarning, got %q", result)
	}
}

// ============================================================
// screen_models.go — single default_model row rendering
// ============================================================

func TestModelsScreen_RenderBody_DefaultModelSet(t *testing.T) {
	s := newModelsScreen()
	s.SetConfig(&config.Config{DefaultModel: "qwen2.5-coder:14b"})
	body := s.renderBody(80, 30)
	if !strings.Contains(body, "qwen2.5-coder:14b") {
		t.Errorf("expected model name in renderBody, got:\n%s", body)
	}
	if !strings.Contains(body, "default_model") {
		t.Errorf("expected 'default_model' label in renderBody, got:\n%s", body)
	}
}

func TestModelsScreen_RenderBody_DefaultModelEmpty(t *testing.T) {
	s := newModelsScreen()
	s.SetConfig(&config.Config{DefaultModel: ""})
	body := s.renderBody(80, 30)
	if !strings.Contains(body, "(not set)") {
		t.Errorf("expected '(not set)' when DefaultModel is empty, got:\n%s", body)
	}
}

func TestModelsScreen_RenderBody_NilConfig(t *testing.T) {
	s := newModelsScreen()
	// cfg is nil — should not panic
	body := s.renderBody(80, 30)
	if !strings.Contains(body, "not available") {
		t.Errorf("expected 'not available' for nil config, got:\n%s", body)
	}
}

func TestModelsScreen_NoSlotLabels(t *testing.T) {
	s := newModelsScreen()
	s.SetConfig(&config.Config{DefaultModel: "claude-3-5-sonnet"})
	body := s.renderBody(80, 30)
	lower := strings.ToLower(body)
	for _, slot := range []string{"coder_model", "planner_model", "reasoner_model"} {
		if strings.Contains(lower, slot) {
			t.Errorf("slot label %q must not appear in renderBody (removed)", slot)
		}
	}
}

func TestModelsScreen_View_ReturnsNonEmpty(t *testing.T) {
	s := newModelsScreen()
	s.SetConfig(&config.Config{DefaultModel: "test-model"})
	view := s.View(100, 40)
	if view == "" {
		t.Error("expected non-empty View output")
	}
	if !strings.Contains(view, "Models") {
		t.Errorf("expected 'Models' header in view, got:\n%s", view)
	}
}

func TestModelsScreen_EscKey_ReturnsBackToChatCmd(t *testing.T) {
	s := newModelsScreen()
	s.SetConfig(&config.Config{DefaultModel: "x"})
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected non-nil cmd on Esc")
	}
	msg := cmd()
	if _, ok := msg.(backToChatMsg); !ok {
		t.Errorf("expected backToChatMsg, got %T", msg)
	}
}

func TestModelsScreen_QKey_ReturnsBackToChatCmd(t *testing.T) {
	s := newModelsScreen()
	s.SetConfig(&config.Config{DefaultModel: "x"})
	_, cmd := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("expected non-nil cmd on 'q'")
	}
	msg := cmd()
	if _, ok := msg.(backToChatMsg); !ok {
		t.Errorf("expected backToChatMsg on 'q', got %T", msg)
	}
}
