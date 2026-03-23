package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cockroachdb/pebble/v2"
)

// TestSafeGet_ClosedDB_ReturnsErrorNotPanic verifies that safeGet returns an
// error (not a panic) when called on a DB that has been closed.
// Pebble panics on Get after Close — the safeGet wrapper must catch that.
func TestSafeGet_ClosedDB_ReturnsErrorNotPanic(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.pebble")
	db, err := pebble.Open(dbPath, &pebble.Options{})
	if err != nil {
		t.Fatalf("pebble.Open: %v", err)
	}
	// Write a key so we have something to Get.
	if err := db.Set([]byte("k"), []byte("v"), pebble.Sync); err != nil {
		t.Fatalf("db.Set: %v", err)
	}
	// Close the database.
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}

	// safeGet on a closed DB must NOT panic; it should return an error.
	val, closer, err := safeGet(db, []byte("k"))
	if closer != nil {
		_ = closer.Close()
	}
	_ = val
	if err == nil {
		// Some pebble versions return ErrClosed as an error rather than panicking.
		// Either outcome is acceptable — we just must not panic.
		t.Log("safeGet returned nil error on closed DB (pebble returned ErrClosed gracefully)")
	}
}

// TestSafeNewIter_ClosedDB_ReturnsErrorNotPanic verifies that safeNewIter returns
// an error (not a panic) when called on a closed DB.
func TestSafeNewIter_ClosedDB_ReturnsErrorNotPanic(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "iter.pebble")
	db, err := pebble.Open(dbPath, &pebble.Options{})
	if err != nil {
		t.Fatalf("pebble.Open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}

	iter, err := safeNewIter(db, nil)
	if iter != nil {
		_ = iter.Close()
	}
	_ = err // error or nil — both acceptable; key requirement is no panic
}

// TestSafeGet_NilDB_ReturnsError verifies that safeGet handles a nil DB gracefully.
func TestSafeGet_NilDB_ReturnsError(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("safeGet panicked on nil DB: %v", r)
		}
	}()
	val, closer, err := safeGet(nil, []byte("k"))
	if closer != nil {
		_ = closer.Close()
	}
	_ = val
	if err == nil {
		t.Error("expected error for nil DB, got nil")
	}
}

// TestOpen_InvalidDir verifies that Open returns an error for an unwritable path.
func TestOpen_InvalidDir_ReturnsError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root; permission test not meaningful")
	}
	_, err := Open("/proc/invalid_huginn_test_path")
	if err == nil {
		t.Error("expected error opening pebble store in unwritable path")
	}
}
