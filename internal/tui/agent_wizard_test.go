package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/scrypster/huginn/internal/agents"
)

// TestAgentWizardStandalone_Init checks the standalone wrapper starts in the name step.
func TestAgentWizardStandalone_Init(t *testing.T) {
	m := NewStandaloneAgentWizard()
	if m.inner.step != wizStepName {
		t.Errorf("initial step: got %d, want wizStepName (0)", m.inner.step)
	}
	if m.IsDone() {
		t.Error("new wizard should not be done")
	}
	if m.WasSaved() {
		t.Error("new wizard should not be saved")
	}
}

// TestAgentWizardStandalone_DoneMsg tests that AgentWizardDoneMsg triggers quit + saves state.
func TestAgentWizardStandalone_DoneMsg(t *testing.T) {
	m := NewStandaloneAgentWizard()
	agent := agents.AgentDef{
		Name:         "testbot",
		Model:        "model-a",
		SystemPrompt: "You are a test bot.",
	}
	result, cmd := m.Update(AgentWizardDoneMsg{Agent: agent})
	if cmd == nil {
		t.Fatal("expected a cmd (tea.Quit) after AgentWizardDoneMsg")
	}
	wizard := result.(StandaloneAgentWizard)
	if !wizard.IsDone() {
		t.Error("expected IsDone=true after AgentWizardDoneMsg")
	}
	if !wizard.WasSaved() {
		t.Error("expected WasSaved=true after AgentWizardDoneMsg")
	}
	if wizard.SavedAgent().Name != "testbot" {
		t.Errorf("agent name: got %q, want %q", wizard.SavedAgent().Name, "testbot")
	}
}

// TestAgentWizardStandalone_CancelMsg tests that AgentWizardCancelMsg triggers quit + not saved.
func TestAgentWizardStandalone_CancelMsg(t *testing.T) {
	m := NewStandaloneAgentWizard()
	result, cmd := m.Update(AgentWizardCancelMsg{})
	if cmd == nil {
		t.Fatal("expected a cmd (tea.Quit) after AgentWizardCancelMsg")
	}
	wizard := result.(StandaloneAgentWizard)
	if !wizard.IsDone() {
		t.Error("expected IsDone=true after AgentWizardCancelMsg")
	}
	if wizard.WasSaved() {
		t.Error("expected WasSaved=false after cancel")
	}
}

// TestAgentWizardStandalone_EscSendsCancel checks that Esc key triggers AgentWizardCancelMsg.
func TestAgentWizardStandalone_EscSendsCancel(t *testing.T) {
	m := NewStandaloneAgentWizard()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected a cmd after Esc")
	}
	msg := cmd()
	if _, ok := msg.(AgentWizardCancelMsg); !ok {
		t.Errorf("expected AgentWizardCancelMsg from Esc, got %T", msg)
	}
}

// TestAgentWizardStandalone_ViewNonEmpty checks View() returns non-empty for the initial step.
func TestAgentWizardStandalone_ViewNonEmpty(t *testing.T) {
	m := NewStandaloneAgentWizard()
	view := m.View()
	if view == "" {
		t.Error("View() should not be empty")
	}
}

// TestAgentWizardStandalone_NameStepProgression checks entering a valid name advances to model step.
func TestAgentWizardStandalone_NameStepProgression(t *testing.T) {
	m := NewStandaloneAgentWizard()
	// Set a valid name via textinput
	m.inner.ti.SetValue("steve")
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wizard := result.(StandaloneAgentWizard)
	if wizard.inner.step != wizStepModel {
		t.Errorf("after valid name + enter: got step %d, want wizStepModel (%d)", wizard.inner.step, wizStepModel)
	}
	if wizard.inner.nameInput != "steve" {
		t.Errorf("nameInput not set: %q", wizard.inner.nameInput)
	}
}

// TestAgentWizardStandalone_InvalidNameStays checks an invalid name keeps the wizard on the name step.
func TestAgentWizardStandalone_InvalidNameStays(t *testing.T) {
	m := NewStandaloneAgentWizard()
	m.inner.ti.SetValue("INVALID")
	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wizard := result.(StandaloneAgentWizard)
	if wizard.inner.step != wizStepName {
		t.Errorf("invalid name should stay on wizStepName, got %d", wizard.inner.step)
	}
	if wizard.inner.nameErr == "" {
		t.Error("expected nameErr to be set after invalid name")
	}
}

// TestAgentWizardStandalone_ModelCursorNavigation checks up/down navigation on the model step.
func TestAgentWizardStandalone_ModelCursorNavigation(t *testing.T) {
	m := NewStandaloneAgentWizard()
	m.inner.step = wizStepModel
	m.inner.nameInput = "testbot"

	result, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	wizard := result.(StandaloneAgentWizard)
	if wizard.inner.modelCursor != 1 {
		t.Errorf("after down: cursor=%d, want 1", wizard.inner.modelCursor)
	}

	result, _ = wizard.Update(tea.KeyMsg{Type: tea.KeyEnter})
	wizard = result.(StandaloneAgentWizard)
	if wizard.inner.step != wizStepBackstory {
		t.Errorf("after model select: got step %d, want wizStepBackstory (%d)", wizard.inner.step, wizStepBackstory)
	}
}

// TestAgentWizardStandalone_ConfirmCreatesAgentMsg checks confirm step sends AgentWizardDoneMsg.
func TestAgentWizardStandalone_ConfirmCreatesAgentMsg(t *testing.T) {
	m := NewStandaloneAgentWizard()
	m.inner.step = wizStepConfirm
	m.inner.nameInput = "testbot"
	m.inner.selectedModel = "model-a"
	m.inner.backstory = "You are a test bot."

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a cmd after Enter on confirm step")
	}
	msg := cmd()
	done, ok := msg.(AgentWizardDoneMsg)
	if !ok {
		t.Fatalf("expected AgentWizardDoneMsg, got %T", msg)
	}
	if done.Agent.Name != "testbot" {
		t.Errorf("agent name: %q", done.Agent.Name)
	}
	if done.Agent.Model != "model-a" {
		t.Errorf("agent model: %q", done.Agent.Model)
	}
}

// TestIsValidAgentName_Cases exercises the isValidAgentName function with various inputs.
func TestIsValidAgentName_Cases(t *testing.T) {
	cases := []struct {
		name  string
		valid bool
	}{
		{"steve", true},
		{"code-reviewer", true},
		{"agent_1", true},
		{"ab", true},
		{"", false},
		{"a", false},         // too short (min 2 chars per regex)
		{"1invalid", false},  // starts with digit
		{"Steve", false},     // uppercase not allowed by regex
		{"valid name", false}, // space not allowed
	}
	for _, c := range cases {
		got := isValidAgentName(c.name)
		if got != c.valid {
			t.Errorf("isValidAgentName(%q) = %v, want %v", c.name, got, c.valid)
		}
	}
}
