package relay

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/scrypster/huginn/internal/storage"
)

const sessionPrefix = "relay:session:"

// SessionMeta holds metadata about an active or recently active satellite session.
type SessionMeta struct {
	ID        string    `json:"id"`
	StartedAt time.Time `json:"started_at"`
	LastSeq   uint64    `json:"last_seq"`
	Status    string    `json:"status"` // "active", "completed", "failed"
}

// SessionStore is a Pebble-backed store for satellite session metadata.
type SessionStore struct {
	db *storage.Store
	mu sync.Mutex // protects NextSeq read-modify-write
}

// NewSessionStore creates a SessionStore backed by db.
func NewSessionStore(db *storage.Store) *SessionStore {
	return &SessionStore{db: db}
}

// Save writes or updates a session.
func (s *SessionStore) Save(sess SessionMeta) error {
	data, err := json.Marshal(sess)
	if err != nil {
		return fmt.Errorf("session_store: marshal: %w", err)
	}
	return s.db.DB().Set([]byte(sessionPrefix+sess.ID), data, &pebble.WriteOptions{Sync: true})
}

// Get retrieves a session by ID. Returns error if not found.
func (s *SessionStore) Get(id string) (SessionMeta, error) {
	val, closer, err := s.db.DB().Get([]byte(sessionPrefix + id))
	if err != nil {
		if err == pebble.ErrNotFound {
			return SessionMeta{}, fmt.Errorf("session_store: session not found: %s", id)
		}
		return SessionMeta{}, fmt.Errorf("session_store: get: %w", err)
	}
	defer closer.Close()

	var sess SessionMeta
	if err := json.Unmarshal(val, &sess); err != nil {
		return SessionMeta{}, fmt.Errorf("session_store: unmarshal: %w", err)
	}
	return sess, nil
}

// List returns all sessions.
func (s *SessionStore) List() ([]SessionMeta, error) {
	var sessions []SessionMeta
	prefix := []byte(sessionPrefix)

	iter, err := s.db.DB().NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: incrementLastByte(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("session_store: create iterator: %w", err)
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		var sess SessionMeta
		if err := json.Unmarshal(iter.Value(), &sess); err != nil {
			return nil, fmt.Errorf("session_store: unmarshal: %w", err)
		}
		sessions = append(sessions, sess)
	}
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("session_store: iterate: %w", err)
	}

	return sessions, nil
}

// ListActive returns sessions with status "active".
func (s *SessionStore) ListActive() ([]SessionMeta, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	var active []SessionMeta
	for _, sess := range all {
		if sess.Status == "active" {
			active = append(active, sess)
		}
	}
	return active, nil
}

// Delete removes a session by ID.
func (s *SessionStore) Delete(id string) error {
	return s.db.DB().Delete([]byte(sessionPrefix+id), &pebble.WriteOptions{Sync: true})
}

// NextSeq atomically increments LastSeq for the given session and returns
// the new sequence number. Returns error if session not found.
func (s *SessionStore) NextSeq(id string) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, err := s.Get(id)
	if err != nil {
		return 0, err
	}
	sess.LastSeq++
	if err := s.Save(sess); err != nil {
		return 0, err
	}
	return sess.LastSeq, nil
}
