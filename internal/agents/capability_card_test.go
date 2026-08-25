// internal/agents/capability_card_test.go
package agents_test

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/modelconfig"
)

func testInfoFn(modelID string) *modelconfig.ModelInfo {
	switch modelID {
	case "capable-model":
		return &modelconfig.ModelInfo{Tier: modelconfig.TierHigh, SupportsTools: true}
	case "medium-model":
		return &modelconfig.ModelInfo{Tier: modelconfig.TierMedium, SupportsTools: true}
	case "low-model":
		return &modelconfig.ModelInfo{Tier: modelconfig.TierLow, SupportsTools: false}
	}
	return nil
}

func TestBuildCapabilityCard_FullCard(t *testing.T) {
	in := agents.CapabilityCardInput{
		Name:         "Ares",
		SystemPrompt: "You are Ares, a security-focused monitoring agent.",
		ModelID:      "capable-model",
		LocalTools:   []string{"filesystem", "web_search"},
		Toolbelt: []agents.ToolbeltEntry{
			{Provider: "github", ConnectionID: "conn1"},
		},
		Skills:     []string{"git-monitor", "pr-reviewer"},
		MemoryMode: "conversational",
	}
	card := agents.BuildCapabilityCard(in, testInfoFn)

	if !strings.Contains(card, "- Ares [capable, tools: yes]") {
		t.Errorf("expected header with tier annotation, got:\n%s", card)
	}
	if !strings.Contains(card, "Role: a security-focused monitoring agent") {
		t.Errorf("expected role line, got:\n%s", card)
	}
	if !strings.Contains(card, "Tools: filesystem, web_search") {
		t.Errorf("expected tools line, got:\n%s", card)
	}
	if !strings.Contains(card, "Connections: GitHub") {
		t.Errorf("expected connections line, got:\n%s", card)
	}
	if !strings.Contains(card, "Skills: git-monitor, pr-reviewer") {
		t.Errorf("expected skills line, got:\n%s", card)
	}
	if !strings.Contains(card, "Memory: conversational") {
		t.Errorf("expected memory line, got:\n%s", card)
	}
}

func TestBuildCapabilityCard_NoInfoFn(t *testing.T) {
	in := agents.CapabilityCardInput{
		Name:         "Sam",
		SystemPrompt: "You are Sam, a QA specialist.",
		ModelID:      "some-model",
	}
	card := agents.BuildCapabilityCard(in, nil)

	// No tier annotation when infoFn is nil
	if strings.Contains(card, "[") {
		t.Errorf("expected no tier annotation when infoFn is nil, got:\n%s", card)
	}
	if !strings.Contains(card, "- Sam\n") {
		t.Errorf("expected plain name header, got:\n%s", card)
	}
}

func TestBuildCapabilityCard_DescriptionOverride(t *testing.T) {
	in := agents.CapabilityCardInput{
		Name:         "Nova",
		SystemPrompt: "You are Nova, the general assistant with a long persona that should be overridden.",
		Description:  "Personal assistant for scheduling and research",
		MemoryMode:   "passive",
	}
	card := agents.BuildCapabilityCard(in, nil)

	if !strings.Contains(card, "Role: Personal assistant for scheduling and research") {
		t.Errorf("expected description override as role, got:\n%s", card)
	}
	if strings.Contains(card, "the general assistant") {
		t.Errorf("system prompt should be overridden, got:\n%s", card)
	}
}

func TestBuildCapabilityCard_EmptyOptionals(t *testing.T) {
	in := agents.CapabilityCardInput{
		Name:         "Ghost",
		SystemPrompt: "You are Ghost.",
	}
	card := agents.BuildCapabilityCard(in, nil)

	if strings.Contains(card, "Tools:") {
		t.Errorf("empty local tools should not emit Tools line, got:\n%s", card)
	}
	if strings.Contains(card, "Connections:") {
		t.Errorf("empty toolbelt should not emit Connections line, got:\n%s", card)
	}
	if strings.Contains(card, "Skills:") {
		t.Errorf("empty skills should not emit Skills line, got:\n%s", card)
	}
	// Memory defaults to conversational when empty
	if !strings.Contains(card, "Memory: conversational") {
		t.Errorf("expected default memory mode, got:\n%s", card)
	}
}

func TestBuildCapabilityCard_LowTierNoTools(t *testing.T) {
	in := agents.CapabilityCardInput{
		Name:    "Cheap",
		ModelID: "low-model",
	}
	card := agents.BuildCapabilityCard(in, testInfoFn)

	if !strings.Contains(card, "[low, tools: no]") {
		t.Errorf("expected low tier no-tools annotation, got:\n%s", card)
	}
}

func TestExtractRoleBlurb(t *testing.T) {
	tests := []struct {
		name     string
		prompt   string
		override string
		want     string
	}{
		{"override wins", "You are Steve, a coder.", "Codes and reviews PRs", "Codes and reviews PRs"},
		{"strips name prefix", "You are Steve, a coder. Use tools.", "", "a coder"},
		{"first sentence only", "Reviews pull requests for regressions. Then files issues.", "", "Reviews pull requests for regressions"},
		{"empty prompt", "", "", ""},
		{"whitespace prompt", "   ", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := agents.ExtractRoleBlurb(tt.prompt, tt.override)
			if got != tt.want {
				t.Fatalf("ExtractRoleBlurb(%q, %q) = %q, want %q", tt.prompt, tt.override, got, tt.want)
			}
		})
	}
}

func TestBuildCapabilityCard_SystemPromptTruncation(t *testing.T) {
	long := "You are Verbose, " + strings.Repeat("a", 300)
	in := agents.CapabilityCardInput{
		Name:         "Verbose",
		SystemPrompt: long,
	}
	card := agents.BuildCapabilityCard(in, nil)

	if strings.Contains(card, strings.Repeat("a", 201)) {
		t.Errorf("role line should be truncated to 200 chars, got:\n%s", card)
	}
}
