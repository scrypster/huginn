package models

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LockEntry records a model that is installed on disk.
type LockEntry struct {
	Name        string    `json:"name"`
	Filename    string    `json:"filename"`
	Path        string    `json:"path"`
	SHA256      string    `json:"sha256"`
	SizeBytes   int64     `json:"size_bytes"`
	InstalledAt time.Time `json:"installed_at"`
}

// Store manages ~/.huginn/models/ and the lock file.
// mu guards all read-modify-write operations on the lock file to prevent
// concurrent installs or deletions from corrupting the JSON index.
type Store struct {
	mu       sync.Mutex
	dir      string // ~/.huginn/models/
	lockPath string // ~/.huginn/models.lock.json
}

// NewStore creates a Store rooted at huginnDir (e.g. ~/.huginn).
func NewStore(huginnDir string) (*Store, error) {
	dir := filepath.Join(huginnDir, "models")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	return &Store{
		dir:      dir,
		lockPath: filepath.Join(huginnDir, "models.lock.json"),
	}, nil
}

// Installed returns all recorded model entries.
func (s *Store) Installed() (map[string]LockEntry, error) {
	data, err := os.ReadFile(s.lockPath)
	if os.IsNotExist(err) {
		return make(map[string]LockEntry), nil
	}
	if err != nil {
		return nil, err
	}
	var entries map[string]LockEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("lock file corrupted (delete %s to reset): %w", s.lockPath, err)
	}
	return entries, nil
}

// Record adds or updates a lock entry.
// The mutex serialises the read-modify-write so concurrent installs cannot
// interleave and corrupt the JSON index.
func (s *Store) Record(name string, entry LockEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.Installed()
	if err != nil {
		return err
	}
	entries[name] = entry
	return s.writeLock(entries)
}

// Remove deletes a lock entry (does not delete the file on disk).
// The mutex serialises the read-modify-write for the same reason as Record.
func (s *Store) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.Installed()
	if err != nil {
		return err
	}
	delete(entries, name)
	return s.writeLock(entries)
}

// ModelPath returns the full path where a model filename should be stored.
func (s *Store) ModelPath(filename string) string {
	return filepath.Join(s.dir, filename)
}

func (s *Store) writeLock(entries map[string]LockEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(s.lockPath, data, 0644)
}
