package secrets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// fileStoreData is the on-disk JSON format for the secrets file.
type fileStoreData struct {
	Slots map[string]string `json:"slots"`
}

// FileStore persists secrets in ~/.huginn/secrets.json with 0600 permissions.
// It is the fallback backend when the OS keychain is unavailable (headless Linux,
// Docker, CI). The file is separate from config.json so it survives config resets.
// Safe for concurrent use.
type FileStore struct {
	path string
	mu   sync.Mutex
}

// NewFileStore creates a FileStore using the given file path.
// Pass ~/.huginn/secrets.json for production; use t.TempDir() in tests.
func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

// DefaultFileStorePath returns ~/.huginn/secrets.json.
func DefaultFileStorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("secrets file store: get home dir: %w", err)
	}
	return filepath.Join(home, ".huginn", "secrets.json"), nil
}

// Get returns the secret for the given slot, or an error if not found.
func (f *FileStore) Get(slot string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	data, err := f.load()
	if err != nil {
		return "", err
	}
	v, ok := data.Slots[slot]
	if !ok {
		return "", fmt.Errorf("secrets file: slot %q not found", slot)
	}
	return v, nil
}

// Set stores a secret for the given slot.
func (f *FileStore) Set(slot, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	data, err := f.load()
	if err != nil {
		// If the file doesn't exist yet, start fresh.
		data = &fileStoreData{Slots: make(map[string]string)}
	}
	if data.Slots == nil {
		data.Slots = make(map[string]string)
	}
	data.Slots[slot] = value
	return f.save(data)
}

// Delete removes the secret for the given slot.
func (f *FileStore) Delete(slot string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	data, err := f.load()
	if err != nil {
		return nil // nothing to delete
	}
	delete(data.Slots, slot)
	return f.save(data)
}

// Has reports whether a secret is stored for the given slot.
func (f *FileStore) Has(slot string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	data, err := f.load()
	if err != nil {
		return false
	}
	_, ok := data.Slots[slot]
	return ok
}

// load reads and parses the secrets file. Caller must hold f.mu.
func (f *FileStore) load() (*fileStoreData, error) {
	raw, err := os.ReadFile(f.path)
	if err != nil {
		return nil, fmt.Errorf("secrets file: read %s: %w", f.path, err)
	}
	var data fileStoreData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("secrets file: parse %s: %w", f.path, err)
	}
	if data.Slots == nil {
		data.Slots = make(map[string]string)
	}
	return &data, nil
}

// save atomically writes the secrets file with 0600 permissions.
// Uses a temp file + rename to prevent partial writes on power loss.
// Caller must hold f.mu.
func (f *FileStore) save(data *fileStoreData) error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o750); err != nil {
		return fmt.Errorf("secrets file: mkdir: %w", err)
	}

	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("secrets file: marshal: %w", err)
	}

	// Write to a temp file with 0600 permissions from creation (not post-chmod).
	tmp := f.path + ".tmp"
	fh, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("secrets file: create tmp: %w", err)
	}
	if _, err := fh.Write(raw); err != nil {
		fh.Close()
		os.Remove(tmp)
		return fmt.Errorf("secrets file: write tmp: %w", err)
	}
	// fsync to protect against partial-write on power loss.
	if err := fh.Sync(); err != nil {
		fh.Close()
		os.Remove(tmp)
		return fmt.Errorf("secrets file: fsync tmp: %w", err)
	}
	if err := fh.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("secrets file: close tmp: %w", err)
	}
	if err := os.Rename(tmp, f.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("secrets file: rename: %w", err)
	}
	return nil
}
