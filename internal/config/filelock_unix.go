//go:build !windows

package config

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// acquireFileLock takes a blocking, exclusive flock(2) on the sidecar lock
// file beside path, creating the file (and its directory) if needed. The
// lock is held by the kernel against the open file descriptor, so it is
// automatically released if the holding process dies or crashes — no
// separate stale-lock detection is needed the way it would be for a
// PID-file-style lock.
//
// This is what makes UpdateAt's read-modify-write safe across OS processes:
// the in-process updateMu mutex only serializes goroutines within one
// process, so the TUI and `serve` running as separate processes could still
// interleave a read from one with a write from the other. Both processes
// acquiring this same flock before touching the config file closes that
// window.
func acquireFileLock(path string) (*fileLock, error) {
	lockPath := lockPathFor(path)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o750); err != nil {
		return nil, fmt.Errorf("config: mkdir for lock %s: %w", lockPath, err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("config: open lock file %s: %w", lockPath, err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("config: flock %s: %w", lockPath, err)
	}
	return &fileLock{closer: func() error {
		unlockErr := unix.Flock(int(f.Fd()), unix.LOCK_UN)
		closeErr := f.Close()
		if unlockErr != nil {
			return unlockErr
		}
		return closeErr
	}}, nil
}
