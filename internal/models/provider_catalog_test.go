package models

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestRefreshProviderCatalog_Non200StampsAndDoesNotRetry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	refreshProviderCatalog(srv.URL, 7*24*time.Hour)
	if hits != 1 {
		t.Fatalf("first start: hits=%d, want 1", hits)
	}

	stampPath := defaultProviderCatalogStampPath()
	data, err := os.ReadFile(stampPath)
	if err != nil {
		t.Fatalf("expected last-attempt stamp at %s: %v", stampPath, err)
	}
	var stamp struct {
		Status int `json:"status"`
	}
	if err := json.Unmarshal(data, &stamp); err != nil {
		t.Fatalf("parse stamp: %v", err)
	}
	if stamp.Status != http.StatusForbidden {
		t.Fatalf("stamp status=%d, want 403", stamp.Status)
	}

	if _, err := os.Stat(defaultProviderCatalogPath()); err == nil {
		t.Fatal("403 must not write provider_catalog.json; bundled catalog stays")
	}

	// Second startServer: freshness gate sees the stamp and must not GET again.
	refreshProviderCatalog(srv.URL, 7*24*time.Hour)
	if hits != 1 {
		t.Fatalf("second start retried CDN; hits=%d, want 1", hits)
	}
}

func TestRefreshProviderCatalog_Non200PreservesExistingCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cachePath := defaultProviderCatalogPath()
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	initial := []byte(`{"catalog_version":"bundled-stay","providers":{}}`)
	if err := os.WriteFile(cachePath, initial, 0644); err != nil {
		t.Fatalf("write cache: %v", err)
	}
	// Age the cache so the freshness gate would otherwise refetch.
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(cachePath, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	refreshProviderCatalog(srv.URL, 7*24*time.Hour)
	if hits != 1 {
		t.Fatalf("hits=%d, want 1", hits)
	}
	got, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	if string(got) != string(initial) {
		t.Fatalf("existing catalog overwritten on non-200: %s", got)
	}

	refreshProviderCatalog(srv.URL, 7*24*time.Hour)
	if hits != 1 {
		t.Fatalf("500 stamp did not gate retry; hits=%d", hits)
	}
}

func TestDefaultProviderCatalogStampPath_UsesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got := defaultProviderCatalogStampPath()
	want := filepath.Join(home, ".huginn", "provider_catalog.last_attempt")
	if got != want {
		t.Fatalf("defaultProviderCatalogStampPath() = %q, want %q", got, want)
	}
}
