package tools

import (
	"sync"
	"testing"
)

// TestFileLockManager_RefcountUnderflow_NoNegative verifies that calling Unlock
// more times than Lock does not drive refcount negative and does not panic.
//
// Bug: Unlock() decrements pl.refcount and then calls pl.mu.Unlock() AFTER
// releasing f.mu. Between those two operations a concurrent Unlock() call can
// see the same pl (still in the map if refcount > 0 after the first decrement),
// decrement refcount again (potentially to 0 or negative), delete the map entry,
// and call pl.mu.Unlock() — resulting in a double-unlock panic (sync: unlock of
// unlocked mutex). Additionally, if refcount goes negative the cleanup condition
// (refcount == 0) never fires, leaking the pathLock entry in the map forever.
//
// Fix: move pl.mu.Unlock() inside the f.mu critical section so the map state
// and mutex state are always updated atomically; add a refcount underflow guard
// (refcount <= 0 before decrement → return without unlocking).
func TestFileLockManager_RefcountUnderflow_NoNegative(t *testing.T) {
	t.Parallel()

	flm := NewFileLockManager()

	// Normal balanced Lock/Unlock — must not panic.
	flm.Lock("foo")
	flm.Unlock("foo")

	// Spurious Unlock (no prior Lock) — must not panic or corrupt state.
	flm.Unlock("foo")
	flm.Unlock("foo")

	// After spurious unlocks, normal Lock/Unlock must still work.
	flm.Lock("bar")
	flm.Unlock("bar")

	// Map must be empty (no leaked entries from spurious unlocks).
	flm.mu.Lock()
	defer flm.mu.Unlock()
	if len(flm.locks) != 0 {
		t.Errorf("expected empty locks map after all unlocks, got %d entries: %v", len(flm.locks), flm.locks)
	}
}

// TestFileLockManager_ConcurrentDoubleUnlock_NoPanic verifies that concurrent
// Lock/Unlock pairs on the same key do not panic or race under the race detector.
func TestFileLockManager_ConcurrentDoubleUnlock_NoPanic(t *testing.T) {
	t.Parallel()

	flm := NewFileLockManager()
	const goroutines = 50
	const iterations = 100

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				flm.Lock("shared")
				flm.Unlock("shared")
			}
		}()
	}
	wg.Wait()

	// Map must be fully cleaned up.
	flm.mu.Lock()
	defer flm.mu.Unlock()
	if len(flm.locks) != 0 {
		t.Errorf("expected empty locks map after all goroutines done, got %d entries", len(flm.locks))
	}
}

// TestFileLockManager_UnlockWithoutLock_NoLeak verifies that calling Unlock
// for a path that was never locked neither panics nor leaks a map entry.
func TestFileLockManager_UnlockWithoutLock_NoLeak(t *testing.T) {
	flm := NewFileLockManager()
	// Should not panic.
	flm.Unlock("never-locked")
	flm.Unlock("never-locked")

	flm.mu.Lock()
	defer flm.mu.Unlock()
	if len(flm.locks) != 0 {
		t.Errorf("expected empty map after unlock-without-lock, got %d entries", len(flm.locks))
	}
}
