package agents_test

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

// TestBuildPersonaPromptWithRoster_IncludesDelegationGuardrail verifies that the
// roster prompt includes guardrail text preventing delegation for trivial messages.
//
// Regression: without this, the model (especially qwen3-coder) misinterprets the
// delegation instruction and calls delegate_to_agent for simple greetings like "hello",
// producing confusing responses like "CodeWriter is not available".
func TestBuildPersonaPromptWithRoster_IncludesDelegationGuardrail(t *testing.T) {
	ag := &agents.Agent{Name: "Mike", SystemPrompt: "You are a developer."}
	roster := "- CodeWriter: writes code\n- CodeReviewer: reviews code"

	result := agents.BuildPersonaPromptWithRoster(ag, "ctx", roster)

	// Must include the roster.
	if !strings.Contains(result, roster) {
		t.Errorf("expected roster in result, got:\n%s", result)
	}

	// Must include the delegation instruction.
	if !strings.Contains(result, "delegate_to_agent") {
		t.Errorf("expected delegate_to_agent instruction in result, got:\n%s", result)
	}

	// Must include guardrail text preventing delegation for trivial messages.
	guardrailPhrases := []string{
		"Only delegate",
		"simple",
		"directly",
	}
	for _, phrase := range guardrailPhrases {
		if !strings.Contains(result, phrase) {
			t.Errorf("expected delegation guardrail phrase %q in result, got:\n%s", phrase, result)
		}
	}
}

// TestBuildPersonaPromptWithRoster_NoRoster_NoGuardrail verifies that when no
// roster is provided, the delegation guardrail is not injected (agent has no team).
func TestBuildPersonaPromptWithRoster_NoRoster_NoGuardrail(t *testing.T) {
	ag := &agents.Agent{Name: "Mike", SystemPrompt: "You are a developer."}

	result := agents.BuildPersonaPromptWithRoster(ag, "ctx", "")

	if strings.Contains(result, "delegate_to_agent") {
		t.Errorf("expected no delegation instruction when roster is empty, got:\n%s", result)
	}
}
