package session_test

// hardening_iter1_test.go — tests added during Hardening Iteration 1.
// Covers the concurrent SaveManifest fix (session store-level mutex).

import (
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/session"
)

// TestSaveManifest_ConcurrentWrites verifies that two goroutines can call
// SaveManifest for the same session concurrently without a data race.
// Without the store-level mutex this would intermittently corrupt the manifest
// because both goroutines write to the same .tmp file and then rename it.
func TestSaveManifest_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(dir)

	sess := store.New("concurrent session", "/workspace", "claude-3")
	// Persist once so the directory exists.
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("initial SaveManifest: %v", err)
	}

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.SaveManifest(sess); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent SaveManifest error: %v", err)
	}

	// Verify the manifest is still readable after all concurrent writes.
	loaded, err := store.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load after concurrent SaveManifest: %v", err)
	}
	if loaded.ID != sess.ID {
		t.Errorf("manifest ID mismatch: %q vs %q", loaded.ID, sess.ID)
	}
}

// TestSaveManifest_UpdatedAtAdvances checks that UpdatedAt is set on each save.
func TestSaveManifest_UpdatedAtAdvances(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(dir)
	sess := store.New("title", "/workspace", "claude-3")

	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("first save: %v", err)
	}
	first, err := store.Load(sess.ID)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}

	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("second save: %v", err)
	}
	second, err := store.Load(sess.ID)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}

	if second.Manifest.UpdatedAt.Before(first.Manifest.UpdatedAt) {
		t.Errorf("UpdatedAt should not go backwards: first=%v second=%v",
			first.Manifest.UpdatedAt, second.Manifest.UpdatedAt)
	}
}
