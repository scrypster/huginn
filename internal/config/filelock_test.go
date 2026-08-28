package config

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestAcquireFileLock_CreatesLockFile verifies acquireFileLock actually
// executes the flock path (item 5, Opus vet 2026-08-28): config.UpdateAt's
// mutex is process-local, so the TUI and `serve` running as separate OS
// processes could still interleave a read from one with a write from the
// other. The sidecar lock file must be created and release() must succeed
// cleanly.
func TestAcquireFileLock_CreatesLockFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	lock, err := acquireFileLock(path)
	if err != nil {
		t.Fatalf("acquireFileLock: %v", err)
	}
	if _, statErr := os.Stat(lockPathFor(path)); statErr != nil {
		t.Fatalf("expected lock file to exist: %v", statErr)
	}
	if err := lock.release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

// TestAcquireFileLock_SecondAcquireBlocksUntilReleased proves the lock is
// actually exclusive: a second acquireFileLock call on the same path must
// not succeed until the first is released. This is the mechanism, not just
// the file's existence — a lock file that isn't actually flocked would let
// both acquires return immediately.
func TestAcquireFileLock_SecondAcquireBlocksUntilReleased(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	first, err := acquireFileLock(path)
	if err != nil {
		t.Fatalf("first acquireFileLock: %v", err)
	}

	var secondAcquired atomic.Bool
	done := make(chan struct{})
	go func() {
		second, err := acquireFileLock(path)
		if err != nil {
			t.Errorf("second acquireFileLock: %v", err)
			close(done)
			return
		}
		secondAcquired.Store(true)
		_ = second.release()
		close(done)
	}()

	// The second acquirer must still be blocked shortly after starting —
	// give it a generous window to (wrongly) succeed if the lock isn't
	// exclusive.
	time.Sleep(150 * time.Millisecond)
	if secondAcquired.Load() {
		t.Fatal("second acquireFileLock succeeded while the first lock was still held — flock is not exclusive")
	}

	if err := first.release(); err != nil {
		t.Fatalf("first release: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("second acquireFileLock never returned after the first lock was released")
	}
	if !secondAcquired.Load() {
		t.Fatal("second acquireFileLock did not succeed after the first lock was released")
	}
}

// TestUpdateAt_FileLock_SerializesConcurrentGoroutines documents that
// UpdateAt's file-lock path executes without error under concurrent callers
// (in-process concurrency is already covered by updateMu; this additionally
// exercises the flock acquire/release around every UpdateAt call). The
// cross-process guarantee itself — two separate `go test` binaries, or the
// TUI and `serve` as separate OS processes — is not exercisable from a single
// Go test binary, since acquireFileLock is only ever called with updateMu
// already held in this process; it is documented on UpdateAt and
// acquireFileLock instead.
func TestUpdateAt_FileLock_SerializesConcurrentGoroutines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := Default().SaveTo(path); err != nil {
		t.Fatalf("seed SaveTo: %v", err)
	}

	const n = 20
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errCh <- UpdateAt(path, func(cfg *Config) {
				cfg.WebUI.Port = 9000 + i
			})
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("UpdateAt: %v", err)
		}
	}

	if _, err := os.Stat(lockPathFor(path)); err != nil {
		t.Fatalf("expected UpdateAt to have created the lock file: %v", err)
	}
}
