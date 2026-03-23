package threadmgr

import (
	"errors"
	"fmt"
)

// ErrEmptyThreadID is returned when AcquireLeases is called with an empty threadID.
var ErrEmptyThreadID = errors.New("threadID must not be empty")

// AcquireLeases attempts to lock all paths for threadID.
// If any path is already held by a different thread, no paths are acquired
// (atomic all-or-nothing) and the conflicting paths are returned.
// Returns (nil, nil) on success.
func (tm *ThreadManager) AcquireLeases(threadID string, paths []string) (conflicts []string, err error) {
	if threadID == "" {
		return nil, fmt.Errorf("AcquireLeases: %w", ErrEmptyThreadID)
	}
	if len(paths) == 0 {
		return nil, nil
	}

	tm.leaseMu.Lock()
	defer tm.leaseMu.Unlock()

	// Check for conflicts first — do not acquire any if there are conflicts.
	for _, p := range paths {
		if owner, held := tm.fileLocks[p]; held && owner != threadID {
			conflicts = append(conflicts, p)
		}
	}
	if len(conflicts) > 0 {
		return conflicts, nil
	}

	// No conflicts — acquire all paths.
	for _, p := range paths {
		tm.fileLocks[p] = threadID
	}
	return nil, nil
}

// ReleaseLeases removes all file leases held by threadID.
func (tm *ThreadManager) ReleaseLeases(threadID string) {
	tm.leaseMu.Lock()
	defer tm.leaseMu.Unlock()
	for path, owner := range tm.fileLocks {
		if owner == threadID {
			delete(tm.fileLocks, path)
		}
	}
}
