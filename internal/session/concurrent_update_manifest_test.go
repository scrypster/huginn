package session_test

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// TestConcurrentUpdateManifest verifies that concurrent UpdateManifest calls
// do not race. Run with -race flag.
func TestConcurrentUpdateManifest(t *testing.T) {
	t.Parallel()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	store := session.NewSQLiteSessionStore(db)

	// Create a session.
	sess := store.New("Race Test", "/tmp", "model-x")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Concurrently update the manifest title from multiple goroutines.
	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			err := store.UpdateManifest(sess.ID, func(ps *session.PersistentSession) {
				ps.Title = fmt.Sprintf("Updated-%d", i)
			})
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("UpdateManifest error: %v", err)
	}

	// Verify the session can still be loaded.
	ps, err := store.LoadManifest(sess.ID)
	if err != nil {
		t.Fatalf("LoadManifest after concurrent updates: %v", err)
	}
	if ps.Title == "" {
		t.Error("expected non-empty title after concurrent updates")
	}
}
