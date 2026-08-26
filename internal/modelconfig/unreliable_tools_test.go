package modelconfig

import "testing"

func TestIsLowTierToolClass(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"qwen2.5-coder:7b", true},
		{"llama3:7b", true},
		{"qwen2.5:3b", true},
		{"phi:tiny", true},
		{"tinyllama", true},
		{"qwen2.5-coder:14b", false},
		{"llama2:13b", false},
		{"llama3.3:70b", false},
		{"qwen2.5-coder:32b", false},
		{"claude-sonnet-4-6", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsLowTierToolClass(tc.name); got != tc.want {
			t.Errorf("IsLowTierToolClass(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestUnreliableForTools(t *testing.T) {
	// 7b-class warns even when Ollama advertises tools.
	if !UnreliableForTools("qwen2.5-coder:7b", true) {
		t.Error("7b with probed tools should still warn")
	}
	if !UnreliableForTools("custom-model", false) {
		t.Error("SupportsTools=false should warn")
	}
	if UnreliableForTools("qwen2.5-coder:14b", true) {
		t.Error("14b with tools should stay quiet")
	}
	if UnreliableForTools("llama3.3:70b", true) {
		t.Error("70b must not match the 7b token")
	}
}

func TestReplaceAvailable_FillsSupportsTools(t *testing.T) {
	reg := NewRegistry(DefaultModels())
	reg.ReplaceAvailable([]ModelInfo{
		{Name: "qwen2.5-coder:7b", SupportsTools: true, Tier: TierLow},
		{Name: "no-tools", SupportsTools: false},
	})
	if !reg.ModelSupportsTools("qwen2.5-coder:7b") {
		t.Error("probed 7b with tools should not be hardcoded false")
	}
	if reg.ModelSupportsTools("no-tools") {
		t.Error("explicit SupportsTools=false must not default true")
	}
	if !reg.ModelSupportsTools("unknown-cloud") {
		t.Error("unknown models stay optimistic")
	}
}

func TestInferCapabilities_Qwen7bVs14b(t *testing.T) {
	low := ModelInfo{Name: "qwen2.5-coder:7b", SupportsTools: true}
	low.InferCapabilities()
	if low.Tier != TierLow {
		t.Errorf("7b tier = %s, want low", low.Tier)
	}
	if low.SupportsDelegation {
		t.Error("7b SupportsDelegation should be false")
	}

	med := ModelInfo{Name: "qwen2.5-coder:14b", SupportsTools: true}
	med.InferCapabilities()
	if med.Tier != TierMedium {
		t.Errorf("14b tier = %s, want medium", med.Tier)
	}
	if !med.SupportsDelegation {
		t.Error("14b with tools should support delegation")
	}
}
