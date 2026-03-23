package tui

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/skills"
)

func newAppWithAgents() *App {
	a := newMinimalApp()
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Chris", ModelID: "qwen2.5-coder:14b"})
	reg.Register(&agents.Agent{Name: "Steve", ModelID: "deepseek-r1:14b"})
	reg.Register(&agents.Agent{Name: "Mark", ModelID: "llama3:8b"})
	a.agentReg = reg
	return a
}

func TestAgentsCommand_ShowsRoster(t *testing.T) {
	a := newAppWithAgents()
	out := a.handleAgentsCommand("")
	if !strings.Contains(out, "Chris") || !strings.Contains(out, "Steve") || !strings.Contains(out, "Mark") {
		t.Errorf("roster should show all agents, got: %s", out)
	}
}

func TestAgentsCommand_SwapUpdatesModel(t *testing.T) {
	a := newAppWithAgents()
	msg := a.handleAgentsCommand("swap Chris new-model:7b")
	if !strings.Contains(msg, "Chris") || !strings.Contains(msg, "new-model:7b") {
		t.Errorf("swap should confirm change, got: %s", msg)
	}
	// Verify the agent's model was actually changed
	ag, _ := a.agentReg.ByName("Chris")
	if ag.GetModelID() != "new-model:7b" {
		t.Errorf("expected new-model:7b, got %s", ag.GetModelID())
	}
}

func TestAgentsCommand_SwapUnknownAgent(t *testing.T) {
	a := newAppWithAgents()
	msg := a.handleAgentsCommand("swap Nobody new-model:7b")
	if !strings.Contains(strings.ToLower(msg), "unknown") && !strings.Contains(strings.ToLower(msg), "not found") {
		t.Errorf("should error for unknown agent, got: %s", msg)
	}
}

func TestAgentsCommand_RenameUpdatesName(t *testing.T) {
	a := newAppWithAgents()
	msg := a.handleAgentsCommand("rename Chris Alex")
	if !strings.Contains(msg, "Alex") {
		t.Errorf("rename should confirm new name, got: %s", msg)
	}
}

func TestAgentsCommand_PersonaShowsPrompt(t *testing.T) {
	a := newAppWithAgents()
	ag, _ := a.agentReg.ByName("Chris")
	ag.SystemPrompt = "You are Chris, a meticulous architect."
	msg := a.handleAgentsCommand("persona Chris")
	if !strings.Contains(msg, "meticulous architect") {
		t.Errorf("persona should show system prompt, got: %s", msg)
	}
}

func TestAgentsCommand_NilRegistry(t *testing.T) {
	a := newMinimalApp()
	// No agent registry
	msg := a.handleAgentsCommand("")
	if msg == "" {
		t.Error("expected informational message when no registry")
	}
}

func TestWizardHasAgentsCommand(t *testing.T) {
	reg := skills.NewSkillRegistry()
	if errs := reg.LoadBuiltins(); len(errs) > 0 {
		t.Fatalf("LoadBuiltins: %v", errs)
	}
	w := newWizardModel()
	w.SetRegistry(reg)
	w.Show("")

	found := false
	for _, cmd := range w.filtered {
		if cmd.Name == "agents" {
			found = true
		}
	}
	if !found {
		t.Error("wizard should include 'agents' command from built-in skills registry")
	}
}
