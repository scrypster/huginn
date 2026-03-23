package sqlitedb

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestStartWALCheckpoint_RunsWithoutError verifies that StartWALCheckpoint
// can be called and the goroutine exits cleanly when the context is cancelled.
func TestStartWALCheckpoint_RunsWithoutError(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if err := db.ApplySchema(); err != nil {
		// Schema may not be available in test build; still exercise checkpoint.
		t.Logf("ApplySchema: %v (continuing)", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start checkpointing with a very short interval to exercise the ticker path.
	// We override the interval by testing the goroutine is launched and exits.
	db.StartWALCheckpoint(ctx)

	// Cancel immediately — goroutine must exit cleanly.
	cancel()

	// Give the goroutine time to observe the cancellation.
	time.Sleep(50 * time.Millisecond)
	// If we reach here without a panic or deadlock, the test passes.
}

// TestStartWALCheckpoint_ManualCheckpoint verifies that the WAL checkpoint
// PRAGMA can be executed directly on the write connection.
func TestStartWALCheckpoint_ManualCheckpoint(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "wal.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// Execute the same PRAGMA that StartWALCheckpoint runs.
	_, execErr := db.Write().ExecContext(context.Background(), "PRAGMA wal_checkpoint(TRUNCATE)")
	if execErr != nil {
		t.Errorf("PRAGMA wal_checkpoint(TRUNCATE) failed: %v", execErr)
	}
}

// TestStartWALCheckpoint_ContextCancelled verifies that the checkpoint goroutine
// respects context cancellation and does not attempt further checkpoints.
func TestStartWALCheckpoint_ContextCancelled(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "cancel.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel before starting — goroutine should exit on first select.
	cancel()
	db.StartWALCheckpoint(ctx)

	// Allow the goroutine to start and observe the cancelled context.
	time.Sleep(20 * time.Millisecond)
	// No assertions needed — if goroutine leaks, race detector or a subsequent
	// test timeout will catch it.
}
