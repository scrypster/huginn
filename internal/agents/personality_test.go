package agents

import (
	"strings"
	"testing"
)

// TestPersonalityAddendum_MapsEveryPreset fails without the feature: before
// PersonalityAddendum existed every preset would return "" (or the function
// wouldn't compile), so this test pins down that each non-default preset has
// distinct, non-empty behavioral text.
func TestPersonalityAddendum_MapsEveryPreset(t *testing.T) {
	presets := []string{
		PersonalityStrictReviewer,
		PersonalityFastBuilder,
		PersonalitySkepticalArchitect,
		PersonalityTerseOperator,
	}
	seen := map[string]bool{}
	for _, p := range presets {
		add := PersonalityAddendum(p)
		if add == "" {
			t.Errorf("PersonalityAddendum(%q) returned empty string", p)
		}
		if seen[add] {
			t.Errorf("PersonalityAddendum(%q) duplicates another preset's text", p)
		}
		seen[add] = true
	}
}

func TestPersonalityAddendum_DefaultAndEmptyAreNoOverlay(t *testing.T) {
	if got := PersonalityAddendum(""); got != "" {
		t.Errorf("PersonalityAddendum(\"\") = %q, want empty", got)
	}
	if got := PersonalityAddendum(PersonalityDefault); got != "" {
		t.Errorf("PersonalityAddendum(default) = %q, want empty", got)
	}
	if got := PersonalityAddendum("not-a-real-preset"); got != "" {
		t.Errorf("PersonalityAddendum(unknown) = %q, want empty", got)
	}
}

func TestStrictReviewerAddendum_DemandsVerificationLanguage(t *testing.T) {
	add := PersonalityAddendum(PersonalityStrictReviewer)
	if !containsAll(add, "never", "done") {
		t.Errorf("strict-reviewer addendum missing verification-before-done discipline: %q", add)
	}
}

func TestSkepticalArchitectAddendum_DemandsChallenge(t *testing.T) {
	add := PersonalityAddendum(PersonalitySkepticalArchitect)
	if !containsAll(add, "risk", "evidence") && !containsAll(add, "framing") {
		t.Errorf("skeptical-architect addendum missing challenge-the-framing instruction: %q", add)
	}
}

func TestTerseOperatorAddendum_CapsLength(t *testing.T) {
	add := PersonalityAddendum(PersonalityTerseOperator)
	if !containsAll(add, "short") {
		t.Errorf("terse-operator addendum missing brevity instruction: %q", add)
	}
}

func TestValidPersonality(t *testing.T) {
	valid := []string{"", PersonalityDefault, PersonalityStrictReviewer, PersonalityFastBuilder, PersonalitySkepticalArchitect, PersonalityTerseOperator}
	for _, v := range valid {
		if !ValidPersonality(v) {
			t.Errorf("ValidPersonality(%q) = false, want true", v)
		}
	}
	if ValidPersonality("bogus") {
		t.Errorf("ValidPersonality(bogus) = true, want false")
	}
}

// TestResolveVetWork_StrictReviewerDefaultsOn fails without the feature:
// prior to ResolveVetWork existing, there was no mechanism binding
// strict-reviewer to vet_work=true by default.
func TestResolveVetWork_StrictReviewerDefaultsOn(t *testing.T) {
	if got := ResolveVetWork(PersonalityStrictReviewer, nil); !got {
		t.Errorf("ResolveVetWork(strict-reviewer, nil) = false, want true")
	}
}

func TestResolveVetWork_OtherPresetsDefaultOff(t *testing.T) {
	for _, p := range []string{"", PersonalityDefault, PersonalityFastBuilder, PersonalitySkepticalArchitect, PersonalityTerseOperator} {
		if got := ResolveVetWork(p, nil); got {
			t.Errorf("ResolveVetWork(%q, nil) = true, want false", p)
		}
	}
}

// TestResolveVetWork_ExplicitOverrideWins is the "model the override cleanly"
// requirement: a user's explicit choice must win over the preset default in
// both directions.
func TestResolveVetWork_ExplicitOverrideWins(t *testing.T) {
	trueVal, falseVal := true, false
	if got := ResolveVetWork(PersonalityStrictReviewer, &falseVal); got {
		t.Errorf("explicit false override on strict-reviewer should win: got true")
	}
	if got := ResolveVetWork(PersonalityFastBuilder, &trueVal); !got {
		t.Errorf("explicit true override on fast-builder should win: got false")
	}
}

// TestBuildPersonaPrompt_InjectsPersonalityAddendum fails without the
// feature: BuildPersonaPrompt previously never consulted ag.Personality at
// all, so a configured agent's persona prompt would not contain the preset's
// behavioral text.
func TestBuildPersonaPrompt_InjectsPersonalityAddendum(t *testing.T) {
	ag := &Agent{Name: "Reviewer", SystemPrompt: "You review code.", Personality: PersonalityStrictReviewer}
	prompt := BuildPersonaPrompt(ag, "")
	if !containsAll(prompt, "Strict Reviewer") {
		t.Errorf("BuildPersonaPrompt did not include strict-reviewer addendum:\n%s", prompt)
	}
}

func TestBuildPersonaPrompt_NoPersonalityNoOverlay(t *testing.T) {
	ag := &Agent{Name: "Plain", SystemPrompt: "You help."}
	prompt := BuildPersonaPrompt(ag, "")
	if containsAll(prompt, "## Personality") {
		t.Errorf("BuildPersonaPrompt injected a personality overlay for an agent with no preset:\n%s", prompt)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(strings.ToLower(s), strings.ToLower(sub)) {
			return false
		}
	}
	return true
}
