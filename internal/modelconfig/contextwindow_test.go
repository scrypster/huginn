package modelconfig

import "testing"

func TestContextWindowForModel_ExactMatch(t *testing.T) {
	cases := []struct {
		model string
		want  int
	}{
		{"claude-opus-4-6", 200000},
		{"claude-sonnet-4-6", 200000},
		{"claude-haiku-4-5", 200000},
		{"gpt-4o", 128000},
		{"gpt-4o-mini", 128000},
		{"gpt-4-turbo", 128000},
		{"o1", 200000},
		{"o3-mini", 200000},
		{"qwen2.5-coder:32b", 32768},
		{"qwen2.5-coder:14b", 32768},
		{"deepseek-r1:14b", 65536},
		{"deepseek-r1:32b", 65536},
		{"llama3.3:70b", 131072},
		{"codellama:34b", 16384},
		{"mistral:7b", 32768},
		{"gemma2:27b", 8192},
	}
	for _, tc := range cases {
		got := ContextWindowForModel(tc.model)
		if got != tc.want {
			t.Errorf("ContextWindowForModel(%q) = %d, want %d", tc.model, got, tc.want)
		}
	}
}

func TestContextWindowForModel_PrefixMatch(t *testing.T) {
	got := ContextWindowForModel("qwen2.5-coder")
	if got == 0 {
		t.Error("prefix match for qwen2.5-coder returned 0, want > 0")
	}
}

// TestContextWindowForModel_ModelIDStartsWithKey verifies that a versioned model ID
// (e.g. "claude-opus-4-6-20250514") resolves via the registered prefix key
// ("claude-opus-4-6"), not the other way around. This is a regression test for
// the inverted HasPrefix bug where strings.HasPrefix(key, modelID) was used
// instead of the correct strings.HasPrefix(modelID, key).
func TestContextWindowForModel_ModelIDStartsWithKey(t *testing.T) {
	cases := []struct {
		modelID string
		wantCW  int
		desc    string
	}{
		{
			modelID: "claude-opus-4-6-20250514",
			wantCW:  200000,
			desc:    "versioned claude-opus-4-6 variant resolves via prefix key",
		},
		{
			modelID: "claude-sonnet-4-6-20250514",
			wantCW:  200000,
			desc:    "versioned claude-sonnet-4-6 variant resolves via prefix key",
		},
		{
			modelID: "claude-haiku-4-5-20250307",
			wantCW:  200000,
			desc:    "versioned claude-haiku-4-5 variant resolves via prefix key",
		},
	}
	for _, tc := range cases {
		got := ContextWindowForModel(tc.modelID)
		if got != tc.wantCW {
			t.Errorf("%s: ContextWindowForModel(%q) = %d, want %d", tc.desc, tc.modelID, got, tc.wantCW)
		}
	}
}

func TestContextWindowForModel_UnknownModel_ReturnsDefault(t *testing.T) {
	got := ContextWindowForModel("some-unknown-model:99b")
	if got != 8192 {
		t.Errorf("unknown model: got %d, want default 8192", got)
	}
}

func TestContextWindowForModel_EmptyString_ReturnsDefault(t *testing.T) {
	got := ContextWindowForModel("")
	if got != 8192 {
		t.Errorf("empty model: got %d, want default 8192", got)
	}
}

func TestContextWindowForModel_PrefixDoesNotOverrideExactMatch(t *testing.T) {
	got := ContextWindowForModel("deepseek-r1:14b")
	if got != 65536 {
		t.Errorf("exact match shadowed by prefix: got %d, want 65536", got)
	}
}
