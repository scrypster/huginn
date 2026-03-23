package models

import (
	"testing"
)

func TestGlobalProviderCatalog_Aliases(t *testing.T) {
	// Reset so we get a clean load from bundled JSON.
	ResetGlobalProviderCatalog()
	cat := GlobalProviderCatalog()

	cases := []struct {
		provider, input, want string
	}{
		{"anthropic", "haiku", "claude-haiku-4-5-20251001"},
		{"anthropic", "sonnet", "claude-sonnet-4-6"},
		{"anthropic", "opus", "claude-opus-4-6"},
		{"anthropic", "claude-sonnet-4-6", "claude-sonnet-4-6"},           // exact ID unchanged
		{"anthropic", "claude-haiku-4-5", "claude-haiku-4-5"},            // deprecated, not an alias
		{"", "haiku", "claude-haiku-4-5-20251001"},                        // no-provider search
		{"openai", "4o-mini", "gpt-4o-mini"},
		{"anthropic", "unknown-model-xyz", "unknown-model-xyz"},           // unknown → unchanged
	}

	for _, c := range cases {
		got := cat.Resolve(c.provider, c.input)
		if got != c.want {
			t.Errorf("Resolve(%q, %q) = %q; want %q", c.provider, c.input, got, c.want)
		}
	}
	t.Logf("Catalog version: %s", cat.Version())
}

func TestGlobalProviderCatalog_Deprecations(t *testing.T) {
	ResetGlobalProviderCatalog()
	cat := GlobalProviderCatalog()

	pairs := []struct{ Provider, ModelID string }{
		{"anthropic", "claude-haiku-4-5"},         // deprecated
		{"anthropic", "claude-haiku-4-5-20251001"}, // not deprecated
		{"anthropic", "claude-sonnet-4-5"},         // deprecated
	}
	warnings := cat.CheckDeprecations(pairs)

	if len(warnings) != 2 {
		t.Errorf("expected 2 deprecation warnings, got %d: %+v", len(warnings), warnings)
	}
	for _, w := range warnings {
		if w.ReplacedBy == "" {
			t.Errorf("deprecation warning for %s/%s has empty ReplacedBy", w.Provider, w.ModelID)
		}
		t.Logf("DEPRECATED %s/%s → %s", w.Provider, w.ModelID, w.ReplacedBy)
	}
}
