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

func TestBuildRoster_NoModelInfo_NoAnnotation(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "NoInfo", ModelID: "unknown"})

	infoFn := func(modelID string) *modelconfig.ModelInfo {
		return nil // No info available
	}

	roster := agents.BuildRoster(reg, infoFn, "Primary")
	// When info is not available, the card has no tier/tools annotation (just name on first line)
	if !strings.Contains(roster, "- NoInfo\n") {
		t.Error("expected agent name without tier/tools annotation when info is missing")
	}
	if strings.Contains(roster, "tools:") {
		t.Error("expected no tools annotation for missing model info")
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
	if !strings.Contains(roster, "Role:") {
		t.Errorf("expected 'Role:' label in roster, got: %s", roster)
	}
}
