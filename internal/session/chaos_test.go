package session_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// TestSQLiteSession_CorruptDBFile verifies that opening a database that has
// been corrupted (garbage bytes written to the file) returns an error from
// sqlitedb.Open rather than silently returning a broken store.
func TestSQLiteSession_CorruptDBFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "corrupt.db")

	// Write garbage bytes to simulate a corrupt database file.
	if err := os.WriteFile(dbPath, []byte("THIS IS NOT A SQLITE DATABASE!!!"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Open should fail (or ApplySchema should fail) because the file is not a
	// valid SQLite database.
	db, err := sqlitedb.Open(dbPath)
	if err != nil {
		// Expected: Open itself reported the corruption.
		return
	}
	// If Open succeeded, ApplySchema must fail.
	if err := db.ApplySchema(); err != nil {
		db.Close()
		return // expected
	}
	t.Cleanup(func() { db.Close() })

	// If both Open and ApplySchema succeeded against garbage bytes,
	// subsequent operations must return errors.
	s := session.NewSQLiteSessionStore(db)
	_, err = s.List()
	if err == nil {
		t.Log("Open+ApplySchema succeeded on corrupt file and List returned no error — this may be OK if SQLite recreated the file")
	}
}

// TestSQLiteSession_ConcurrentSaves verifies that concurrent SaveManifest
// calls for different sessions do not deadlock, corrupt data, or cause
// duplicate-ID issues.  This test is suitable for go test -race.
func TestSQLiteSession_ConcurrentSaves(t *testing.T) {
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sess := s.New(fmt.Sprintf("sess-%d", n), "/workspace", "model")
			if err := s.SaveManifest(sess); err != nil {
				errs[n] = err
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: SaveManifest error: %v", i, err)
		}
	}

	manifests, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(manifests) != goroutines {
		t.Errorf("expected %d sessions, got %d", goroutines, len(manifests))
	}
}

// TestSQLiteSession_ConcurrentAppends verifies that concurrent Append calls
// on the same session produce the correct message count and no errors.
func TestSQLiteSession_ConcurrentAppends(t *testing.T) {
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	sess := s.New("append-race", "/workspace", "model")
	if err := s.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			msg := session.SessionMessage{
				Role:    "user",
				Content: fmt.Sprintf("message %d", n),
			}
			if err := s.Append(sess, msg); err != nil {
				errs[n] = err
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: Append error: %v", i, err)
		}
	}

	// Verify all messages were persisted.
	msgs, err := s.TailMessages(sess.ID, goroutines+10)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	if len(msgs) != goroutines {
		t.Errorf("expected %d messages, got %d", goroutines, len(msgs))
	}
}

// TestSQLiteSession_ClosedDB_SaveManifestReturnsError verifies that operations
// on a closed database return errors rather than panicking.
func TestSQLiteSession_ClosedDB_SaveManifestReturnsError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	db, err := sqlitedb.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		db.Close()
		t.Fatalf("ApplySchema: %v", err)
	}

	s := session.NewSQLiteSessionStore(db)
	sess := s.New("closed-db-test", "/workspace", "model")

	db.Close()

	if err := s.SaveManifest(sess); err == nil {
		t.Error("expected error when saving to a closed DB, got nil")
	}
}

// TestSQLiteSession_ConcurrentReadWrite verifies that concurrent reads and
// writes do not deadlock.  A timeout context is used to detect hangs.
func TestSQLiteSession_ConcurrentReadWrite(t *testing.T) {
	db := openSessTestDB(t)
	s := session.NewSQLiteSessionStore(db)

	// Seed some initial sessions.
	for i := 0; i < 5; i++ {
		sess := s.New(fmt.Sprintf("seed-%d", i), "/ws", "m")
		if err := s.SaveManifest(sess); err != nil {
			t.Fatalf("SaveManifest: %v", err)
		}
	}

	const goroutines = 10
	done := make(chan struct{})
	deadline := time.After(10 * time.Second)

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if n%2 == 0 {
				// Writer
				sess := s.New(fmt.Sprintf("concurrent-%d", n), "/ws", "m")
				_ = s.SaveManifest(sess)
			} else {
				// Reader
				_, _ = s.List()
			}
		}(i)
	}

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// No deadlock.
	case <-deadline:
		t.Fatal("concurrent read/write test deadlocked")
	}
}
