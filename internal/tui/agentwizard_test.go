package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestAgentWizard_NameValidation(t *testing.T) {
	// Valid names
	if !isValidAgentName("steve") {
		t.Error("expected 'steve' to be valid agent name")
	}
	if !isValidAgentName("my-agent-1") {
		t.Error("expected 'my-agent-1' to be valid agent name")
	}
	if !isValidAgentName("ab") {
		t.Error("expected 'ab' to be valid agent name")
	}
	// Invalid names
	if isValidAgentName("Steve") {
		t.Error("expected 'Steve' (uppercase) to be invalid")
	}
	if isValidAgentName("") {
		t.Error("expected '' to be invalid")
	}
	if isValidAgentName("1agent") {
		t.Error("expected '1agent' (starts with digit) to be invalid")
	}
	if isValidAgentName("a") {
		t.Error("expected 'a' (too short, needs at least 2 chars) to be invalid")
	}
}

func TestAgentWizard_StepProgression(t *testing.T) {
	wiz := newAgentWizardModel()
	if wiz.step != wizStepName {
		t.Errorf("expected initial step to be wizStepName, got %d", wiz.step)
	}

	// Simulate entering a valid name and pressing enter
	wiz.ti.SetValue("alice")
	m, _ := wiz.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := m.(agentWizardModel)

	if got.step != wizStepModel {
		t.Errorf("expected step wizStepModel after entering name, got %d", got.step)
	}
}

func TestAgentWizard_ViewRendersCurrentStep(t *testing.T) {
	wiz := newAgentWizardModel()
	view := wiz.View()
	if !strings.Contains(view, "name") && !strings.Contains(view, "Name") {
		t.Errorf("expected name step in initial view, got: %s", view)
	}
}

func TestAgentWizard_InvalidNameShowsError(t *testing.T) {
	wiz := newAgentWizardModel()
	wiz.ti.SetValue("INVALID")
	m, _ := wiz.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := m.(agentWizardModel)

	// Should stay on name step
	if got.step != wizStepName {
		t.Errorf("expected to stay on wizStepName after invalid name, got step %d", got.step)
	}
	if got.nameErr == "" {
		t.Error("expected nameErr to be set after invalid name")
	}
}

func TestAgentWizard_ModelSelection(t *testing.T) {
	wiz := newAgentWizardModel()
	wiz.step = wizStepModel
	wiz.nameInput = "alice"

	// Move cursor down once
	m, _ := wiz.Update(tea.KeyMsg{Type: tea.KeyDown})
	got := m.(agentWizardModel)
	if got.modelCursor != 1 {
		t.Errorf("expected modelCursor=1 after down, got %d", got.modelCursor)
	}

	// Select model with Enter
	m, _ = got.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got = m.(agentWizardModel)
	if got.step != wizStepBackstory {
		t.Errorf("expected wizStepBackstory after model selection, got %d", got.step)
	}
	if got.selectedModel != got.availModels[1] {
		t.Errorf("expected selectedModel=%s, got %s", got.availModels[1], got.selectedModel)
	}
}

func TestAgentWizard_EscCancels(t *testing.T) {
	wiz := newAgentWizardModel()
	var cancelReceived bool

	_, cmd := wiz.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected a cmd after Esc")
	}
	msg := cmd()
	if _, ok := msg.(AgentWizardCancelMsg); ok {
		cancelReceived = true
	}
	if !cancelReceived {
		t.Error("expected AgentWizardCancelMsg after Esc")
	}
}

func TestAgentWizard_ConfirmCreatesAgent(t *testing.T) {
	wiz := newAgentWizardModel()
	wiz.step = wizStepConfirm
	wiz.nameInput = "alice"
	wiz.selectedModel = "claude-sonnet-4-6"
	wiz.backstory = "A test agent"

	_, cmd := wiz.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a cmd after Enter on confirm step")
	}
	msg := cmd()
	done, ok := msg.(AgentWizardDoneMsg)
	if !ok {
		t.Fatalf("expected AgentWizardDoneMsg, got %T", msg)
	}
	if done.Agent.Name != "alice" {
		t.Errorf("expected agent name 'alice', got %q", done.Agent.Name)
	}
	if done.Agent.Model != "claude-sonnet-4-6" {
		t.Errorf("expected model 'claude-sonnet-4-6', got %q", done.Agent.Model)
	}
}

func TestAgentWizard_MemoryStep_AppearsAfterBackstory(t *testing.T) {
	wiz := newAgentWizardWithMemory("", false) // no endpoint = nudge state
	// Advance past name
	wiz.nameInput = "steve"
	wiz.step = wizStepModel
	wiz.selectedModel = "claude-sonnet-4-6"
	wiz.step = wizStepBackstory
	wiz.backstory = "A helpful agent"
	wiz.step = wizStepMemory

	view := wiz.View()
	if !strings.Contains(view, "Memory") {
		t.Errorf("expected Memory heading in view, got: %s", view)
	}
}

func TestAgentWizard_MemoryStep_NudgeWhenNotConfigured(t *testing.T) {
	wiz := newAgentWizardWithMemory("", false) // no endpoint
	wiz.nameInput = "steve"
	wiz.step = wizStepMemory

	view := wiz.View()
	if !strings.Contains(view, "muninndb.com") {
		t.Errorf("expected muninndb.com nudge, got: %s", view)
	}
}

func TestAgentWizard_MemoryStep_ShowsVaultNameWhenConfigured(t *testing.T) {
	wiz := newAgentWizardWithMemory("http://localhost:8475", true)
	wiz.nameInput = "steve"
	wiz.step = wizStepMemory

	view := wiz.View()
	if !strings.Contains(view, "huginn-steve") {
		t.Errorf("expected proposed vault name huginn-steve in view, got: %s", view)
	}
}
