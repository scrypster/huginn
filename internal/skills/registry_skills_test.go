package skills

import (
	"log/slog"
	"strings"
	"testing"
)

// newTestRegistry builds a registry with "code" and "plan" skills.
func newTestRegistry() *SkillRegistry {
	r := NewSkillRegistry()
	r.Register(&stubSkill{name: "code", description: "", prompt: "## Code Skill\nWrite clean code.", rules: ""})
	r.Register(&stubSkill{name: "plan", description: "", prompt: "## Plan Skill\nThink before acting.", rules: "## Plan Rules\nAlways outline first."})
	return r
}

func TestFilteredSkillsFragment_NilReturnsGlobal(t *testing.T) {
	r := newTestRegistry()
	got := r.FilteredSkillsFragment(nil)
	if !strings.Contains(got, "Code Skill") {
		t.Errorf("nil: expected code skill in result, got: %q", got)
	}
	if !strings.Contains(got, "Plan Skill") {
		t.Errorf("nil: expected plan skill in result, got: %q", got)
	}
}

func TestFilteredSkillsFragment_EmptyReturnsEmpty(t *testing.T) {
	r := newTestRegistry()
	got := r.FilteredSkillsFragment([]string{})
	if got != "" {
		t.Errorf("empty list: expected empty string, got: %q", got)
	}
}

func TestFilteredSkillsFragment_FiltersByName(t *testing.T) {
	r := newTestRegistry()
	got := r.FilteredSkillsFragment([]string{"code"})
	if !strings.Contains(got, "Code Skill") {
		t.Errorf("expected code fragment, got: %q", got)
	}
	if strings.Contains(got, "Plan Skill") {
		t.Errorf("should NOT contain plan fragment, got: %q", got)
	}
}

func TestFilteredSkillsFragment_UnknownNameSkipped(t *testing.T) {
	r := newTestRegistry()
	got := r.FilteredSkillsFragment([]string{"ghost"})
	if got != "" {
		t.Errorf("unknown name: expected empty string, got: %q", got)
	}
}

func TestFilteredSkillsFragment_UnknownNameLogsWarning(t *testing.T) {
	var buf strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	r := newTestRegistry()
	r.FilteredSkillsFragment([]string{"ghost"})

	if !strings.Contains(buf.String(), "unknown skill in agent list") {
		t.Errorf("expected slog.Warn for unknown skill, log output: %q", buf.String())
	}
}

func TestFilteredSkillsFragment_MultipleNames(t *testing.T) {
	r := newTestRegistry()
	got := r.FilteredSkillsFragment([]string{"code", "plan"})
	if !strings.Contains(got, "Code Skill") {
		t.Errorf("expected code fragment, got: %q", got)
	}
	if !strings.Contains(got, "Plan Skill") {
		t.Errorf("expected plan fragment, got: %q", got)
	}
}

func TestFilteredSkillsFragment_DuplicateDeduped(t *testing.T) {
	r := newTestRegistry()
	got := r.FilteredSkillsFragment([]string{"code", "code"})
	count := strings.Count(got, "Code Skill")
	if count != 1 {
		t.Errorf("expected code fragment once, appeared %d times in: %q", count, got)
	}
}

func TestFilteredSkillsFragment_RulesOnlySkill(t *testing.T) {
	r := NewSkillRegistry()
	r.Register(&stubSkill{name: "rules-only", description: "", prompt: "", rules: "## Must do this"})
	got := r.FilteredSkillsFragment([]string{"rules-only"})
	if !strings.Contains(got, "Must do this") {
		t.Errorf("expected rules content injected, got: %q", got)
	}
}
