package agent

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/skills"
	"github.com/scrypster/huginn/internal/tools"
)

// fakeSkill is a minimal skills.Skill double sufficient to drive
// SkillRegistry.FilteredSkillsFragment without dragging in real skill files.
type fakeSkill struct {
	name   string
	prompt string
	rules  string
}

func (f *fakeSkill) Name() string                 { return f.name }
func (f *fakeSkill) Description() string          { return "" }
func (f *fakeSkill) SystemPromptFragment() string { return f.prompt }
func (f *fakeSkill) RuleContent() string          { return f.rules }
func (f *fakeSkill) Tools() []tools.Tool          { return nil }
func (f *fakeSkill) Path() string                 { return "" }

// TestSkillsFragmentForAgent_PerAgentResolution verifies that the per-agent
// fragment helper looks at the *agent's* Skills field rather than the
// orchestrator's default-agent value. This is the core Phase 1.2 contract:
// each agent participating in a workflow must get its own assigned skills,
// not whatever the registry's default agent happens to be configured with.
func TestSkillsFragmentForAgent_PerAgentResolution(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)

	reg := skills.NewSkillRegistry()
	reg.Register(&fakeSkill{name: "alpha", prompt: "ALPHA-PROMPT"})
	reg.Register(&fakeSkill{name: "beta", prompt: "BETA-PROMPT"})
	reg.Register(&fakeSkill{name: "gamma", prompt: "GAMMA-PROMPT"})
	o.SetSkillsRegistry(reg)

	specialist := &agents.Agent{Name: "specialist", Skills: []string{"beta"}}
	other := &agents.Agent{Name: "other", Skills: []string{"alpha", "gamma"}}

	got := o.SkillsFragmentForAgent(specialist)
	if !strings.Contains(got, "BETA-PROMPT") {
		t.Errorf("specialist fragment missing BETA-PROMPT:\n%s", got)
	}
	if strings.Contains(got, "ALPHA-PROMPT") || strings.Contains(got, "GAMMA-PROMPT") {
		t.Errorf("specialist fragment leaked other agents' skills:\n%s", got)
	}

	got = o.SkillsFragmentForAgent(other)
	if !strings.Contains(got, "ALPHA-PROMPT") || !strings.Contains(got, "GAMMA-PROMPT") {
		t.Errorf("other fragment missing alpha/gamma:\n%s", got)
	}
	if strings.Contains(got, "BETA-PROMPT") {
		t.Errorf("other fragment leaked beta:\n%s", got)
	}
}

// TestSkillsFragmentForAgent_NoRegistry returns empty when no skills registry
// is configured. This guards against the obvious nil-deref bug where the
// helper assumes the registry is always set.
func TestSkillsFragmentForAgent_NoRegistry(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
	if got := o.SkillsFragmentForAgent(&agents.Agent{Name: "x", Skills: []string{"y"}}); got != "" {
		t.Errorf("expected empty fragment without registry; got %q", got)
	}
}

// TestSkillsFragmentForAgent_NilAgent must not panic and must fall through to
// the global-fallback semantics (nil Skills field → CombinedPromptFragment).
// A workflow runner that hands us a nil agent — e.g. unresolved agent name —
// is a programmer bug, but we still want a graceful return value rather than
// a panic that crashes the run.
func TestSkillsFragmentForAgent_NilAgent(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
	reg := skills.NewSkillRegistry()
	reg.Register(&fakeSkill{name: "global", prompt: "GLOBAL-PROMPT"})
	o.SetSkillsRegistry(reg)

	got := o.SkillsFragmentForAgent(nil)
	if !strings.Contains(got, "GLOBAL-PROMPT") {
		t.Errorf("nil agent should fall back to global skills, got %q", got)
	}
}
