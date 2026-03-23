package connections

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store persists Connection metadata to a JSON file on disk.
// All mutations are protected by a mutex and written atomically
// via a temp-file + rename pattern.
type Store struct {
	path  string
	mu    sync.RWMutex
	conns []Connection
}

// NewStore creates (or loads) a Store backed by the file at path.
// The directory containing path must already exist.
func NewStore(path string) (*Store, error) {
	s := &Store{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// List returns a shallow copy of all connections.
func (s *Store) List() ([]Connection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Connection, len(s.conns))
	copy(out, s.conns)
	return out, nil
}

// ListByProvider returns all connections whose Provider field matches p.
func (s *Store) ListByProvider(p Provider) ([]Connection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Connection
	for _, c := range s.conns {
		if c.Provider == p {
			out = append(out, c)
		}
	}
	return out, nil
}

// Get returns the connection with the given ID, or false if not found.
func (s *Store) Get(id string) (Connection, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.conns {
		if c.ID == id {
			return c, true
		}
	}
	return Connection{}, false
}

// Add appends a new connection and persists the store.
// Returns an error if a connection with the same ID already exists.
func (s *Store) Add(conn Connection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		if c.ID == conn.ID {
			return errAlreadyExists("Add", fmt.Sprintf("connection %q already exists", conn.ID))
		}
	}
	s.conns = append(s.conns, conn)
	if err := s.save(); err != nil {
		return errStorage("Add", fmt.Sprintf("persist connection %q", conn.ID), err)
	}
	return nil
}

// Remove deletes the connection with the given ID and persists the store.
// Returns an error if no such connection exists.
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i, c := range s.conns {
		if c.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return errNotFound("Remove", fmt.Sprintf("connection %q not found", id))
	}
	s.conns = append(s.conns[:idx], s.conns[idx+1:]...)
	if err := s.save(); err != nil {
		return errStorage("Remove", fmt.Sprintf("persist after removing %q", id), err)
	}
	return nil
}

// SetDefault moves the connection with the given ID to the front of its
// provider group, making it the default for that provider.
func (s *Store) SetDefault(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i, c := range s.conns {
		if c.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return errNotFound("SetDefault", fmt.Sprintf("connection %q not found", id))
	}
	provider := s.conns[idx].Provider
	// Find the first index in the slice that has the same provider
	firstIdx := -1
	for i, c := range s.conns {
		if c.Provider == provider {
			firstIdx = i
			break
		}
	}
	if firstIdx == idx {
		return nil // already default
	}
	// Move target to firstIdx by rotating the subslice
	conn := s.conns[idx]
	copy(s.conns[firstIdx+1:idx+1], s.conns[firstIdx:idx])
	s.conns[firstIdx] = conn
	return s.save()
}

// UpdateExpiry sets the ExpiresAt timestamp for the given connection ID.
func (s *Store) UpdateExpiry(id string, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.conns {
		if c.ID == id {
			s.conns[i].ExpiresAt = expiresAt
			return s.save()
		}
	}
	return errNotFound("UpdateExpiry", fmt.Sprintf("connection %q not found", id))
}

// UpdateRefreshError persists (or clears) the refresh failure state for a connection.
// Pass empty errMsg to clear a previously recorded error on successful refresh.
func (s *Store) UpdateRefreshError(id string, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.conns {
		if c.ID == id {
			if errMsg == "" {
				s.conns[i].RefreshFailedAt = nil
				s.conns[i].LastRefreshError = ""
			} else {
				now := time.Now().UTC()
				s.conns[i].RefreshFailedAt = &now
				s.conns[i].LastRefreshError = errMsg
			}
			return s.save()
		}
	}
	return errNotFound("UpdateRefreshError", fmt.Sprintf("connection %q not found", id))
}

// load reads the JSON file at s.path into s.conns.
// If the file does not exist the store starts empty (not an error).
func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		s.conns = []Connection{}
		return nil
	}
	if err != nil {
		return fmt.Errorf("connections store: read %s: %w", s.path, err)
	}
	var conns []Connection
	if err := json.Unmarshal(data, &conns); err != nil {
		return fmt.Errorf("connections store: parse %s: %w", s.path, err)
	}
	s.conns = conns
	return nil
}

// save writes s.conns atomically to s.path using a temp file + rename.
// Caller must hold s.mu (write lock).
func (s *Store) save() error {
	data, err := json.MarshalIndent(s.conns, "", "  ")
	if err != nil {
		return fmt.Errorf("connections store: marshal: %w", err)
	}

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".connections-*.json.tmp")
	if err != nil {
		return fmt.Errorf("connections store: create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("connections store: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("connections store: close temp file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("connections store: rename: %w", err)
	}
	return nil
}
