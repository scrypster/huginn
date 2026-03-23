package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// InstalledEntry tracks a skill's provenance in installed.json.
type InstalledEntry struct {
	Name    string `json:"name"`
	Source  string `json:"source"` // "registry", "local", or "github:user/repo"
	Enabled bool   `json:"enabled"`
}

// Manifest is the in-memory representation of installed.json.
type Manifest struct {
	Entries []InstalledEntry
	path    string
}

// DefaultManifestPath returns the default path to installed.json.
// Returns ~/.huginn/skills/installed.json
func DefaultManifestPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fallback if UserHomeDir fails (should not happen in normal circumstances)
		return filepath.Join(".", ".huginn", "skills", "installed.json")
	}
	return filepath.Join(home, ".huginn", "skills", "installed.json")
}

// LoadManifest reads the manifest from the given path.
// If the file does not exist, returns a nil error and an empty Manifest with the path set.
// If the file exists but is invalid JSON, returns an error.
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist: return empty manifest with path set
			return &Manifest{
				Entries: []InstalledEntry{},
				path:    path,
			}, nil
		}
		// Other errors (permission, etc.) are returned
		return nil, fmt.Errorf("skills: LoadManifest %q: %w", path, err)
	}

	// File exists: parse JSON
	var entries []InstalledEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("skills: LoadManifest %q: JSON parse: %w", path, err)
	}

	return &Manifest{
		Entries: entries,
		path:    path,
	}, nil
}

// Save writes the manifest to its path as indented JSON.
// Creates parent directories if needed.
func (m *Manifest) Save() error {
	// Ensure parent directory exists
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("skills: Manifest.Save: MkdirAll %q: %w", dir, err)
	}

	// Marshal to indented JSON
	data, err := json.MarshalIndent(m.Entries, "", "  ")
	if err != nil {
		return fmt.Errorf("skills: Manifest.Save: marshal: %w", err)
	}

	// Atomic write: write to a uniquely-named temp file then rename so that
	// (a) a crash mid-write never leaves a corrupt installed.json and
	// (b) concurrent Save() calls don't collide on the same .tmp path.
	f, err := os.CreateTemp(dir, "installed-*.tmp")
	if err != nil {
		return fmt.Errorf("skills: Manifest.Save: CreateTemp: %w", err)
	}
	tmp := f.Name()
	if _, werr := f.Write(data); werr != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("skills: Manifest.Save: write tmp %q: %w", tmp, werr)
	}
	if cerr := f.Close(); cerr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("skills: Manifest.Save: close tmp %q: %w", tmp, cerr)
	}
	if err := os.Rename(tmp, m.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("skills: Manifest.Save: rename %q -> %q: %w", tmp, m.path, err)
	}

	return nil
}

// Upsert inserts or updates an entry by name.
// If an entry with the same Name exists, it is replaced. Otherwise, appended.
func (m *Manifest) Upsert(e InstalledEntry) {
	for i, entry := range m.Entries {
		if entry.Name == e.Name {
			// Update existing entry
			m.Entries[i] = e
			return
		}
	}
	// Not found: append new entry
	m.Entries = append(m.Entries, e)
}

// Remove deletes an entry by name.
// Returns true if an entry was removed, false if not found.
func (m *Manifest) Remove(name string) bool {
	for i, entry := range m.Entries {
		if entry.Name == name {
			// Remove by slicing
			m.Entries = append(m.Entries[:i], m.Entries[i+1:]...)
			return true
		}
	}
	return false
}

// SetEnabled updates the enabled status of an entry by name.
// Returns true if the entry was found and updated, false otherwise.
func (m *Manifest) SetEnabled(name string, enabled bool) bool {
	for i, entry := range m.Entries {
		if entry.Name == name {
			m.Entries[i].Enabled = enabled
			return true
		}
	}
	return false
}

// Get returns a pointer to the entry with the given name, or nil if not found.
func (m *Manifest) Get(name string) *InstalledEntry {
	for i, entry := range m.Entries {
		if entry.Name == name {
			return &m.Entries[i]
		}
	}
	return nil
}
