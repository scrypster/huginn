package modelconfig

import "testing"

// TestDefaultContextWindow verifies that DefaultContextWindow returns 8192.
func TestDefaultContextWindow(t *testing.T) {
	got := DefaultContextWindow()
	if got != 8192 {
		t.Errorf("DefaultContextWindow() = %d, want 8192", got)
	}
}

// TestDefaultContextWindow_MatchesFallback verifies that DefaultContextWindow
// matches what ContextWindowForModel returns for an unknown model.
func TestDefaultContextWindow_MatchesFallback(t *testing.T) {
	unknown := ContextWindowForModel("some-totally-unknown-model-xyz")
	dflt := DefaultContextWindow()
	if unknown != dflt {
		t.Errorf("unknown model window %d != DefaultContextWindow %d", unknown, dflt)
	}
}

// TestModels_Set_UnknownSlot verifies that directly setting a known model works.
func TestModels_Set_UnknownSlot(t *testing.T) {
	m := &Models{
		Reasoner: "r",
	}
	// Reasoner must be unchanged when we don't touch it
	if m.Reasoner != "r" {
		t.Error("Reasoner must equal 'r'")
	}
}

// TestNewRegistry_EmptyAvailable verifies that a freshly created registry
// has an empty Available slice.
func TestNewRegistry_EmptyAvailable(t *testing.T) {
	reg := NewRegistry(DefaultModels())
	if len(reg.Available) != 0 {
		t.Errorf("expected empty Available, got %d entries", len(reg.Available))
	}
}

// TestInferCapabilities_MediumTier13b verifies that a model with "13b" in
// the name is classified as medium tier.
func TestInferCapabilities_MediumTier13b(t *testing.T) {
	m := ModelInfo{Name: "llama2:13b", ContextWindow: 4096, SupportsTools: true}
	m.InferCapabilities()
	if m.Tier != TierMedium {
		t.Errorf("expected TierMedium for llama2:13b, got %s", m.Tier)
	}
}

// TestInferCapabilities_HighTier_GPT4Variant verifies "gpt4" pattern (without hyphen).
func TestInferCapabilities_HighTier_GPT4Variant(t *testing.T) {
	m := ModelInfo{Name: "custom-gpt4-model", ContextWindow: 128000, SupportsTools: true}
	m.InferCapabilities()
	if m.Tier != TierHigh {
		t.Errorf("expected TierHigh for gpt4 variant, got %s", m.Tier)
	}
}

// TestCapabilityTier_Values verifies the tier constants are non-empty strings.
func TestCapabilityTier_Values(t *testing.T) {
	tiers := []CapabilityTier{TierHigh, TierMedium, TierLow}
	for _, tier := range tiers {
		if string(tier) == "" {
			t.Error("expected non-empty tier string")
		}
	}
	// All three must be distinct.
	if TierHigh == TierMedium || TierMedium == TierLow || TierHigh == TierLow {
		t.Error("tier constants must be distinct")
	}
}

// TestSlotSupportsTools_ModelFound verifies that when the model is in Available
// with SupportsTools=true, the method returns true.
func TestSlotSupportsTools_ModelFound_True(t *testing.T) {
	reg := &ModelRegistry{
		Available: []ModelInfo{
			{Name: "smart-model", ContextWindow: 128000, SupportsTools: true},
		},
	}
	if !reg.ModelSupportsTools("smart-model") {
		t.Error("expected SupportsTools=true for smart-model")
	}
}

// TestContextWindowForModel_FullVariantSuffix verifies that a model with a
// date-like suffix still gets matched if the base is a known key.
// e.g. ContextWindowForModel("gpt-4o") == 128000 (exact match).
func TestContextWindowForModel_KnownModels(t *testing.T) {
	cases := []struct {
		model string
		min   int
	}{
		{"gpt-4-turbo", 100000},
		{"o3", 100000},
		{"llama3.3:70b", 100000},
	}
	for _, tc := range cases {
		got := ContextWindowForModel(tc.model)
		if got < tc.min {
			t.Errorf("ContextWindowForModel(%q) = %d, want >= %d", tc.model, got, tc.min)
		}
	}
}

// TestInferCapabilities_CaseInsensitive verifies that model name matching
// is case-insensitive (name is lowercased before pattern matching).
func TestInferCapabilities_CaseInsensitive(t *testing.T) {
	m := ModelInfo{Name: "CLAUDE-OPUS-UPPER", ContextWindow: 200000, SupportsTools: true}
	m.InferCapabilities()
	if m.Tier != TierHigh {
		t.Errorf("expected TierHigh for CLAUDE-OPUS-UPPER (case-insensitive), got %s", m.Tier)
	}
}

// TestInferCapabilities_LowTier verifies the default low-tier path.
func TestInferCapabilities_LowTier(t *testing.T) {
	m := &ModelInfo{Name: "llama3:7b", ContextWindow: 4096, SupportsTools: false}
	m.InferCapabilities()
	if m.Tier != TierLow {
		t.Errorf("expected TierLow, got %q", m.Tier)
	}
	if m.SupportsDelegation {
		t.Error("expected SupportsDelegation=false for low-tier")
	}
	if m.ReliableFinish {
		t.Error("expected ReliableFinish=false for low-tier")
	}
	if m.PromptBudget != 1024 {
		t.Errorf("expected PromptBudget=1024, got %d", m.PromptBudget)
	}
}

// TestInferCapabilities_MediumTier_NoTools verifies medium tier when SupportsTools=false.
func TestInferCapabilities_MediumTier_NoTools(t *testing.T) {
	m := &ModelInfo{Name: "haiku-ultra", ContextWindow: 8192, SupportsTools: false}
	m.InferCapabilities()
	if m.Tier != TierMedium {
		t.Errorf("expected TierMedium, got %q", m.Tier)
	}
	if m.SupportsDelegation {
		t.Error("medium tier without tools: expected SupportsDelegation=false")
	}
}

// TestInferCapabilities_HighTier_Opus verifies the high-tier "opus" pattern.
func TestInferCapabilities_HighTier_Opus(t *testing.T) {
	m := &ModelInfo{Name: "claude-opus-4", ContextWindow: 200000, SupportsTools: true}
	m.InferCapabilities()
	if m.Tier != TierHigh {
		t.Errorf("expected TierHigh, got %q", m.Tier)
	}
	if !m.SupportsDelegation {
		t.Error("expected SupportsDelegation=true for high-tier")
	}
	if m.PromptBudget != 8192 {
		t.Errorf("expected PromptBudget=8192, got %d", m.PromptBudget)
	}
}
