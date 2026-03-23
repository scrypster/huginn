package modelconfig_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/modelconfig"
)

func TestModelTierHigh(t *testing.T) {
	info := modelconfig.ModelInfo{
		Name:          "claude-opus-4-6",
		ContextWindow: 200000,
		SupportsTools: true,
	}
	info.InferCapabilities()
	if info.Tier != modelconfig.TierHigh {
		t.Errorf("expected TierHigh, got %s", info.Tier)
	}
	if !info.SupportsDelegation {
		t.Error("expected SupportsDelegation=true for TierHigh")
	}
	if !info.ReliableFinish {
		t.Error("expected ReliableFinish=true for TierHigh")
	}
	if info.PromptBudget != 8192 {
		t.Errorf("expected PromptBudget=8192, got %d", info.PromptBudget)
	}
}

func TestModelTierHighSonnet(t *testing.T) {
	info := modelconfig.ModelInfo{
		Name:          "claude-sonnet-4-6",
		ContextWindow: 200000,
		SupportsTools: true,
	}
	info.InferCapabilities()
	if info.Tier != modelconfig.TierHigh {
		t.Errorf("expected TierHigh for sonnet, got %s", info.Tier)
	}
}

func TestModelTierHighGPT4(t *testing.T) {
	info := modelconfig.ModelInfo{
		Name:          "gpt-4o",
		ContextWindow: 128000,
		SupportsTools: true,
	}
	info.InferCapabilities()
	if info.Tier != modelconfig.TierHigh {
		t.Errorf("expected TierHigh for gpt-4o, got %s", info.Tier)
	}
}

func TestModelTierMedium(t *testing.T) {
	info := modelconfig.ModelInfo{
		Name:          "claude-haiku-4-5-20251001",
		ContextWindow: 200000,
		SupportsTools: true,
	}
	info.InferCapabilities()
	if info.Tier != modelconfig.TierMedium {
		t.Errorf("expected TierMedium, got %s", info.Tier)
	}
	if info.PromptBudget != 4096 {
		t.Errorf("expected PromptBudget=4096, got %d", info.PromptBudget)
	}
}

func TestModelTierMedium14b(t *testing.T) {
	info := modelconfig.ModelInfo{
		Name:          "qwen2.5-coder:14b",
		ContextWindow: 32768,
		SupportsTools: true,
	}
	info.InferCapabilities()
	if info.Tier != modelconfig.TierMedium {
		t.Errorf("expected TierMedium for 14b model, got %s", info.Tier)
	}
}

func TestModelTierLow(t *testing.T) {
	info := modelconfig.ModelInfo{
		Name:          "qwen2.5-coder:7b",
		ContextWindow: 4096,
		SupportsTools: false,
	}
	info.InferCapabilities()
	if info.Tier != modelconfig.TierLow {
		t.Errorf("expected TierLow, got %s", info.Tier)
	}
	if info.SupportsDelegation {
		t.Error("expected SupportsDelegation=false for TierLow")
	}
	if info.ReliableFinish {
		t.Error("expected ReliableFinish=false for TierLow")
	}
	if info.PromptBudget != 1024 {
		t.Errorf("expected PromptBudget=1024, got %d", info.PromptBudget)
	}
}

func TestRegistryCapabilitiesByName(t *testing.T) {
	reg := &modelconfig.ModelRegistry{
		Available: []modelconfig.ModelInfo{
			{
				Name: "opus-model", ContextWindow: 200000, SupportsTools: true,
				Tier: modelconfig.TierHigh, SupportsDelegation: true,
				ReliableFinish: true, PromptBudget: 8192,
			},
		},
	}
	// Verify the model is in the registry and has expected tier.
	var found *modelconfig.ModelInfo
	for i := range reg.Available {
		if reg.Available[i].Name == "opus-model" {
			found = &reg.Available[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected opus-model in registry")
	}
	if found.Tier != modelconfig.TierHigh {
		t.Errorf("expected TierHigh, got %s", found.Tier)
	}
}

func TestRegistryEmpty_NoAvailable(t *testing.T) {
	reg := &modelconfig.ModelRegistry{
		Available: []modelconfig.ModelInfo{},
	}
	if len(reg.Available) != 0 {
		t.Errorf("expected empty registry, got %d entries", len(reg.Available))
	}
}

func TestInferCapabilitiesIdempotent(t *testing.T) {
	info := modelconfig.ModelInfo{
		Name:          "claude-opus-4-6",
		ContextWindow: 200000,
		SupportsTools: true,
	}
	info.InferCapabilities()
	tier1 := info.Tier
	info.InferCapabilities() // call again
	if info.Tier != tier1 {
		t.Error("InferCapabilities should be idempotent")
	}
}
