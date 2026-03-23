package skills

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultCachePath(t *testing.T) {
	path := DefaultCachePath()
	if path == "" {
		t.Error("DefaultCachePath() returned empty string")
	}
	if !filepath.IsAbs(path) {
		t.Errorf("DefaultCachePath() returned relative path: %s", path)
	}
	// Verify it contains the expected path components
	home, _ := os.UserHomeDir()
	if !strings.HasPrefix(path, home) {
		t.Errorf("DefaultCachePath() should be in home directory: %s", path)
	}
}

func TestLoadIndex_FromCache(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "cache.json")

	// Create a fresh cache (1 hour old)
	freshTime := time.Now().Add(-1 * time.Hour)
	cached := cachedIndex{
		FetchedAt: freshTime,
		Entries: []IndexEntry{
			{
				Name:        "test-skill",
				Description: "A test skill",
				Author:      "test-author",
				Category:    "test",
			},
		},
	}

	data, err := json.Marshal(cached)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Load from cache
	entries, _, err := loadIndexFromFile(cachePath)
	if err != nil {
		t.Fatalf("loadIndexFromFile: %v", err)
	}

	if len(entries) != 1 {
		t.Errorf("Expected 1 entry, got %d", len(entries))
	}
	if entries[0].Name != "test-skill" {
		t.Errorf("Expected name 'test-skill', got %q", entries[0].Name)
	}
}

func TestLoadIndex_StaleCache(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "cache.json")

	// Create a stale cache (25 hours old)
	staleTime := time.Now().Add(-25 * time.Hour)
	cached := cachedIndex{
		FetchedAt: staleTime,
		Entries: []IndexEntry{
			{
				Name:        "stale-skill",
				Description: "This is stale",
				Author:      "test",
				Category:    "test",
			},
		},
	}

	data, err := json.Marshal(cached)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Try to load stale cache - should return error
	_, _, err = loadIndexFromFile(cachePath)
	if err == nil {
		t.Error("loadIndexFromFile should return error for stale cache")
	}
}

func TestFetchAndCacheIndex_Server(t *testing.T) {
	// Create test registry response in remoteRegistry format
	testRegistry := remoteRegistry{
		Version: "1",
		Skills: []IndexEntry{
			{
				Name:        "go-expert",
				Description: "Go expert skill",
				Author:      "huginn",
				Category:    "development",
				SourceURL:   "https://example.com/go-expert/SKILL.md",
			},
			{
				Name:        "python-master",
				Description: "Python master skill",
				Author:      "huginn",
				Category:    "development",
				SourceURL:   "https://example.com/python-master/SKILL.md",
			},
		},
	}

	// Create httptest server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(testRegistry)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "cache.json")

	// Fetch and cache
	entries, _, err := fetchAndCacheIndex(server.URL, cachePath)
	if err != nil {
		t.Fatalf("fetchAndCacheIndex: %v", err)
	}

	// Verify returned entries
	if len(entries) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(entries))
	}
	if entries[0].Name != "go-expert" {
		t.Errorf("Expected first entry name 'go-expert', got %q", entries[0].Name)
	}

	// Verify cache file was written
	if _, err := os.Stat(cachePath); err != nil {
		t.Errorf("Cache file not written: %v", err)
	}

	// Verify cache file content
	cached := cachedIndex{}
	cacheData, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("ReadFile cache: %v", err)
	}

	if err := json.Unmarshal(cacheData, &cached); err != nil {
		t.Fatalf("Unmarshal cache: %v", err)
	}

	if len(cached.Entries) != 2 {
		t.Errorf("Cache entries: expected 2, got %d", len(cached.Entries))
	}

	// Verify FetchedAt was set
	now := time.Now()
	if cached.FetchedAt.After(now.Add(1 * time.Second)) {
		t.Errorf("FetchedAt is in the future")
	}
	if cached.FetchedAt.Before(now.Add(-10 * time.Second)) {
		t.Errorf("FetchedAt is too far in the past")
	}
}

func TestSearchIndex(t *testing.T) {
	entries := []IndexEntry{
		{
			Name:        "go-expert",
			Description: "Go programming expert",
			Author:      "alice",
			Category:    "development",
		},
		{
			Name:        "python-master",
			Description: "Python data science master",
			Author:      "bob",
			Category:    "data",
		},
		{
			Name:        "golang-benchmarking",
			Description: "Performance tuning for Go applications",
			Author:      "alice",
			Category:    "development",
		},
	}

	// Test searching for "go"
	results := SearchIndex(entries, "go")
	if len(results) != 2 {
		t.Errorf("Search 'go': expected 2 results, got %d", len(results))
	}
	// Verify both "go-expert" and "golang-benchmarking" are returned
	found := make(map[string]bool)
	for _, e := range results {
		found[e.Name] = true
	}
	if !found["go-expert"] {
		t.Error("Search 'go': expected 'go-expert' in results")
	}
	if !found["golang-benchmarking"] {
		t.Error("Search 'go': expected 'golang-benchmarking' in results")
	}

	// Test empty query (should return all entries)
	results = SearchIndex(entries, "")
	if len(results) != 3 {
		t.Errorf("Search '': expected 3 results, got %d", len(results))
	}

	// Test search with no matches
	results = SearchIndex(entries, "rust")
	if len(results) != 0 {
		t.Errorf("Search 'rust': expected 0 results, got %d", len(results))
	}

	// Test case-insensitive search
	results = SearchIndex(entries, "PYTHON")
	if len(results) != 1 {
		t.Errorf("Search 'PYTHON': expected 1 result, got %d", len(results))
	}
	if results[0].Name != "python-master" {
		t.Errorf("Search 'PYTHON': expected 'python-master', got %q", results[0].Name)
	}

	// Test search in description
	results = SearchIndex(entries, "performance")
	if len(results) != 1 {
		t.Errorf("Search 'performance': expected 1 result, got %d", len(results))
	}
	if results[0].Name != "golang-benchmarking" {
		t.Errorf("Search 'performance': expected 'golang-benchmarking', got %q", results[0].Name)
	}

	// Test search in author
	results = SearchIndex(entries, "alice")
	if len(results) != 2 {
		t.Errorf("Search 'alice': expected 2 results, got %d", len(results))
	}
}

func TestLoadIndex_MissingCacheFallback(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, "cache.json")

	// Test that missing cache file is handled (by loadIndexFromFile)
	_, _, err := loadIndexFromFile(cachePath)
	if err == nil {
		t.Error("loadIndexFromFile should return error for missing file")
	}
}
