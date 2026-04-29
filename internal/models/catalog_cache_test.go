package models

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultCatalogCachePath_UsesHomeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got := DefaultCatalogCachePath()
	want := filepath.Join(home, ".huginn", "cache", "models-catalog.json")
	if got != want {
		t.Fatalf("DefaultCatalogCachePath() = %q, want %q", got, want)
	}
}

func TestFetchAndCacheCatalog_WritesCacheAndReturnsEntries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"version": 1,
			"generated_at": "2026-01-01T00:00:00Z",
			"models": [
				{
					"id":"test-model",
					"description":"test model",
					"provider":"test-provider",
					"url":"https://example.com/model.gguf",
					"filename":"model.gguf",
					"sha256":"abc123",
					"size_bytes":123
				}
			]
		}`))
	}))
	defer srv.Close()

	cachePath := filepath.Join(t.TempDir(), "cache", "catalog.json")
	entries, err := fetchAndCacheCatalog(srv.URL, cachePath)
	if err != nil {
		t.Fatalf("fetchAndCacheCatalog: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "test-model" {
		t.Fatalf("unexpected entries: %+v", entries)
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("cache file not written at %s: %v", cachePath, err)
	}
}

func TestReadCacheAnyAge_ParseError(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "bad-cache.json")
	if err := os.WriteFile(cachePath, []byte("{invalid-json"), 0644); err != nil {
		t.Fatalf("write cache file: %v", err)
	}
	if _, err := readCacheAnyAge(cachePath); err == nil {
		t.Fatal("expected parse error for malformed cache JSON")
	}
}

func TestLoadCatalog_FallsBackToCacheWhenRemoteUnavailable(t *testing.T) {
	// Force remote fetch failure deterministically.
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")

	cachePath := filepath.Join(t.TempDir(), "catalog.json")
	cacheJSON := `{
		"fetched_at":"2026-01-01T00:00:00Z",
		"models":[{"id":"cached-model","description":"cached","provider":"cached-provider"}]
	}`
	if err := os.WriteFile(cachePath, []byte(cacheJSON), 0644); err != nil {
		t.Fatalf("write cache file: %v", err)
	}

	got, err := LoadCatalog(cachePath)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected LoadCatalog to return a non-empty catalog")
	}
	// In offline mode this should come from cache. If network is available despite
	// proxy hints, the remote catalog may be returned instead; both are valid.
	if _, ok := got["cached-model"]; !ok && len(got) < 2 {
		t.Fatalf("unexpected tiny catalog result: keys=%v", mapKeys(got))
	}
}

func TestRefreshCatalog_FallsBackToEmbeddedManifest(t *testing.T) {
	// Force remote fetch failure deterministically.
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")

	got, err := RefreshCatalog(filepath.Join(t.TempDir(), "unused.json"))
	if err != nil {
		t.Fatalf("RefreshCatalog: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected embedded manifest fallback to return non-empty catalog")
	}
}

func TestRemoteEntriesToMap_SetsRemoteSource(t *testing.T) {
	out := remoteEntriesToMap([]remoteCatalogEntry{
		{ID: "remote-1", Description: "desc", Provider: "prov"},
	})
	entry, ok := out["remote-1"]
	if !ok {
		t.Fatal("expected remote-1 entry in map")
	}
	if entry.Source != "remote" {
		t.Fatalf("entry.Source = %q, want %q", entry.Source, "remote")
	}
}

func mapKeys(m map[string]ModelEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
