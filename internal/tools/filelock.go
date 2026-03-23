package tools

import "sync"

// FileLockManager provides per-path mutual exclusion for concurrent file access.
type FileLockManager struct {
	mu    sync.Mutex
	locks map[string]*pathLock
}

// pathLock wraps a mutex with a reference count to clean up empty entries.
type pathLock struct {
	mu       sync.Mutex
	refcount int
}

// NewFileLockManager creates a new FileLockManager.
func NewFileLockManager() *FileLockManager {
	return &FileLockManager{locks: make(map[string]*pathLock)}
}

// Lock acquires the lock for the given path.
func (f *FileLockManager) Lock(path string) {
	f.mu.Lock()
	pl, ok := f.locks[path]
	if !ok {
		pl = &pathLock{}
		f.locks[path] = pl
	}
	pl.refcount++
	f.mu.Unlock()
	pl.mu.Lock()
}

// Unlock releases the lock for the given path.
//
// Design note: pl.mu.Unlock() is called inside the f.mu critical section so
// that the map state (refcount, presence in map) and the mutex state are always
// updated atomically. This eliminates the window that existed between the old
// f.mu.Unlock() and pl.mu.Unlock() calls where a concurrent Unlock() could
// decrement refcount a second time (potentially to 0 or below) and call
// pl.mu.Unlock() again — a double-unlock that panics at runtime.
//
// The underflow guard (refcount <= 0) prevents refcount from going negative if
// Unlock is called more times than Lock, which would cause the cleanup condition
// (refcount == 0) to never fire and leak the map entry indefinitely.
func (f *FileLockManager) Unlock(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	pl, ok := f.locks[path]
	if !ok {
		return // never locked or already cleaned up
	}
	if pl.refcount <= 0 {
		// Underflow guard: defensive against double-unlock bugs.
		// Do NOT call pl.mu.Unlock() here — we don't own the lock.
		return
	}
	pl.refcount--
	if pl.refcount == 0 {
		delete(f.locks, path)
	}
	pl.mu.Unlock()
}
