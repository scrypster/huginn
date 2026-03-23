package session_test

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// openConcurrentTestDB opens a file-based SQLite DB, applies the schema, and
// registers a cleanup. Uses a temp dir so tests do not clobber each other.
func openConcurrentTestDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "concurrent-test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		db.Close()
		t.Fatalf("ApplySchema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestSaveManifestConcurrent verifies that concurrent calls to SaveManifest for
// the same session ID (using different Session instances with the same ID but
// each updated safely via their own lock) do not produce DB errors. This is the
// core scenario the per-session mutex (Fix 6) guards against: two goroutines
// racing to upsert the same row.
func TestSaveManifestConcurrent(t *testing.T) {
	db := openConcurrentTestDB(t)
	store := session.NewSQLiteSessionStore(db)

	// Create a canonical session and persist it once.
	canonical := store.New("concurrent test", "/tmp", "claude-3")
	canonical.Manifest.UpdatedAt = time.Now().UTC()
	if err := store.SaveManifest(canonical); err != nil {
		t.Fatalf("initial save: %v", err)
	}

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// Each goroutine creates its own Session copy with the same ID
			// so there is no shared struct (no intra-process race) while still
			// exercising the per-session DB serialisation.
			s := store.New(fmt.Sprintf("title-%d", n), "/tmp", "claude-3")
			s.ID = canonical.ID
			s.Manifest.ID = canonical.ID
			s.Manifest.SessionID = canonical.ID
			s.Manifest.UpdatedAt = time.Now().UTC()
			if err := store.SaveManifest(s); err != nil {
				errs <- err
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent SaveManifest error: %v", err)
	}

	// Verify the session can still be loaded.
	loaded, err := store.Load(canonical.ID)
	if err != nil {
		t.Fatalf("load after concurrent saves: %v", err)
	}
	if loaded.ID != canonical.ID {
		t.Errorf("loaded session ID mismatch: got %q, want %q", loaded.ID, canonical.ID)
	}
}

// TestSaveManifestConcurrent_Distinct verifies concurrent SaveManifest across
// distinct sessions does not interfere (no shared-lock contention stalls all).
func TestSaveManifestConcurrent_Distinct(t *testing.T) {
	db := openConcurrentTestDB(t)
	store := session.NewSQLiteSessionStore(db)

	const count = 10
	canonicals := make([]*session.Session, count)
	for i := 0; i < count; i++ {
		s := store.New(fmt.Sprintf("initial-%d", i), "/tmp", "claude-3")
		s.Manifest.UpdatedAt = time.Now().UTC()
		if err := store.SaveManifest(s); err != nil {
			t.Fatalf("initial save %d: %v", i, err)
		}
		canonicals[i] = s
	}

	var wg sync.WaitGroup
	errs := make(chan error, count*5)

	for i := 0; i < count; i++ {
		canonicalID := canonicals[i].ID
		for j := 0; j < 5; j++ {
			wg.Add(1)
			go func(title string) {
				defer wg.Done()
				// Use a fresh Session object with the canonical ID.
				s := store.New(title, "/tmp", "claude-3")
				s.ID = canonicalID
				s.Manifest.ID = canonicalID
				s.Manifest.SessionID = canonicalID
				s.Manifest.UpdatedAt = time.Now().UTC()
				if err := store.SaveManifest(s); err != nil {
					errs <- err
				}
			}(fmt.Sprintf("title-%d-%d", i, j))
		}
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent SaveManifest error: %v", err)
	}
}
