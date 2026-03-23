package skills

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/tools"
)

type stubSkill struct {
	name        string
	description string
	prompt      string
	rules       string
}

func (s *stubSkill) Name() string                { return s.name }
func (s *stubSkill) Description() string         { return s.description }
func (s *stubSkill) SystemPromptFragment() string { return s.prompt }
func (s *stubSkill) RuleContent() string          { return s.rules }
func (s *stubSkill) Tools() []tools.Tool          { return nil }

func TestSkillRegistry_RegisterAndAll(t *testing.T) {
	reg := NewSkillRegistry()
	reg.Register(&stubSkill{name: "a", prompt: "Prompt A", rules: "Rules A"})
	reg.Register(&stubSkill{name: "b", prompt: "Prompt B", rules: "Rules B"})
	all := reg.All()
	if len(all) != 2 {
		t.Fatalf("All() = %d skills, want 2", len(all))
	}
}

func TestSkillRegistry_CombinedPromptFragment(t *testing.T) {
	reg := NewSkillRegistry()
	reg.Register(&stubSkill{name: "a", prompt: "Fragment A"})
	reg.Register(&stubSkill{name: "b", prompt: "Fragment B"})
	combined := reg.CombinedPromptFragment()
	if !strings.Contains(combined, "Fragment A") {
		t.Errorf("CombinedPromptFragment missing Fragment A: %q", combined)
	}
	if !strings.Contains(combined, "Fragment B") {
		t.Errorf("CombinedPromptFragment missing Fragment B: %q", combined)
	}
}

func TestSkillRegistry_CombinedRuleContent(t *testing.T) {
	reg := NewSkillRegistry()
	reg.Register(&stubSkill{name: "a", rules: "Rule A"})
	reg.Register(&stubSkill{name: "b", rules: "Rule B"})
	combined := reg.CombinedRuleContent()
	if !strings.Contains(combined, "Rule A") {
		t.Errorf("CombinedRuleContent missing Rule A: %q", combined)
	}
	if !strings.Contains(combined, "Rule B") {
		t.Errorf("CombinedRuleContent missing Rule B: %q", combined)
	}
}

func TestSkillRegistry_Empty_ReturnsEmptyStrings(t *testing.T) {
	reg := NewSkillRegistry()
	if got := reg.CombinedPromptFragment(); got != "" {
		t.Errorf("CombinedPromptFragment() on empty registry = %q, want empty", got)
	}
	if got := reg.CombinedRuleContent(); got != "" {
		t.Errorf("CombinedRuleContent() on empty registry = %q, want empty", got)
	}
	if got := reg.All(); len(got) != 0 {
		t.Errorf("All() on empty registry = %d skills, want 0", len(got))
	}
}

func TestSkillRegistry_SkillWithEmptyFragments_NotIncluded(t *testing.T) {
	reg := NewSkillRegistry()
	reg.Register(&stubSkill{name: "no-content", prompt: "", rules: ""})
	reg.Register(&stubSkill{name: "has-content", prompt: "Has prompt", rules: "Has rules"})
	if got := reg.CombinedPromptFragment(); !strings.Contains(got, "Has prompt") {
		t.Errorf("CombinedPromptFragment() missing 'Has prompt': %q", got)
	}
	if got := reg.CombinedPromptFragment(); !strings.Contains(got, `<skill name="has-content">`) {
		t.Errorf("CombinedPromptFragment() missing skill delimiter: %q", got)
	}
	if got := reg.CombinedRuleContent(); got != "Has rules" {
		t.Errorf("CombinedRuleContent() = %q, want %q", got, "Has rules")
	}
}

func TestSkillRegistry_ConcurrentRegisterAndAll(t *testing.T) {
	reg := NewSkillRegistry()
	var wg sync.WaitGroup
	const goroutines = 20
	for i := 0; i < goroutines; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Use unique names so each skill is kept — Register replaces same-name skills.
			reg.Register(&stubSkill{name: fmt.Sprintf("skill-%d", i), prompt: "p", rules: "r"}) //nolint:errcheck
			_ = reg.All()
		}()
	}
	wg.Wait()
	all := reg.All()
	if len(all) != goroutines {
		t.Errorf("After %d concurrent registers, All() = %d, want %d", goroutines, len(all), goroutines)
	}
}

func TestSkillRegistry_LoadBuiltins_PopulatesRegistry(t *testing.T) {
	r := NewSkillRegistry()
	if errs := r.LoadBuiltins(); len(errs) > 0 {
		t.Fatalf("LoadBuiltins: %v", errs)
	}
	skills := r.All()
	if len(skills) == 0 {
		t.Fatal("expected at least one built-in skill after LoadBuiltins")
	}
	var found bool
	for _, s := range skills {
		if s.Name() == "plan" {
			found = true
			if s.Description() == "" {
				t.Error("plan skill has empty description")
			}
		}
	}
	if !found {
		t.Error("expected 'plan' skill in built-ins")
	}
}

func TestSkillRegistry_FindByName_UserSkillOverridesBuiltin(t *testing.T) {
	r := NewSkillRegistry()
	if errs := r.LoadBuiltins(); len(errs) > 0 {
		t.Fatalf("LoadBuiltins: %v", errs)
	}

	// Register a user skill with the same name as a built-in; last registered wins.
	userPlan := &stubSkill{name: "plan", description: "custom plan"}
	r.Register(userPlan)

	named := r.FindByName("plan")
	if named == nil {
		t.Fatal("FindByName returned nil")
	}
	if named.Description() != "custom plan" {
		t.Errorf("expected user skill to win override, got description %q", named.Description())
	}
}
