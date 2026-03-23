package threadmgr

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// openTestDB opens a fresh in-memory-backed SQLite DB for testing and runs
// ApplySchema + threadmgr.Migrations(). Returns the DB and a cleanup func.
func openTestDB(t *testing.T) (*sqlitedb.DB, func()) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	db, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatalf("openTestDB: open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		db.Close()
		t.Fatalf("openTestDB: ApplySchema: %v", err)
	}
	if err := db.Migrate(Migrations()); err != nil {
		db.Close()
		t.Fatalf("openTestDB: Migrate: %v", err)
	}
	return db, func() {
		db.Close()
		os.RemoveAll(dir)
	}
}

func makeThread(id, sessionID, agentID string) *Thread {
	now := time.Now().UTC().Truncate(time.Second)
	return &Thread{
		ID:              id,
		SessionID:       sessionID,
		AgentID:         agentID,
		Task:            "do something",
		Status:          StatusQueued,
		ParentMessageID: "msg-parent-1",
		CreatedAt:       now,
		StartedAt:       now,
		TokenBudget:     100,
		TokensUsed:      0,
		InputCh:         make(chan string, 1),
	}
}

// TestSaveAndLoadRoundtrip ensures a thread saved via SaveThread comes back
// intact from LoadThreads.
func TestSaveAndLoadRoundtrip(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	store := NewSQLiteThreadStore(db)
	ctx := context.Background()

	sess := "sess-abc"
	th := makeThread("t-1", sess, "agent-alice")

	if err := store.SaveThread(ctx, th); err != nil {
		t.Fatalf("SaveThread: %v", err)
	}

	threads, err := store.LoadThreads(ctx, sess)
	if err != nil {
		t.Fatalf("LoadThreads: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(threads))
	}

	got := threads[0]
	if got.ID != th.ID {
		t.Errorf("ID: got %q, want %q", got.ID, th.ID)
	}
	if got.SessionID != th.SessionID {
		t.Errorf("SessionID: got %q, want %q", got.SessionID, th.SessionID)
	}
	if got.AgentID != th.AgentID {
		t.Errorf("AgentID: got %q, want %q", got.AgentID, th.AgentID)
	}
	if got.Task != th.Task {
		t.Errorf("Task: got %q, want %q", got.Task, th.Task)
	}
	if got.Status != th.Status {
		t.Errorf("Status: got %q, want %q", got.Status, th.Status)
	}
	if got.ParentMessageID != th.ParentMessageID {
		t.Errorf("ParentMessageID: got %q, want %q", got.ParentMessageID, th.ParentMessageID)
	}
	if got.TokenBudget != th.TokenBudget {
		t.Errorf("TokenBudget: got %d, want %d", got.TokenBudget, th.TokenBudget)
	}
}

// TestSaveUpsert ensures a second SaveThread call updates the existing row.
func TestSaveUpsert(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	store := NewSQLiteThreadStore(db)
	ctx := context.Background()

	sess := "sess-upsert"
	th := makeThread("t-upsert", sess, "agent-bob")

	if err := store.SaveThread(ctx, th); err != nil {
		t.Fatalf("first SaveThread: %v", err)
	}

	// Mutate and save again.
	th.Status = StatusDone
	th.TokensUsed = 42
	now := time.Now().UTC()
	th.CompletedAt = now
	th.Summary = &FinishSummary{
		Summary:       "all done",
		Status:        "completed",
		FilesModified: []string{"foo.go"},
		KeyDecisions:  []string{"used approach X"},
		Artifacts:     []string{"artifact-1"},
	}

	if err := store.SaveThread(ctx, th); err != nil {
		t.Fatalf("second SaveThread: %v", err)
	}

	threads, err := store.LoadThreads(ctx, sess)
	if err != nil {
		t.Fatalf("LoadThreads: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(threads))
	}

	got := threads[0]
	if got.Status != StatusDone {
		t.Errorf("Status: got %q, want %q", got.Status, StatusDone)
	}
	if got.TokensUsed != 42 {
		t.Errorf("TokensUsed: got %d, want 42", got.TokensUsed)
	}
	if got.Summary == nil {
		t.Fatal("Summary is nil after upsert")
	}
	if got.Summary.Summary != "all done" {
		t.Errorf("Summary.Summary: got %q, want %q", got.Summary.Summary, "all done")
	}
	if got.Summary.Status != "completed" {
		t.Errorf("Summary.Status: got %q, want %q", got.Summary.Status, "completed")
	}
	if len(got.Summary.FilesModified) != 1 || got.Summary.FilesModified[0] != "foo.go" {
		t.Errorf("Summary.FilesModified: got %v, want [foo.go]", got.Summary.FilesModified)
	}
}

// TestUpdateStatusPersists verifies UpdateThreadStatus is reflected on reload.
func TestUpdateStatusPersists(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	store := NewSQLiteThreadStore(db)
	ctx := context.Background()

	sess := "sess-status"
	th := makeThread("t-status", sess, "agent-charlie")

	if err := store.SaveThread(ctx, th); err != nil {
		t.Fatalf("SaveThread: %v", err)
	}
	if err := store.UpdateThreadStatus(ctx, th.ID, string(StatusDone)); err != nil {
		t.Fatalf("UpdateThreadStatus: %v", err)
	}

	threads, err := store.LoadThreads(ctx, sess)
	if err != nil {
		t.Fatalf("LoadThreads: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(threads))
	}
	if threads[0].Status != StatusDone {
		t.Errorf("Status after update: got %q, want %q", threads[0].Status, StatusDone)
	}
}

// TestDeleteRemovesFromDB verifies DeleteThread removes the record.
func TestDeleteRemovesFromDB(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	store := NewSQLiteThreadStore(db)
	ctx := context.Background()

	sess := "sess-delete"
	th := makeThread("t-delete", sess, "agent-dave")

	if err := store.SaveThread(ctx, th); err != nil {
		t.Fatalf("SaveThread: %v", err)
	}
	if err := store.DeleteThread(ctx, th.ID); err != nil {
		t.Fatalf("DeleteThread: %v", err)
	}

	threads, err := store.LoadThreads(ctx, sess)
	if err != nil {
		t.Fatalf("LoadThreads: %v", err)
	}
	if len(threads) != 0 {
		t.Errorf("expected 0 threads after delete, got %d", len(threads))
	}
}

// TestDeleteNonExistentIsNoop verifies that deleting a missing thread is safe.
func TestDeleteNonExistentIsNoop(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	store := NewSQLiteThreadStore(db)
	ctx := context.Background()

	if err := store.DeleteThread(ctx, "t-does-not-exist"); err != nil {
		t.Errorf("DeleteThread non-existent: expected nil, got %v", err)
	}
}

// TestLoadThreadsReturnsCorrectSubset ensures threads from different sessions
// don't bleed into each other's LoadThreads results.
func TestLoadThreadsReturnsCorrectSubset(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	store := NewSQLiteThreadStore(db)
	ctx := context.Background()

	sessA := "sess-A"
	sessB := "sess-B"

	for i, id := range []string{"t-a1", "t-a2", "t-a3"} {
		th := makeThread(id, sessA, "agent")
		th.Task = "task for A"
		_ = i
		if err := store.SaveThread(ctx, th); err != nil {
			t.Fatalf("SaveThread %s: %v", id, err)
		}
	}
	for _, id := range []string{"t-b1", "t-b2"} {
		th := makeThread(id, sessB, "agent")
		th.Task = "task for B"
		if err := store.SaveThread(ctx, th); err != nil {
			t.Fatalf("SaveThread %s: %v", id, err)
		}
	}

	aThreads, err := store.LoadThreads(ctx, sessA)
	if err != nil {
		t.Fatalf("LoadThreads A: %v", err)
	}
	if len(aThreads) != 3 {
		t.Errorf("expected 3 threads for sessA, got %d", len(aThreads))
	}

	bThreads, err := store.LoadThreads(ctx, sessB)
	if err != nil {
		t.Fatalf("LoadThreads B: %v", err)
	}
	if len(bThreads) != 2 {
		t.Errorf("expected 2 threads for sessB, got %d", len(bThreads))
	}
}

// TestLoadThreadsEmptySession returns an empty (non-nil) slice for an unknown session.
func TestLoadThreadsEmptySession(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	store := NewSQLiteThreadStore(db)
	ctx := context.Background()

	threads, err := store.LoadThreads(ctx, "sess-nonexistent")
	if err != nil {
		t.Fatalf("LoadThreads: %v", err)
	}
	if threads == nil {
		t.Error("expected non-nil slice for empty session")
	}
	if len(threads) != 0 {
		t.Errorf("expected 0 threads, got %d", len(threads))
	}
}

// TestConcurrentSavesNoCrash checks that concurrent SaveThread calls don't
// corrupt data or cause data races.
func TestConcurrentSavesNoCrash(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	store := NewSQLiteThreadStore(db)
	ctx := context.Background()

	const n = 20
	var wg sync.WaitGroup
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := "t-concurrent-" + string(rune('a'+idx%26)) + "-" + time.Now().Format("150405.000000000")
			th := makeThread(id, "sess-concurrent", "agent")
			errs[idx] = store.SaveThread(ctx, th)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: SaveThread error: %v", i, err)
		}
	}

	threads, err := store.LoadThreads(ctx, "sess-concurrent")
	if err != nil {
		t.Fatalf("LoadThreads: %v", err)
	}
	if len(threads) != n {
		t.Errorf("expected %d threads, got %d", n, len(threads))
	}
}

// TestManagerWithStoreSavesOnCreate verifies that after wiring a ThreadStore,
// Create() saves the thread to the DB synchronously — the thread must be visible
// in the store immediately after Create() returns, with no sleep or polling.
func TestManagerWithStoreSavesOnCreate(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	store := NewSQLiteThreadStore(db)
	tm := New()
	tm.SetStore(store)

	sess := "sess-mgr-create"

	thread, err := tm.Create(CreateParams{
		SessionID: sess,
		AgentID:   "agent-echo",
		Task:      "test task",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// SaveThread must have been called synchronously — no sleep required.
	// If this check fails it means Create() reverted to async persistence.
	ctx := context.Background()
	threads, err := store.LoadThreads(ctx, sess)
	if err != nil {
		t.Fatalf("LoadThreads: %v", err)
	}
	found := false
	for _, th := range threads {
		if th.ID == thread.ID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("thread %s not found in DB immediately after Create (save must be synchronous)", thread.ID)
	}
}

// TestLoadFromStorePopulatesManager ensures LoadFromStore rehydrates the in-memory map.
func TestLoadFromStorePopulatesManager(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	store := NewSQLiteThreadStore(db)
	ctx := context.Background()

	sess := "sess-rehydrate"

	// Pre-populate DB directly.
	th := makeThread("t-rehydrate-1", sess, "agent-fox")
	if err := store.SaveThread(ctx, th); err != nil {
		t.Fatalf("SaveThread: %v", err)
	}

	// Fresh manager with no in-memory threads.
	tm := New()
	tm.SetStore(store)

	if err := tm.LoadFromStore(ctx, sess); err != nil {
		t.Fatalf("LoadFromStore: %v", err)
	}

	got, ok := tm.Get(th.ID)
	if !ok {
		t.Fatalf("thread %s not found in manager after LoadFromStore", th.ID)
	}
	if got.AgentID != th.AgentID {
		t.Errorf("AgentID: got %q, want %q", got.AgentID, th.AgentID)
	}
}

// TestLoadFromStoreNoOpWhenNoStore ensures LoadFromStore returns nil when store is nil.
func TestLoadFromStoreNoOpWhenNoStore(t *testing.T) {
	tm := New() // no store set
	if err := tm.LoadFromStore(context.Background(), "any-session"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// TestMigrationIdempotent runs Migrate twice and verifies no error.
func TestMigrationIdempotent(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	// The second call should detect migrations already applied and be a no-op.
	if err := db.Migrate(Migrations()); err != nil {
		t.Errorf("second Migrate run: %v", err)
	}
}

// TestTimeoutPersistRoundtrip verifies that a thread's Timeout field (time.Duration)
// is stored as nanoseconds in the DB and correctly restored on LoadThreads.
func TestTimeoutPersistRoundtrip(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	store := NewSQLiteThreadStore(db)
	ctx := context.Background()

	const wantTimeout = 5 * time.Minute

	sess := "sess-timeout-rt"
	th := makeThread("t-timeout-rt", sess, "agent-timeout")
	th.Timeout = wantTimeout

	if err := store.SaveThread(ctx, th); err != nil {
		t.Fatalf("SaveThread: %v", err)
	}

	threads, err := store.LoadThreads(ctx, sess)
	if err != nil {
		t.Fatalf("LoadThreads: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(threads))
	}

	got := threads[0]
	if got.Timeout != wantTimeout {
		t.Errorf("Timeout: got %v, want %v", got.Timeout, wantTimeout)
	}
}
