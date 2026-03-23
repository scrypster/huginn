package skills

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	RegistryIndexURL = "https://skills.huginncloud.com/index.json"
	cacheTTL         = 24 * time.Hour
)

// IndexEntry represents a single skill in the registry index.
type IndexEntry struct {
	ID          string   `json:"id"`           // "obra/brainstorming"
	Name        string   `json:"name"`         // "brainstorming"
	DisplayName string   `json:"display_name"` // "Brainstorming"
	Author      string   `json:"author"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	SourceURL   string   `json:"source_url"` // full URL to the raw SKILL.md
	Collection  string   `json:"collection"` // e.g. "obra/superpowers", or ""
}

// IndexCollection represents a collection of related skills.
type IndexCollection struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Author      string   `json:"author"`
	Description string   `json:"description"`
	Skills      []string `json:"skills"` // list of skill IDs in the collection
}

// remoteRegistry is the on-wire format from skills.huginncloud.com/index.json
type remoteRegistry struct {
	Version     string            `json:"version"`
	GeneratedAt string            `json:"generated_at"`
	Skills      []IndexEntry      `json:"skills"`
	Collections []IndexCollection `json:"collections"`
}

// cachedIndex is the on-disk format with freshness timestamp.
type cachedIndex struct {
	FetchedAt   time.Time         `json:"fetched_at"`
	Entries     []IndexEntry      `json:"entries"`
	Collections []IndexCollection `json:"collections"`
}

// DefaultCachePath returns the default cache path for the skills index.
func DefaultCachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".huginn", "cache", "skills-index.json")
}

// LoadIndex loads the skills index from cache, fetching from the registry if
// the cache is missing or stale. Returns skills and collections.
func LoadIndex(cachePath string) ([]IndexEntry, []IndexCollection, error) {
	entries, collections, err := loadIndexFromFile(cachePath)
	if err == nil {
		return entries, collections, nil
	}
	return fetchAndCacheIndex(RegistryIndexURL, cachePath)
}

// loadIndexFromFile reads cache and returns entries if cache is fresh.
func loadIndexFromFile(cachePath string) ([]IndexEntry, []IndexCollection, error) {
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, nil, fmt.Errorf("read cache file: %w", err)
	}

	var cached cachedIndex
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, nil, fmt.Errorf("parse cache file: %w", err)
	}

	if time.Since(cached.FetchedAt) > cacheTTL {
		return nil, nil, fmt.Errorf("cache is stale (age: %v)", time.Since(cached.FetchedAt))
	}

	return cached.Entries, cached.Collections, nil
}

// fetchAndCacheIndex fetches the index from the registry URL, caches it, and
// returns the skill entries and collections.
func fetchAndCacheIndex(url, cachePath string) ([]IndexEntry, []IndexCollection, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("fetch registry: got status %d", resp.StatusCode)
	}

	const maxIndexBytes = 10 << 20
	lr := io.LimitReader(resp.Body, int64(maxIndexBytes+1))
	body, err := io.ReadAll(lr)
	if err != nil {
		return nil, nil, fmt.Errorf("skills: fetch index: read body: %w", err)
	}
	if len(body) > maxIndexBytes {
		return nil, nil, fmt.Errorf("skills: fetch index: response exceeds 10 MB limit")
	}

	var remote remoteRegistry
	if err := json.Unmarshal(body, &remote); err != nil {
		return nil, nil, fmt.Errorf("parse registry index: %w", err)
	}

	if remote.Skills == nil {
		remote.Skills = []IndexEntry{}
	}
	if remote.Collections == nil {
		remote.Collections = []IndexCollection{}
	}

	// Write to cache (non-fatal)
	cached := cachedIndex{
		FetchedAt:   time.Now(),
		Entries:     remote.Skills,
		Collections: remote.Collections,
	}
	if cacheData, err := json.Marshal(cached); err == nil {
		if mkErr := os.MkdirAll(filepath.Dir(cachePath), 0755); mkErr == nil {
			_ = os.WriteFile(cachePath, cacheData, 0644)
		}
	}

	return remote.Skills, remote.Collections, nil
}

// FetchAndCacheIndex forces a refresh from the registry.
func FetchAndCacheIndex(cachePath string) ([]IndexEntry, []IndexCollection, error) {
	return fetchAndCacheIndex(RegistryIndexURL, cachePath)
}

// SearchIndex searches entries by name, description, author, and tags.
// An empty query returns all entries.
func SearchIndex(entries []IndexEntry, query string) []IndexEntry {
	if query == "" {
		return entries
	}
	q := strings.ToLower(query)
	var results []IndexEntry
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.Name), q) ||
			strings.Contains(strings.ToLower(entry.DisplayName), q) ||
			strings.Contains(strings.ToLower(entry.Description), q) ||
			strings.Contains(strings.ToLower(entry.Author), q) {
			results = append(results, entry)
			continue
		}
		for _, tag := range entry.Tags {
			if strings.Contains(strings.ToLower(tag), q) {
				results = append(results, entry)
				break
			}
		}
	}
	return results
}
