package models

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	CatalogURL            = "https://models.huginncloud.com/catalog.json"
	catalogFetchTimeout   = 5 * time.Second
)

// remoteCatalogEntry is the on-wire format from the remote catalog JSON.
type remoteCatalogEntry struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Provider         string   `json:"provider"`
	ProviderURL      string   `json:"provider_url"`
	Host             string   `json:"host"`
	HostURL          string   `json:"host_url"`
	Description      string   `json:"description"`
	URL              string   `json:"url"`
	Filename         string   `json:"filename"`
	SHA256           string   `json:"sha256"`
	SizeBytes        int64    `json:"size_bytes"`
	MinRAMGB         int      `json:"min_ram_gb"`
	RecommendedRAMGB int      `json:"recommended_ram_gb"`
	ContextLength    int      `json:"context_length"`
	ChatTemplate     string   `json:"chat_template"`
	Tags             []string `json:"tags"`
}

// remoteCatalog is the on-wire format from the remote catalog endpoint.
type remoteCatalog struct {
	Version     int                  `json:"version"`
	GeneratedAt string               `json:"generated_at"`
	Models      []remoteCatalogEntry `json:"models"`
}

// cachedCatalog is the on-disk cache format.
type cachedCatalog struct {
	FetchedAt time.Time            `json:"fetched_at"`
	Models    []remoteCatalogEntry `json:"models"`
}

// DefaultCatalogCachePath returns the default cache path for the model catalog.
func DefaultCatalogCachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".huginn", "cache", "models-catalog.json")
}

// LoadCatalog loads the model catalog using a network-first strategy:
//  1. Try to fetch from the remote CDN (5s timeout).
//  2. On success, update the local cache if the content changed.
//  3. On failure, fall back to the local cache (any age).
//  4. If no cache exists, fall back to the embedded manifest.
//
// The returned map is keyed by model ID (e.g. "qwen2.5-coder:7b").
func LoadCatalog(cachePath string) (map[string]ModelEntry, error) {
	entries, err := fetchAndCacheCatalog(CatalogURL, cachePath)
	if err != nil {
		// Remote unavailable — serve from local cache regardless of age.
		cached, cacheErr := readCacheAnyAge(cachePath)
		if cacheErr != nil {
			return LoadMerged()
		}
		return remoteEntriesToMap(cached), nil
	}
	return remoteEntriesToMap(entries), nil
}

// RefreshCatalog forces a fresh fetch from the remote catalog regardless of
// cache freshness. Falls back to embedded manifest on error.
func RefreshCatalog(cachePath string) (map[string]ModelEntry, error) {
	entries, err := fetchAndCacheCatalog(CatalogURL, cachePath)
	if err != nil {
		return LoadMerged()
	}
	return remoteEntriesToMap(entries), nil
}

// readCacheAnyAge reads the local cache ignoring age — used as offline fallback.
func readCacheAnyAge(cachePath string) ([]remoteCatalogEntry, error) {
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, fmt.Errorf("read catalog cache: %w", err)
	}
	var cached cachedCatalog
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, fmt.Errorf("parse catalog cache: %w", err)
	}
	return cached.Models, nil
}

// fetchAndCacheCatalog fetches the remote catalog, writes it to cache, and
// returns the raw entries.
func fetchAndCacheCatalog(url, cachePath string) ([]remoteCatalogEntry, error) {
	client := &http.Client{Timeout: catalogFetchTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch model catalog: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch model catalog: HTTP %d", resp.StatusCode)
	}

	const maxBytes = 10 << 20 // 10 MB
	lr := io.LimitReader(resp.Body, int64(maxBytes+1))
	body, err := io.ReadAll(lr)
	if err != nil {
		return nil, fmt.Errorf("fetch model catalog: read body: %w", err)
	}
	if len(body) > maxBytes {
		return nil, fmt.Errorf("fetch model catalog: response exceeds 10 MB limit")
	}

	var remote remoteCatalog
	if err := json.Unmarshal(body, &remote); err != nil {
		return nil, fmt.Errorf("parse model catalog: %w", err)
	}
	if remote.Models == nil {
		remote.Models = []remoteCatalogEntry{}
	}

	// Write to cache (non-fatal).
	cached := cachedCatalog{FetchedAt: time.Now(), Models: remote.Models}
	if cacheData, err := json.Marshal(cached); err == nil {
		if mkErr := os.MkdirAll(filepath.Dir(cachePath), 0755); mkErr == nil {
			_ = os.WriteFile(cachePath, cacheData, 0644)
		}
	}

	return remote.Models, nil
}

// remoteEntriesToMap converts the remote array format to the ModelEntry map
// used throughout the codebase.
func remoteEntriesToMap(entries []remoteCatalogEntry) map[string]ModelEntry {
	result := make(map[string]ModelEntry, len(entries))
	for _, e := range entries {
		entry := ModelEntry{
			Description:      e.Description,
			Provider:         e.Provider,
			ProviderURL:      e.ProviderURL,
			Host:             e.Host,
			HostURL:          e.HostURL,
			URL:              e.URL,
			Filename:         e.Filename,
			SHA256:           e.SHA256,
			SizeBytes:        e.SizeBytes,
			MinRAMGB:         e.MinRAMGB,
			RecommendedRAMGB: e.RecommendedRAMGB,
			ContextLength:    e.ContextLength,
			ChatTemplate:     e.ChatTemplate,
			Tags:             e.Tags,
			Source:           "remote",
		}
		result[e.ID] = applyDefaults(e.ID, entry)
	}
	return result
}
