package spaces_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// openWSForMigration returns a fresh *sqlitedb.DB and its associated
// WorkstreamStore so that tests can call db.Migrate() a second time.
func openWSForMigration(t *testing.T) (*sqlitedb.DB, *spaces.WorkstreamStore) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "ws_mig.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	if err := db.Migrate(spaces.WorkstreamMigrations()); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, spaces.NewWorkstreamStore(db)
}

// ── Create edge cases ─────────────────────────────────────────────────────────

// TestWorkstream_Create_WhitespaceOnlyName verifies that a whitespace-only name
// ("   ") is accepted by Create and stored exactly as provided. The source only
// rejects the empty string ""; it performs no trimming or further validation.
func TestWorkstream_Create_WhitespaceOnlyName(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	ws, err := store.Create(ctx, "   ", "desc")
	if err != nil {
		// If the implementation is tightened in the future to reject
		// whitespace-only names this test should be updated accordingly.
		t.Fatalf("Create with whitespace-only name returned unexpected error: %v", err)
	}
	if ws.Name != "   " {
		t.Errorf("expected name %q to be stored verbatim, got %q", "   ", ws.Name)
	}

	// Verify it round-trips through Get correctly.
	got, err := store.Get(ctx, ws.ID)
	if err != nil {
		t.Fatalf("Get after Create: %v", err)
	}
	if got.Name != "   " {
		t.Errorf("Get returned name %q, want %q", got.Name, "   ")
	}
}

// TestWorkstream_Create_UnicodeName verifies that names containing emoji, CJK
// characters, and accented characters are stored and retrieved without
// corruption.
func TestWorkstream_Create_UnicodeName(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	name := "🔥 陈 café"
	ws, err := store.Create(ctx, name, "unicode description")
	if err != nil {
		t.Fatalf("Create with unicode name: %v", err)
	}
	if ws.Name != name {
		t.Errorf("Create returned name %q, want %q", ws.Name, name)
	}

	got, err := store.Get(ctx, ws.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != name {
		t.Errorf("Get returned name %q, want %q", got.Name, name)
	}

	// Verify it also appears in List with the correct name.
	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, w := range list {
		if w.Name == name {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("unicode-named workstream not found in List results")
	}
}

// TestWorkstream_Create_DuplicateNames verifies that the store allows two
// workstreams to share the same name (no UNIQUE constraint on the name column).
func TestWorkstream_Create_DuplicateNames(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	const sharedName = "shared-name"
	ws1, err := store.Create(ctx, sharedName, "first")
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	ws2, err := store.Create(ctx, sharedName, "second")
	if err != nil {
		t.Fatalf("Create second with duplicate name: %v", err)
	}
	if ws1.ID == ws2.ID {
		t.Errorf("duplicate-name workstreams must have distinct IDs")
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	count := 0
	for _, w := range list {
		if w.Name == sharedName {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 workstreams with name %q, got %d", sharedName, count)
	}
}

// ── List ordering ─────────────────────────────────────────────────────────────

// TestWorkstream_List_Ordering creates 3 workstreams in sequence and verifies
// that List returns them ordered by created_at DESC (newest first).
func TestWorkstream_List_Ordering(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	names := []string{"first", "second", "third"}
	var ids []string
	for _, n := range names {
		ws, err := store.Create(ctx, n, "")
		if err != nil {
			t.Fatalf("Create %q: %v", n, err)
		}
		ids = append(ids, ws.ID)
		// Introduce a small sleep so that created_at timestamps are strictly
		// ordered even under SQLite's millisecond-precision timestamp.
		time.Sleep(2 * time.Millisecond)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 workstreams, got %d", len(list))
	}

	// Expected: newest (third) first, oldest (first) last.
	wantOrder := []string{ids[2], ids[1], ids[0]}
	for i, want := range wantOrder {
		if list[i].ID != want {
			t.Errorf("list[%d].ID = %q, want %q", i, list[i].ID, want)
		}
	}
}

// ── Get edge cases ────────────────────────────────────────────────────────────

// TestWorkstream_Get_EmptyID verifies that Get with an empty ID string returns
// an error and does not panic.
func TestWorkstream_Get_EmptyID(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	_, err := store.Get(ctx, "")
	if err == nil {
		t.Fatal("expected error for Get(\"\"), got nil")
	}
}

// ── Delete edge cases ─────────────────────────────────────────────────────────

// TestWorkstream_Delete_Idempotent verifies that deleting the same ID twice
// returns an error on the second call (the row no longer exists).
func TestWorkstream_Delete_Idempotent(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	ws, err := store.Create(ctx, "double-delete", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Delete(ctx, ws.ID); err != nil {
		t.Fatalf("first Delete: %v", err)
	}

	err = store.Delete(ctx, ws.ID)
	if err == nil {
		t.Fatal("expected error on second Delete of same ID, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got %q", err.Error())
	}
}

// ── TagSession edge cases ─────────────────────────────────────────────────────

// TestWorkstream_TagSession_NonexistentWorkstream verifies that TagSession
// returns an error when the workstream ID does not exist. Because the DB is
// opened with PRAGMA foreign_keys = ON the REFERENCES constraint on
// workstream_sessions.workstream_id is enforced.
func TestWorkstream_TagSession_NonexistentWorkstream(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	err := store.TagSession(ctx, "does-not-exist", "sess-999")
	if err == nil {
		t.Fatal("expected foreign-key error for nonexistent workstream, got nil")
	}
}

// TestWorkstream_TagSession_Idempotency verifies that calling TagSession 5
// times with the same (workstream, session) pair results in exactly 1 entry
// in ListSessions (INSERT OR IGNORE semantics).
func TestWorkstream_TagSession_Idempotency(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	ws, err := store.Create(ctx, "idem-project", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for i := 0; i < 5; i++ {
		if err := store.TagSession(ctx, ws.ID, "sess-idem"); err != nil {
			t.Fatalf("TagSession call %d: %v", i+1, err)
		}
	}

	ids, err := store.ListSessions(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(ids) != 1 {
		t.Errorf("expected exactly 1 session after 5 idempotent TagSession calls, got %d", len(ids))
	}
	if ids[0] != "sess-idem" {
		t.Errorf("expected session ID %q, got %q", "sess-idem", ids[0])
	}
}

// ── ListSessions ordering ─────────────────────────────────────────────────────

// TestWorkstream_ListSessions_Ordering tags 5 sessions and verifies that
// ListSessions returns them in tagged_at DESC order (most-recently-tagged
// session first).
func TestWorkstream_ListSessions_Ordering(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	ws, err := store.Create(ctx, "ordering-project", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	sessionIDs := []string{"sess-1", "sess-2", "sess-3", "sess-4", "sess-5"}
	for _, sid := range sessionIDs {
		if err := store.TagSession(ctx, ws.ID, sid); err != nil {
			t.Fatalf("TagSession %q: %v", sid, err)
		}
		// Small sleep to ensure strictly ordered tagged_at timestamps.
		time.Sleep(2 * time.Millisecond)
	}

	ids, err := store.ListSessions(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(ids) != 5 {
		t.Fatalf("expected 5 sessions, got %d", len(ids))
	}

	// Expected order: sess-5 (newest) … sess-1 (oldest).
	wantOrder := []string{"sess-5", "sess-4", "sess-3", "sess-2", "sess-1"}
	for i, want := range wantOrder {
		if ids[i] != want {
			t.Errorf("ids[%d] = %q, want %q", i, ids[i], want)
		}
	}
}

// ── Migration idempotency ─────────────────────────────────────────────────────

// TestWorkstream_Migration_Idempotent verifies that calling db.Migrate with
// WorkstreamMigrations() a second time succeeds without error. The migration
// runner tracks applied migrations in _migrations and skips already-applied
// ones.
func TestWorkstream_Migration_Idempotent(t *testing.T) {
	db, _ := openWSForMigration(t)

	// Second call: all migrations should already be recorded as done.
	if err := db.Migrate(spaces.WorkstreamMigrations()); err != nil {
		t.Fatalf("second Migrate call failed: %v", err)
	}
}

// ── Concurrent operations ─────────────────────────────────────────────────────

// TestWorkstream_ConcurrentTagSessions spawns 5 goroutines each tagging a
// distinct session ID to the same workstream concurrently, then verifies that
// all 5 session IDs appear in ListSessions.
func TestWorkstream_ConcurrentTagSessions(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	ws, err := store.Create(ctx, "concurrent-project", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	const n = 5
	errCh := make(chan error, n)
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			sid := fmt.Sprintf("concurrent-sess-%d", i)
			if err := store.TagSession(ctx, ws.ID, sid); err != nil {
				errCh <- fmt.Errorf("goroutine %d TagSession: %w", i, err)
			}
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}

	ids, err := store.ListSessions(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(ids) != n {
		t.Errorf("expected %d sessions after concurrent tagging, got %d", n, len(ids))
	}
}

// ── Delete cascade verification ───────────────────────────────────────────────

// TestWorkstream_Delete_CascadeVerification tags 5 sessions to a workstream,
// deletes the workstream, and verifies that ListSessions returns empty because
// the workstream_sessions rows were removed by the ON DELETE CASCADE constraint.
func TestWorkstream_Delete_CascadeVerification(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	ws, err := store.Create(ctx, "cascade-verify", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for i := 0; i < 5; i++ {
		sid := fmt.Sprintf("cascade-sess-%d", i)
		if err := store.TagSession(ctx, ws.ID, sid); err != nil {
			t.Fatalf("TagSession %q: %v", sid, err)
		}
	}

	// Confirm sessions are present before delete.
	before, err := store.ListSessions(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ListSessions before delete: %v", err)
	}
	if len(before) != 5 {
		t.Fatalf("expected 5 sessions before delete, got %d", len(before))
	}

	if err := store.Delete(ctx, ws.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// After cascading delete, the session rows must be gone.
	after, err := store.ListSessions(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ListSessions after cascade delete: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("expected 0 sessions after cascade delete, got %d: %v", len(after), after)
	}
}
