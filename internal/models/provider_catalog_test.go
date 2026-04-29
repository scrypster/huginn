package models

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
		{"anthropic", "claude-sonnet-4-6", "claude-sonnet-4-6"}, // exact ID unchanged
		{"anthropic", "claude-haiku-4-5", "claude-haiku-4-5"},   // deprecated, not an alias
		{"", "haiku", "claude-haiku-4-5-20251001"},              // no-provider search
		{"openai", "4o-mini", "gpt-4o-mini"},
		{"anthropic", "unknown-model-xyz", "unknown-model-xyz"}, // unknown → unchanged
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
		{"anthropic", "claude-haiku-4-5"},          // deprecated
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

func TestGlobalProviderCatalog_Info_ResolvesAlias(t *testing.T) {
	ResetGlobalProviderCatalog()
	cat := GlobalProviderCatalog()
	info := cat.Info("anthropic", "haiku")
	if info == nil {
		t.Fatal("Info returned nil for known alias anthropic/haiku")
	}
	if info.DisplayName == "" {
		t.Fatal("Info returned empty DisplayName for known alias")
	}
}

func TestProviderCatalog_Overlay_AddsAliasAndInfo(t *testing.T) {
	cat := &ProviderCatalog{}
	if err := cat.load([]byte(`{"catalog_version":"base","providers":{}}`)); err != nil {
		t.Fatalf("load base catalog: %v", err)
	}
	if err := cat.overlay([]byte(`{
		"catalog_version": "overlay",
		"providers": {
			"test-provider": {
				"test-model-1": {
					"display_name": "Test Model 1",
					"aliases": ["friendly-model"]
				}
			}
		}
	}`)); err != nil {
		t.Fatalf("overlay: %v", err)
	}
	if got := cat.Resolve("test-provider", "friendly-model"); got != "test-model-1" {
		t.Fatalf("Resolve alias = %q, want %q", got, "test-model-1")
	}
	info := cat.Info("test-provider", "friendly-model")
	if info == nil || info.DisplayName != "Test Model 1" {
		t.Fatalf("Info for alias = %+v, want DisplayName=Test Model 1", info)
	}
}

func TestDefaultProviderCatalogPath_UsesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got := defaultProviderCatalogPath()
	want := filepath.Join(home, ".huginn", "provider_catalog.json")
	if got != want {
		t.Fatalf("defaultProviderCatalogPath() = %q, want %q", got, want)
	}
}

func TestTryRefreshProviderCatalog_SkipsWhenCacheIsFresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cachePath := defaultProviderCatalogPath()
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	initial := []byte(`{"catalog_version":"cached","providers":{}}`)
	if err := os.WriteFile(cachePath, initial, 0644); err != nil {
		t.Fatalf("write cache: %v", err)
	}

	// With a fresh cache and long maxAge, refresh should return before network I/O.
	TryRefreshProviderCatalog("http://127.0.0.1:1/unreachable", 24*time.Hour)
	time.Sleep(150 * time.Millisecond)

	got, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	if string(got) != string(initial) {
		t.Fatal("expected fresh cache file to remain unchanged")
	}
}
