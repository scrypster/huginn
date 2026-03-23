// internal/skills/version_conflict_e2e_test.go
package skills_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/skills"
	"github.com/scrypster/huginn/internal/tools"
)

// versionedTestSkill is a minimal Skill + VersionedSkill implementation for tests.
type versionedTestSkill struct {
	name    string
	version string
	prompt  string
}

func (s *versionedTestSkill) Name() string                 { return s.name }
func (s *versionedTestSkill) Description() string          { return "" }
func (s *versionedTestSkill) SystemPromptFragment() string { return s.prompt }
func (s *versionedTestSkill) RuleContent() string          { return "" }
func (s *versionedTestSkill) Tools() []tools.Tool          { return nil }
func (s *versionedTestSkill) Version() string              { return s.version }

// Compile-time assertion: versionedTestSkill satisfies VersionedSkill.
var _ skills.VersionedSkill = (*versionedTestSkill)(nil)

// TestVersionConflict_SameVersionNoWarn registers two VersionedSkills with the
// same name AND the same version. Both registrations succeed and FindByName
// returns the last-registered skill.
func TestVersionConflict_SameVersionNoWarn(t *testing.T) {
	t.Parallel()

	reg := skills.NewSkillRegistry()

	s1 := &versionedTestSkill{name: "tool-x", version: "1.0.0", prompt: "prompt v1"}
	s2 := &versionedTestSkill{name: "tool-x", version: "1.0.0", prompt: "prompt v1 duplicate"}

	reg.Register(s1)
	reg.Register(s2)

	found := reg.FindByName("tool-x")
	if found == nil {
		t.Fatal("FindByName returned nil")
	}
	// Last registered wins.
	vs, ok := found.(skills.VersionedSkill)
	if !ok {
		t.Fatal("expected VersionedSkill, got plain Skill")
	}
	if vs.Version() != "1.0.0" {
		t.Errorf("want version 1.0.0, got %q", vs.Version())
	}
	if found.SystemPromptFragment() != "prompt v1 duplicate" {
		t.Errorf("expected last-registered prompt, got %q", found.SystemPromptFragment())
	}
}

// TestVersionConflict_DifferentVersionReplacesOld registers VersionedSkill v1
// then v2 with the same name. v2 replaces v1 (new version wins, no duplicates).
// FindByName returns the v2 skill and only 1 skill is in the registry.
func TestVersionConflict_DifferentVersionReplacesOld(t *testing.T) {
	t.Parallel()

	reg := skills.NewSkillRegistry()

	v1 := &versionedTestSkill{name: "analyzer", version: "1.0.0", prompt: "v1 prompt"}
	v2 := &versionedTestSkill{name: "analyzer", version: "2.0.0", prompt: "v2 prompt"}

	// Register v1 first, then v2 — v2 replaces v1 atomically.
	reg.Register(v1)
	reg.Register(v2)

	// Only one skill should be registered — the new version replaced the old.
	all := reg.All()
	if len(all) != 1 {
		t.Fatalf("want 1 registered skill (replacement), got %d", len(all))
	}

	found := reg.FindByName("analyzer")
	if found == nil {
		t.Fatal("FindByName returned nil")
	}
	vs, ok := found.(skills.VersionedSkill)
	if !ok {
		t.Fatal("expected VersionedSkill from FindByName")
	}
	if vs.Version() != "2.0.0" {
		t.Errorf("expected v2 to win (replacement), got version %q", vs.Version())
	}
	if found.SystemPromptFragment() != "v2 prompt" {
		t.Errorf("expected v2 prompt, got %q", found.SystemPromptFragment())
	}
}

// nonVersionedStub is a plain Skill without the VersionedSkill interface.
type nonVersionedStub struct {
	name   string
	prompt string
}

func (s *nonVersionedStub) Name() string                 { return s.name }
func (s *nonVersionedStub) Description() string          { return "" }
func (s *nonVersionedStub) SystemPromptFragment() string { return s.prompt }
func (s *nonVersionedStub) RuleContent() string          { return "" }
func (s *nonVersionedStub) Tools() []tools.Tool          { return nil }

// TestVersionConflict_NonVersionedSkillNoConflict registers two non-versioned
// skills with the same name. No version conflict logic is applied; last-wins.
func TestVersionConflict_NonVersionedSkillNoConflict(t *testing.T) {
	t.Parallel()

	reg := skills.NewSkillRegistry()

	a := &nonVersionedStub{name: "reporter", prompt: "first reporter"}
	b := &nonVersionedStub{name: "reporter", prompt: "second reporter"}

	reg.Register(a)
	reg.Register(b)

	found := reg.FindByName("reporter")
	if found == nil {
		t.Fatal("FindByName returned nil")
	}
	if found.SystemPromptFragment() != "second reporter" {
		t.Errorf("expected last-registered to win, got prompt %q", found.SystemPromptFragment())
	}
	// Confirm neither is VersionedSkill.
	if _, ok := found.(skills.VersionedSkill); ok {
		t.Error("non-versioned stub should NOT implement VersionedSkill")
	}
}

// TestVersionConflict_UserSkillOverridesBuiltin loads built-ins then registers
// a user skill with the same name as the first builtin. FindByName must return
// the user skill (last-registered wins).
func TestVersionConflict_UserSkillOverridesBuiltin(t *testing.T) {
	t.Parallel()

	reg := skills.NewSkillRegistry()
	if errs := reg.LoadBuiltins(); len(errs) > 0 {
		t.Fatalf("LoadBuiltins errors: %v", errs)
	}

	builtins := reg.All()
	if len(builtins) == 0 {
		t.Skip("no built-in skills available — skipping override test")
	}

	// Pick the first built-in's name to override.
	targetName := builtins[0].Name()

	// Register a user skill with the same name.
	userSkill := &nonVersionedStub{name: targetName, prompt: "user-override-prompt"}
	reg.Register(userSkill)

	found := reg.FindByName(targetName)
	if found == nil {
		t.Fatalf("FindByName(%q) returned nil after override", targetName)
	}
	if found.SystemPromptFragment() != "user-override-prompt" {
		t.Errorf("expected user skill to override builtin, got prompt %q", found.SystemPromptFragment())
	}
}
