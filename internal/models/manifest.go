package models

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/scrypster/huginn/internal/logger"
)

//go:embed models.json
var curatedJSON []byte

// ModelEntry is a single model in the manifest.
type ModelEntry struct {
	Description      string   `json:"description"`
	Provider         string   `json:"provider"`
	ProviderURL      string   `json:"provider_url"`
	Host             string   `json:"host"`
	HostURL          string   `json:"host_url"`
	URL              string   `json:"url"`
	Filename         string   `json:"filename"`
	SHA256           string   `json:"sha256"`
	SizeBytes        int64    `json:"size_bytes"`
	MinRAMGB         int      `json:"min_ram_gb"`
	RecommendedRAMGB int      `json:"recommended_ram_gb"`
	ContextLength    int      `json:"context_length"`
	ChatTemplate     string   `json:"chat_template"`
	Tags             []string `json:"tags"`
	Source           string   `json:"-"` // "curated", "remote", or "user" — set at load time
}

type manifestFile struct {
	Version int                   `json:"huginn_manifest_version"`
	Models  map[string]ModelEntry `json:"models"`
}

// LoadMerged loads the curated manifest and merges the user manifest on top.
// User entries win wholesale on name collision. Bad user entries are warned and skipped.
func LoadMerged() (map[string]ModelEntry, error) {
	// Load curated (embedded)
	var curated manifestFile
	if err := json.Unmarshal(curatedJSON, &curated); err != nil {
		return nil, fmt.Errorf("models: parse curated manifest: %w", err)
	}

	result := make(map[string]ModelEntry, len(curated.Models))
	for name, entry := range curated.Models {
		entry.Source = "curated"
		entry = applyDefaults(name, entry)
		result[name] = entry
	}

	// Load user manifest (optional)
	userPath, err := userManifestPath()
	if err == nil {
		userEntries, errs := loadUserManifest(userPath)
		for _, e := range errs {
			logger.Warn("models: user manifest warning", "err", e)
		}
		for name, entry := range userEntries {
			entry.Source = "user"
			entry = applyDefaults(name, entry)
			result[name] = entry // user wins
		}
	}

	return result, nil
}

func userManifestPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".huginn", "models.user.json"), nil
}

func loadUserManifest(path string) (map[string]ModelEntry, []error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil // not an error — file is optional
	}
	if err != nil {
		return nil, []error{fmt.Errorf("read %s: %w", path, err)}
	}

	var mf manifestFile
	if err := json.Unmarshal(data, &mf); err != nil {
		return nil, []error{fmt.Errorf("parse %s: %w", path, err)}
	}

	var errs []error
	valid := make(map[string]ModelEntry)
	for name, entry := range mf.Models {
		if entry.URL == "" {
			errs = append(errs, fmt.Errorf("entry %q: missing required field 'url'", name))
			continue
		}
		valid[name] = entry
	}
	return valid, errs
}

func applyDefaults(name string, e ModelEntry) ModelEntry {
	if e.Filename == "" {
		// Derive from URL: use last path segment
		parts := filepath.Base(e.URL)
		if parts != "" && parts != "." {
			e.Filename = parts
		} else {
			e.Filename = name + ".gguf"
		}
	}
	if e.ContextLength == 0 {
		e.ContextLength = 4096
	}
	if e.ChatTemplate == "" {
		e.ChatTemplate = "chatml"
	}
	return e
}
