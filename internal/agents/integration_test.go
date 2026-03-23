package agents_test

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/modelconfig"
)

func TestBuildRegistry_SyncsReasonerModel(t *testing.T) {
	cfg := &agents.AgentsConfig{
		Agents: []agents.AgentDef{
			{Name: "Chris", Model: "custom-reason-model"},
		},
	}
	models := modelconfig.DefaultModels()
	reg := agents.BuildRegistry(cfg, models)

	// Registry should have the agent.
	if _, ok := reg.ByName("Chris"); !ok {
		t.Error("Chris not in registry")
	}
}

func TestAgentPersonaInjected_InSystemPrompt(t *testing.T) {
	def := agents.AgentDef{
		Name:         "Chris",
		Model:        "m",
		SystemPrompt: "You are Chris, the architect.",
	}
	ag := agents.FromDef(def)
	ctxBlock := "## Codebase Context\n(files here)"

	prompt := agents.BuildPersonaPrompt(ag, ctxBlock)
	if !strings.Contains(prompt, "You are Chris, the architect.") {
		t.Errorf("persona not in prompt: %s", prompt)
	}
	if !strings.Contains(prompt, "Codebase Context") {
		t.Errorf("context not in prompt: %s", prompt)
	}
}
