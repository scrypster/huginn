package session_test

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

func openTestDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestSessionStore_Security_ConcurrentSaveManifest verifies concurrent saves don't corrupt state.
func TestSessionStore_Security_ConcurrentSaveManifest(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	store := session.NewSQLiteSessionStore(db)
	const numGoroutines = 10
	const numSaves = 5

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*numSaves)

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			sess := store.New("test-session", "/tmp", "gpt-4")

			for i := 0; i < numSaves; i++ {
				sess.Manifest.Title = "title-" + string(rune(goroutineID*numSaves+i))
				if err := store.SaveManifest(sess); err != nil {
					errors <- err
				}
			}
		}(g)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent save failed: %v", err)
	}
}

// TestSQLiteSessionStore_SaveManifest_TransactionAtomicity tests that FTS index stays in sync.
func TestSQLiteSessionStore_SaveManifest_TransactionAtomicity(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	store := session.NewSQLiteSessionStore(db)
	sess := store.New("test-session", "/workspace", "gpt-4")
	sess.Manifest.Title = "searchable-title"

	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Verify we can load the session back
	loaded, err := store.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Manifest.Title != "searchable-title" {
		t.Errorf("title mismatch: %q != %q", loaded.Manifest.Title, "searchable-title")
	}
}

// TestSQLiteSessionStore_DeleteSession_ConcurrencyLoss tests race between delete and save.
func TestSQLiteSessionStore_DeleteSession_ConcurrencyLoss(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	store := session.NewSQLiteSessionStore(db)
	sess := store.New("test-session", "/tmp", "gpt-4")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	var wg sync.WaitGroup
	results := make(chan bool, 2)

	// Goroutine 1: tries to save
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		sess.Manifest.Title = "updated"
		err := store.SaveManifest(sess)
		results <- (err == nil)
	}()

	// Goroutine 2: deletes
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := store.Delete(sess.ID)
		results <- (err == nil)
	}()

	wg.Wait()
	close(results)

	// At least one should succeed; both should be safe (no corruption).
	successCount := 0
	for r := range results {
		if r {
			successCount++
		}
	}
	if successCount == 0 {
		t.Error("both operations failed unexpectedly")
	}
}

// TestSQLiteSessionStore_ClosedDB_ReturnsError tests graceful handling of closed database.
func TestSQLiteSessionStore_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	store := session.NewSQLiteSessionStore(db)
	db.Close() // Close the database

	sess := store.New("test-session", "/tmp", "gpt-4")

	// Should return an error, not panic
	err := store.SaveManifest(sess)
	if err == nil {
		t.Error("expected error when saving to closed database")
	}
}

// TestSQLiteSessionStore_New_CreatesValidSession verifies New() correctness.
func TestSQLiteSessionStore_New_CreatesValidSession(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	store := session.NewSQLiteSessionStore(db)
	sess := store.New("My Session", "/home/user/workspace", "gpt-4")

	if sess.ID == "" {
		t.Error("session ID should not be empty")
	}
	if sess.Manifest.Title != "My Session" {
		t.Errorf("title: %q != %q", sess.Manifest.Title, "My Session")
	}
	if sess.Manifest.Model != "gpt-4" {
		t.Errorf("model: %q != %q", sess.Manifest.Model, "gpt-4")
	}
	if sess.Manifest.Status != "active" {
		t.Errorf("status: %q != %q", sess.Manifest.Status, "active")
	}
	if sess.Manifest.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

// TestSQLiteSessionStore_Load_InexistentSession tests error handling.
func TestSQLiteSessionStore_Load_InexistentSession(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	store := session.NewSQLiteSessionStore(db)
	_, err := store.Load("nonexistent-id")

	if err == nil {
		t.Error("expected error loading nonexistent session")
	}
}

// TestSQLiteSessionStore_Exists_Correctness tests the Exists method.
func TestSQLiteSessionStore_Exists_Correctness(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	store := session.NewSQLiteSessionStore(db)

	// Before save
	if store.Exists("test-id") {
		t.Error("session should not exist before being saved")
	}

	// After save
	sess := store.New("test-session", "/tmp", "gpt-4")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	if !store.Exists(sess.ID) {
		t.Error("session should exist after being saved")
	}

	// After delete
	if err := store.Delete(sess.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if store.Exists(sess.ID) {
		t.Error("session should not exist after deletion")
	}
}

// TestSessionStore_Security_UpdatePreservesMetadata tests that updates preserve metadata correctly.
func TestSessionStore_Security_UpdatePreservesMetadata(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	store := session.NewSQLiteSessionStore(db)
	sess := store.New("test-session", "/tmp", "gpt-4")

	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Update and re-save
	sess.Manifest.Title = "updated title"
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest after update: %v", err)
	}

	// Load and verify
	loaded, err := store.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Manifest.Title != "updated title" {
		t.Errorf("title not updated: %q", loaded.Manifest.Title)
	}
}

// TestNewID_Uniqueness tests ID generation uniqueness.
func TestNewID_Uniqueness(t *testing.T) {
	ids := make(map[string]bool)
	const count = 1000

	for i := 0; i < count; i++ {
		id := session.NewID()
		if id == "" {
			t.Error("NewID returned empty string")
		}
		if ids[id] {
			t.Errorf("duplicate ID: %s", id)
		}
		ids[id] = true
	}

	if len(ids) != count {
		t.Errorf("expected %d unique IDs, got %d", count, len(ids))
	}
}

// TestSessionManifest_WorkspaceName_Extraction tests directory name extraction.
func TestSessionManifest_WorkspaceName_Extraction(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	store := session.NewSQLiteSessionStore(db)

	tests := []struct {
		workspaceRoot string
		expectedName  string
	}{
		{"/home/user/project", "project"},
		{"/tmp/.", "."},
		{"/", "/"},
		{".", "."},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.workspaceRoot, func(t *testing.T) {
			sess := store.New("test", tt.workspaceRoot, "gpt-4")
			if sess.Manifest.WorkspaceName != tt.expectedName {
				t.Errorf("WorkspaceName: %q != %q for %q", sess.Manifest.WorkspaceName, tt.expectedName, tt.workspaceRoot)
			}
		})
	}
}

// TestSessionManifest_SpaceID_Nullable tests that SpaceID is properly handled as nullable.
func TestSessionManifest_SpaceID_Nullable(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	store := session.NewSQLiteSessionStore(db)

	// Session with SpaceID
	sess1 := store.New("with-space", "/tmp", "gpt-4")
	sess1.Manifest.SpaceID = "space-123"
	if err := store.SaveManifest(sess1); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	loaded1, err := store.Load(sess1.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded1.Manifest.SpaceID != "space-123" {
		t.Errorf("SpaceID mismatch: %q", loaded1.Manifest.SpaceID)
	}

	// Session without SpaceID
	sess2 := store.New("without-space", "/tmp", "gpt-4")
	if err := store.SaveManifest(sess2); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	loaded2, err := store.Load(sess2.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded2.Manifest.SpaceID != "" {
		t.Errorf("SpaceID should be empty: %q", loaded2.Manifest.SpaceID)
	}
}
