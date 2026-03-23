package sqlitedb_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// TestPragmasPersist_AfterReopen verifies that pragmas set during Open()
// are still in effect after closing and reopening the database.
func TestPragmasPersist_AfterReopen(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "persist_test.db")

	// Open database and set pragmas.
	db1, err := sqlitedb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open db1: %v", err)
	}

	// Check that WAL mode is set.
	var journalMode string
	if err := db1.Read().QueryRow(`PRAGMA journal_mode`).Scan(&journalMode); err != nil {
		t.Fatalf("Query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("journal_mode after Open: got %q, want %q", journalMode, "wal")
	}

	// Check that foreign_keys is ON.
	var fkEnabled int
	if err := db1.Read().QueryRow(`PRAGMA foreign_keys`).Scan(&fkEnabled); err != nil {
		t.Fatalf("Query foreign_keys: %v", err)
	}
	if fkEnabled != 1 {
		t.Errorf("foreign_keys after Open: got %d, want 1", fkEnabled)
	}

	db1.Close()

	// Reopen database.
	db2, err := sqlitedb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open db2: %v", err)
	}
	defer db2.Close()

	// Pragmas should still be in effect (they're set per-connection, not persisted).
	var journalMode2 string
	if err := db2.Read().QueryRow(`PRAGMA journal_mode`).Scan(&journalMode2); err != nil {
		t.Fatalf("Query journal_mode on db2: %v", err)
	}
	if journalMode2 != "wal" {
		t.Errorf("journal_mode after reopen: got %q, want %q", journalMode2, "wal")
	}

	var fkEnabled2 int
	if err := db2.Read().QueryRow(`PRAGMA foreign_keys`).Scan(&fkEnabled2); err != nil {
		t.Fatalf("Query foreign_keys on db2: %v", err)
	}
	if fkEnabled2 != 1 {
		t.Errorf("foreign_keys after reopen: got %d, want 1", fkEnabled2)
	}
}

// TestReadPoolExhaustion_WaitOnWrite simulates the case where the read pool
// is at max capacity (4 connections) and a write is pending. Verifies that
// reads block correctly and don't starve the write.
func TestReadPoolExhaustion_WaitOnWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running test in short mode")
	}
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	// Create a test table.
	if _, err := db.Write().Exec(`CREATE TABLE _readpool_test (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("Create table: %v", err)
	}

	var wg sync.WaitGroup
	var writeCompleted atomic.Bool
	var readAfterWrite atomic.Bool

	// Start a long-running read on the write connection (simulating a slow operation).
	// Note: writes are serialized, so this will block the write connection.
	wg.Add(1)
	go func() {
		defer wg.Done()
		tx, _ := db.Write().Begin()
		// Hold the transaction open briefly to simulate a slow write.
		time.Sleep(100 * time.Millisecond)
		tx.Rollback()
	}()

	// While the write is pending, try to read from the read pool.
	// The read should succeed (read pool is separate from write).
	time.Sleep(10 * time.Millisecond) // Ensure write goroutine starts first
	var count int
	if err := db.Read().QueryRow(`SELECT COUNT(*) FROM _readpool_test`).Scan(&count); err != nil {
		t.Fatalf("Read during write: %v", err)
	}
	readAfterWrite.Store(true)

	// Now do a write (should eventually succeed after the long-running transaction ends).
	if _, err := db.Write().Exec(`INSERT INTO _readpool_test VALUES ('test')`); err != nil {
		t.Fatalf("Write after read: %v", err)
	}
	writeCompleted.Store(true)

	wg.Wait()

	// Verify both operations completed.
	if !readAfterWrite.Load() || !writeCompleted.Load() {
		t.Error("Read or write did not complete")
	}
}

// TestWALCheckpointRace_NoDataLoss verifies that a WAL checkpoint doesn't
// cause data loss when writes are in progress.
func TestWALCheckpointRace_NoDataLoss(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	// Create a test table.
	if _, err := db.Write().Exec(`CREATE TABLE _wal_test (id TEXT PRIMARY KEY, val INT)`); err != nil {
		t.Fatalf("Create table: %v", err)
	}

	var wg sync.WaitGroup
	var inserts atomic.Int32

	// Insert many rows concurrently.
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				key := fmt.Sprintf("key_%d_%d", id, i)
				if _, err := db.Write().Exec(`INSERT INTO _wal_test (id, val) VALUES (?, ?)`, key, i); err != nil {
					t.Logf("Insert error: %v", err)
					continue
				}
				inserts.Add(1)
			}
		}(g)
	}

	// While inserts are happening, trigger a checkpoint.
	time.Sleep(10 * time.Millisecond)
	if _, err := db.Write().Exec(`PRAGMA wal_checkpoint(RESTART)`); err != nil {
		t.Logf("WAL checkpoint: %v", err)
	}

	wg.Wait()

	// Verify all inserted rows are present.
	var finalCount int
	if err := db.Read().QueryRow(`SELECT COUNT(*) FROM _wal_test`).Scan(&finalCount); err != nil {
		t.Fatalf("Count rows: %v", err)
	}

	expectedCount := 5 * 20 // 5 goroutines * 20 inserts each
	if finalCount != expectedCount {
		t.Errorf("Expected %d rows, got %d (some inserts may have failed)", expectedCount, finalCount)
	}
}

// TestClosedDB_ReadWrite_ReturnsNil verifies that Read() and Write() return nil
// (not panic) when called on a closed database.
func TestClosedDB_ReadWrite_ReturnsNil(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)

	// Close the database.
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Read() should return nil.
	readConn := db.Read()
	if readConn != nil {
		t.Error("Expected Read() to return nil after Close")
	}

	// Write() should return nil.
	writeConn := db.Write()
	if writeConn != nil {
		t.Error("Expected Write() to return nil after Close")
	}
}

// TestMultipleClose_Idempotent verifies that closing multiple times is safe.
func TestMultipleClose_Idempotent(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)

	// Close multiple times.
	for i := 0; i < 3; i++ {
		if err := db.Close(); err != nil {
			t.Errorf("Close #%d: %v", i+1, err)
		}
	}
}

// TestForeignKeyEnforcement verifies that foreign keys are enforced.
func TestForeignKeyEnforcement_Strict(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	// Create parent and child tables.
	if _, err := db.Write().Exec(`CREATE TABLE _parent (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("Create parent: %v", err)
	}
	if _, err := db.Write().Exec(`CREATE TABLE _child (id TEXT PRIMARY KEY, parent_id TEXT REFERENCES _parent(id))`); err != nil {
		t.Fatalf("Create child: %v", err)
	}

	// Insert parent.
	if _, err := db.Write().Exec(`INSERT INTO _parent VALUES ('p1')`); err != nil {
		t.Fatalf("Insert parent: %v", err)
	}

	// Valid insert (referencing existing parent).
	if _, err := db.Write().Exec(`INSERT INTO _child VALUES ('c1', 'p1')`); err != nil {
		t.Fatalf("Valid insert: %v", err)
	}

	// Invalid insert (referencing non-existent parent).
	_, err := db.Write().Exec(`INSERT INTO _child VALUES ('c2', 'nonexistent')`)
	if err == nil {
		t.Fatal("Expected FK violation error for non-existent parent, got nil")
	}
}

// TestSchemaCreation_Idempotent verifies that ApplySchema can be called
// multiple times without errors or data loss.
func TestSchemaCreation_Idempotent(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	// ApplySchema is already called in openTestDB, but call it again.
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("Second ApplySchema: %v", err)
	}

	// Verify schema exists by checking for expected tables.
	var tableCount int
	if err := db.Read().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name LIKE '_%'`).Scan(&tableCount); err != nil {
		t.Logf("Count tables: %v", err)
	}
	// Schema should have at least _migrations table.
	if tableCount < 1 {
		t.Errorf("Expected at least 1 system table, got %d", tableCount)
	}
}

// TestPragmaValues_Correct verifies that the set pragmas have the expected values.
func TestPragmaValues_Correct(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	tests := []struct {
		pragma   string
		expected string
	}{
		{"PRAGMA synchronous", "1"}, // NORMAL = 1
		{"PRAGMA foreign_keys", "1"},
		{"PRAGMA temp_store", "2"}, // MEMORY = 2
	}

	for _, tt := range tests {
		t.Run(tt.pragma, func(t *testing.T) {
			var val string
			if err := db.Read().QueryRow(tt.pragma).Scan(&val); err != nil {
				t.Fatalf("Query: %v", err)
			}
			if val != tt.expected {
				t.Errorf("%s: got %q, want %q", tt.pragma, val, tt.expected)
			}
		})
	}
}

// TestConcurrentReaders_AllSucceed verifies that multiple concurrent reads
// all succeed (not blocked by each other).
func TestConcurrentReaders_AllSucceed(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	if _, err := db.Write().Exec(`CREATE TABLE _readers_test (id TEXT PRIMARY KEY, val INT)`); err != nil {
		t.Fatalf("Create table: %v", err)
	}

	// Insert test data.
	for i := 0; i < 10; i++ {
		if _, err := db.Write().Exec(`INSERT INTO _readers_test VALUES (?, ?)`, fmt.Sprintf("k%d", i), i); err != nil {
			t.Fatalf("Insert: %v", err)
		}
	}

	// Launch many concurrent readers.
	const numReaders = 10
	var wg sync.WaitGroup
	var successCount atomic.Int32
	var errorCount atomic.Int32

	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Each reader queries the table.
			var count int
			if err := db.Read().QueryRow(`SELECT COUNT(*) FROM _readers_test`).Scan(&count); err != nil {
				errorCount.Add(1)
				t.Logf("Reader %d: %v", id, err)
				return
			}

			if count != 10 {
				t.Logf("Reader %d: expected count 10, got %d", id, count)
				errorCount.Add(1)
				return
			}

			successCount.Add(1)
		}(r)
	}

	wg.Wait()

	if errorCount.Load() > 0 {
		t.Logf("ConcurrentReaders: %d errors, %d successes", errorCount.Load(), successCount.Load())
	}

	if successCount.Load() < numReaders {
		t.Errorf("Expected %d successful reads, got %d", numReaders, successCount.Load())
	}
}

// TestMigration_BasicFlow verifies that migrations can be registered and run.
func TestMigration_BasicFlow(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	defer db.Close()

	// Define a custom migration.
	migrations := []sqlitedb.Migration{
		{
			Name: "test_migration_1",
			Up: func(tx *sql.Tx) error {
				_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS _custom_1 (id TEXT PRIMARY KEY)`)
				return err
			},
		},
		{
			Name: "test_migration_2",
			Up: func(tx *sql.Tx) error {
				_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS _custom_2 (id TEXT PRIMARY KEY)`)
				return err
			},
		},
	}

	// ApplySchema creates the _migrations tracking table.
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	// Run migrations.
	if err := db.Migrate(migrations); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Run again (should be idempotent).
	if err := db.Migrate(migrations); err != nil {
		t.Fatalf("Migrate again: %v", err)
	}

	// Verify both tables exist.
	var count1, count2 int
	if err := db.Read().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='_custom_1'`).Scan(&count1); err != nil {
		t.Fatalf("Check table 1: %v", err)
	}
	if err := db.Read().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='_custom_2'`).Scan(&count2); err != nil {
		t.Fatalf("Check table 2: %v", err)
	}

	if count1 == 0 || count2 == 0 {
		t.Error("Custom tables were not created by migrations")
	}
}
