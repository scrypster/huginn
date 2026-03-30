package agents_test

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

// TestBuildPersonaPrompt_CustomSystemPrompt_InjectsAgentName verifies that
// when an agent has both a Name and a custom SystemPrompt, the agent's name
// is prepended so the model knows what it is called.
//
// Regression: previously only the fallback path ("You are X, an expert...")
// included the name. When SystemPrompt was set, the name was silently omitted,
// causing the model to respond with its default identity (e.g. "I am Qwen")
// instead of the configured name (e.g. "I am Mike").
func TestBuildPersonaPrompt_CustomSystemPrompt_InjectsAgentName(t *testing.T) {
	ag := &agents.Agent{
		Name:         "Mike",
		SystemPrompt: "You are a developer.",
	}
	result := agents.BuildPersonaPrompt(ag, "some context")

	if !strings.Contains(result, "Mike") {
		t.Errorf("expected agent name 'Mike' in prompt, got:\n%s", result)
	}
	// The custom system prompt content must also be present.
	if !strings.Contains(result, "You are a developer.") {
		t.Errorf("expected custom system prompt in result, got:\n%s", result)
	}
	// Context must be present.
	if !strings.Contains(result, "some context") {
		t.Errorf("expected context in result, got:\n%s", result)
	}
	// Name must come before the system prompt.
	nameIdx := strings.Index(result, "Mike")
	promptIdx := strings.Index(result, "You are a developer.")
	if nameIdx > promptIdx {
		t.Errorf("expected agent name to appear before system prompt content")
	}
}

// TestBuildPersonaPrompt_EmptySystemPrompt_FallbackIncludesName verifies the
// existing fallback path still includes the agent name (unchanged behaviour).
func TestBuildPersonaPrompt_EmptySystemPrompt_FallbackIncludesName(t *testing.T) {
	ag := &agents.Agent{Name: "Alex"}
	result := agents.BuildPersonaPrompt(ag, "ctx")
	if !strings.Contains(result, "Alex") {
		t.Errorf("expected 'Alex' in fallback prompt, got:\n%s", result)
	}
}

// TestBuildPersonaPrompt_NoName_CustomPrompt_DoesNotPrependEmpty verifies that
// when the agent has no name set, no "Your name is ." garbage is prepended.
func TestBuildPersonaPrompt_NoName_CustomPrompt_DoesNotPrependEmpty(t *testing.T) {
	ag := &agents.Agent{SystemPrompt: "Be helpful."}
	result := agents.BuildPersonaPrompt(ag, "ctx")
	if strings.Contains(result, "Your name is") {
		t.Errorf("expected no 'Your name is' when Name is empty, got:\n%s", result)
	}
	if !strings.Contains(result, "Be helpful.") {
		t.Errorf("expected system prompt in result")
	}
}
