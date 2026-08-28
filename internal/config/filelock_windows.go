//go:build windows

package config

// acquireFileLock is a no-op on windows: flock(2) has no direct Windows
// equivalent reachable from golang.org/x/sys/unix, and huginn's cross-process
// story (TUI + `serve` as separate OS processes) is a Unix (macOS/Linux)
// deployment today. UpdateAt still serializes goroutines within a single
// Windows process via updateMu; it just doesn't gain the cross-process
// guarantee that flock gives on Unix.
func acquireFileLock(path string) (*fileLock, error) {
	return &fileLock{}, nil
}
