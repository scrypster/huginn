package agent

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/skills"
	"github.com/scrypster/huginn/internal/tools"
)

// stubSkill satisfies skills.Skill for orchestrator-level tests.
type stubSkill struct {
	name   string
	prompt string
	rules  string
}

func (s *stubSkill) Name() string                 { return s.name }
func (s *stubSkill) Version() string              { return "1.0" }
func (s *stubSkill) Description() string          { return "" }
func (s *stubSkill) SystemPromptFragment() string { return s.prompt }
func (s *stubSkill) RuleContent() string          { return s.rules }
func (s *stubSkill) Tools() []tools.Tool          { return nil }

// newTestSkillsReg builds a skills registry with "code" and "plan".
func newTestSkillsReg() *skills.SkillRegistry {
	r := skills.NewSkillRegistry()
	r.Register(&stubSkill{name: "code", prompt: "## Code Skill"})
	r.Register(&stubSkill{name: "plan", prompt: "## Plan Skill"})
	return r
}

// agentRegWith builds an AgentRegistry containing a single default agent
// with the given skills list.
func agentRegWith(skillList []string) *agents.AgentRegistry {
	reg := agents.NewRegistry()
	ag := &agents.Agent{
		Name:      "test-agent",
		IsDefault: true,
		Skills:    skillList,
	}
	reg.Register(ag)
	return reg
}

func TestSkillsFragmentFor_NilUsesGlobal(t *testing.T) {
	o := newBareOrchestrator()
	o.skillsReg = newTestSkillsReg()

	got := o.skillsFragmentFor(agentRegWith(nil))
	if !strings.Contains(got, "Code Skill") {
		t.Errorf("nil skills: expected code skill in prompt, got: %q", got)
	}
	if !strings.Contains(got, "Plan Skill") {
		t.Errorf("nil skills: expected plan skill in prompt, got: %q", got)
	}
}

func TestSkillsFragmentFor_EmptyGetsNothing(t *testing.T) {
	o := newBareOrchestrator()
	o.skillsReg = newTestSkillsReg()

	got := o.skillsFragmentFor(agentRegWith([]string{}))
	if got != "" {
		t.Errorf("empty skills list: expected empty fragment, got: %q", got)
	}
}

func TestSkillsFragmentFor_FilteredByList(t *testing.T) {
	o := newBareOrchestrator()
	o.skillsReg = newTestSkillsReg()

	got := o.skillsFragmentFor(agentRegWith([]string{"code"}))
	if !strings.Contains(got, "Code Skill") {
		t.Errorf("expected code skill, got: %q", got)
	}
	if strings.Contains(got, "Plan Skill") {
		t.Errorf("should NOT contain plan skill, got: %q", got)
	}
}

func TestSkillsFragmentFor_NilRegistryReturnsEmpty(t *testing.T) {
	o := newBareOrchestrator()
	// skillsReg is nil by default in bare orchestrator

	got := o.skillsFragmentFor(agentRegWith(nil))
	if got != "" {
		t.Errorf("nil registry: expected empty string, got: %q", got)
	}
}

func TestSkillsFragmentFor_NoDefaultAgent_UsesGlobal(t *testing.T) {
	o := newBareOrchestrator()
	o.skillsReg = newTestSkillsReg()

	// Empty agent registry — no default agent
	emptyReg := agents.NewRegistry()
	got := o.skillsFragmentFor(emptyReg)
	// No default agent → agentSkills stays nil → global fallback
	if !strings.Contains(got, "Code Skill") {
		t.Errorf("no default agent: expected global fallback, got: %q", got)
	}
}
