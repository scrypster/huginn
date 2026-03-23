package session_test

import (
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/session"
)

// TestNewID_UniqueUnder100Goroutines verifies that NewID generates unique IDs
// when called concurrently from 100 goroutines.
// ULIDs are monotonically increasing with per-millisecond entropy,
// so collisions would indicate a broken entropy source or mutex issue.
func TestNewID_UniqueUnder100Goroutines(t *testing.T) {
	const goroutines = 100
	ids := make([]string, goroutines)
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ids[n] = session.NewID()
		}(i)
	}
	wg.Wait()

	seen := make(map[string]int, goroutines)
	for i, id := range ids {
		if id == "" {
			t.Errorf("goroutine %d produced an empty ID", i)
			continue
		}
		if prev, ok := seen[id]; ok {
			t.Errorf("duplicate ULID %q from goroutines %d and %d", id, prev, i)
		}
		seen[id] = i
	}
}

// TestNewID_NotEmpty verifies that a single call to NewID returns a non-empty string.
func TestNewID_NotEmpty(t *testing.T) {
	id := session.NewID()
	if id == "" {
		t.Error("NewID returned empty string")
	}
}

// TestNewID_LengthIsCorrect verifies that ULID strings are exactly 26 characters.
// This is the canonical ULID format: 10 chars timestamp + 16 chars randomness.
func TestNewID_LengthIsCorrect(t *testing.T) {
	for i := 0; i < 20; i++ {
		id := session.NewID()
		if len(id) != 26 {
			t.Errorf("expected ULID length 26, got %d (id=%q)", len(id), id)
		}
	}
}

// TestNewID_MonotonicWithinGoroutine verifies that successive IDs within the
// same goroutine are monotonically non-decreasing (a ULID guarantee).
func TestNewID_MonotonicWithinGoroutine(t *testing.T) {
	ids := make([]string, 50)
	for i := range ids {
		ids[i] = session.NewID()
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] < ids[i-1] {
			t.Errorf("ULID not monotonic: ids[%d]=%q < ids[%d]=%q", i, ids[i], i-1, ids[i-1])
		}
	}
}

// TestSQLiteSession_ConcurrentULIDsAllUnique creates 100 sessions from 100
// goroutines and verifies that every session ID is unique.  This is a
// higher-level integration test that exercises both NewID and the session store.
func TestSQLiteSession_ConcurrentULIDsAllUnique(t *testing.T) {
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	const goroutines = 100
	ids := make([]string, goroutines)
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sess := s.New("concurrent-ulid", "/workspace", "model")
			ids[n] = sess.ID
			_ = s.SaveManifest(sess)
		}(i)
	}
	wg.Wait()

	seen := make(map[string]int, goroutines)
	for i, id := range ids {
		if id == "" {
			t.Errorf("goroutine %d produced an empty session ID", i)
			continue
		}
		if prev, ok := seen[id]; ok {
			t.Errorf("duplicate session ID %q from goroutines %d and %d", id, prev, i)
		}
		seen[id] = i
	}
}

// TestSQLiteSession_ConcurrentReadNoDeadlock verifies that 50 concurrent
// List calls on a non-empty store complete without deadlocking.
func TestSQLiteSession_ConcurrentReadNoDeadlock(t *testing.T) {
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	// Seed 5 sessions.
	for i := 0; i < 5; i++ {
		sess := s.New("seed", "/ws", "m")
		if err := s.SaveManifest(sess); err != nil {
			t.Fatalf("SaveManifest: %v", err)
		}
	}

	const goroutines = 50
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.List()
			if err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent List error: %v", err)
	}
}
