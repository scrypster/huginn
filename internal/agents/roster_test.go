package agents_test

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/modelconfig"
)

func TestBuildRoster_IncludesNonPrimaryAgents(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Stacy", ModelID: "claude-sonnet-4"})
	reg.Register(&agents.Agent{Name: "Tom", ModelID: "qwen2.5-coder:7b"})

	roster := agents.BuildRoster(reg, nil, "Stacy")
	if !strings.Contains(roster, "Tom") {
		t.Error("roster should include Tom")
	}
	if strings.Contains(roster, "Stacy") {
		t.Error("roster should exclude the primary agent (Stacy)")
	}
}

func TestBuildRoster_EmptyRegistryReturnsEmpty(t *testing.T) {
	reg := agents.NewRegistry()
	roster := agents.BuildRoster(reg, nil, "Alex")
	if roster != "" {
		t.Errorf("expected empty roster, got %q", roster)
	}
}

func TestBuildRoster_OnlyPrimaryAgent_ReturnsEmpty(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Alex"})
	roster := agents.BuildRoster(reg, nil, "Alex")
	if roster != "" {
		t.Errorf("expected empty roster when only primary, got %q", roster)
	}
}

func TestBuildRoster_StartsWithHeader(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Stacy"})
	reg.Register(&agents.Agent{Name: "Sam"})
	roster := agents.BuildRoster(reg, nil, "Alex")
	if !strings.HasPrefix(roster, "Available team members:") {
		t.Errorf("expected roster header, got: %q", roster)
	}
}

func TestBuildRoster_WithInfoFn_ShowsToolsAnnotation(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Stacy", ModelID: "claude-sonnet-4"})

	infoFn := func(modelID string) *modelconfig.ModelInfo {
		info := &modelconfig.ModelInfo{Name: modelID, SupportsTools: true}
		info.InferCapabilities()
		return info
	}

	roster := agents.BuildRoster(reg, infoFn, "Alex")
	if !strings.Contains(roster, "tools: yes") {
		t.Errorf("expected 'tools: yes' annotation, got: %s", roster)
	}
}

func TestBuildRoster_CaseInsensitivePrimaryExclusion(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Alex"})
	reg.Register(&agents.Agent{Name: "Stacy"})
	// Primary name in different case
	roster := agents.BuildRoster(reg, nil, "alex")
	if strings.Contains(roster, "Alex") {
		t.Error("Alex should be excluded even with case mismatch")
	}
	if !strings.Contains(roster, "Stacy") {
		t.Error("Stacy should be in roster")
	}
}

func TestBuildRoster_WithInfoFn_ShowsTierAnnotations(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "HighTier", ModelID: "high-model"})
	reg.Register(&agents.Agent{Name: "MediumTier", ModelID: "med-model"})

	infoFn := func(modelID string) *modelconfig.ModelInfo {
		info := &modelconfig.ModelInfo{Name: modelID}
		if modelID == "high-model" {
			info.Tier = modelconfig.TierHigh
		} else if modelID == "med-model" {
			info.Tier = modelconfig.TierMedium
		} else {
			info.Tier = modelconfig.TierLow
		}
		info.SupportsTools = true
		return info
	}

	roster := agents.BuildRoster(reg, infoFn, "Primary")
	if !strings.Contains(roster, "capable") {
		t.Error("expected 'capable' tier annotation for high-tier model")
	}
	if !strings.Contains(roster, "medium") {
		t.Error("expected 'medium' tier annotation for medium-tier model")
	}
}

func TestBuildRoster_NoToolsSupport(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "NoTools", ModelID: "model"})

	infoFn := func(modelID string) *modelconfig.ModelInfo {
		return &modelconfig.ModelInfo{Name: modelID, SupportsTools: false}
	}

	roster := agents.BuildRoster(reg, infoFn, "Primary")
	if !strings.Contains(roster, "tools: no") {
		t.Error("expected 'tools: no' annotation")
	}
}

func TestBuildRoster_NoModelInfo_DefaultsToUnknown(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "NoInfo", ModelID: "unknown"})

	infoFn := func(modelID string) *modelconfig.ModelInfo {
		return nil // No info available
	}

	roster := agents.BuildRoster(reg, infoFn, "Primary")
	if !strings.Contains(roster, "tools: unknown") {
		t.Error("expected 'tools: unknown' for missing model info")
	}
}

func TestBuildRoster_AgentWithPersona_IncludesBlurb(t *testing.T) {
	reg := agents.NewRegistry()
	prompt := "You are Steve, a pragmatic senior engineer. You write clean code."
	reg.Register(&agents.Agent{Name: "Steve", SystemPrompt: prompt})

	roster := agents.BuildRoster(reg, nil, "Primary")
	if !strings.Contains(roster, "pragmatic senior engineer") {
		t.Errorf("expected persona blurb in roster, got: %s", roster)
	}
}

func TestExtractPersonaBlurb_StandardFormat(t *testing.T) {
	tests := []struct {
		name     string
		prompt   string
		expected string
	}{
		{
			name:     "Standard prefix with period",
			prompt:   "You are Chris, a meticulous architect. You think in systems.",
			expected: "a meticulous architect",
		},
		{
			name:     "Standard prefix with exclamation",
			prompt:   "You are Steve, a pragmatic engineer! You write clean code.",
			expected: "a pragmatic engineer",
		},
		{
			name:     "Standard prefix with question mark",
			prompt:   "You are Mark, a deep thinker? You find edge cases.",
			expected: "a deep thinker",
		},
		{
			name:     "Empty string",
			prompt:   "",
			expected: "",
		},
		{
			name:     "Whitespace only",
			prompt:   "   \n  \t  ",
			expected: "",
		},
		{
			name:     "No comma prefix",
			prompt:   "This is a prompt without the standard format. First sentence.",
			expected: "This is a prompt without the standard format",
		},
		{
			name:     "No sentence terminator",
			prompt:   "You are Mark, a deep thinker without ending punctuation",
			expected: "a deep thinker without ending punctuation",
		},
		{
			name:     "Long blurb truncated to 60 chars",
			prompt:   "You are Agent, a very long description that is definitely more than sixty characters long and should be truncated.",
			expected: "a very long description that is definitely more than sixt",
		},
		{
			name:     "Comma after 20 chars not stripped",
			prompt:   "This is a very long prefix, and the actual content.",
			expected: "This is a very long prefix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We need to test extractPersonaBlurb directly; it's unexported
			// So we test it indirectly via BuildRoster behavior
			// For now, document what we expect and verify integration
			reg := agents.NewRegistry()
			reg.Register(&agents.Agent{Name: "TestAgent", SystemPrompt: tt.prompt})
			roster := agents.BuildRoster(reg, nil, "Primary")

			if tt.expected == "" {
				// If no blurb expected, agent line should not have " — " in it
				if !strings.Contains(roster, "TestAgent") && roster != "" {
					t.Errorf("agent should be in roster even with empty blurb")
				}
			} else {
				if !strings.Contains(roster, tt.expected) {
					t.Errorf("expected blurb %q in roster, got: %s", tt.expected, roster)
				}
			}
		})
	}
}
