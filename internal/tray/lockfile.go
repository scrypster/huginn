package tray

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// AcquireLock attempts to claim the lockfile at path by writing the current
// process PID. Returns owned=true if this process now owns the lock.
// Returns owned=false (no error) if a live process already holds the lock.
// Returns an error only on unexpected I/O failure.
func AcquireLock(path string) (owned bool, err error) {
	if data, readErr := os.ReadFile(path); readErr == nil {
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if parseErr == nil && pid > 0 && processIsLive(pid) {
			return false, nil
		}
		_ = os.Remove(path)
	}

	content := fmt.Sprintf("%d\n", os.Getpid())
	if writeErr := os.WriteFile(path, []byte(content), 0644); writeErr != nil {
		return false, fmt.Errorf("tray: acquire lock %s: %w", path, writeErr)
	}
	return true, nil
}

// ReleaseLock removes the lockfile. Safe to call even if the file does not exist.
func ReleaseLock(path string) {
	_ = os.Remove(path)
}

// processIsLive returns true if the process with the given PID is running.
// Uses signal 0: checks existence without sending a signal.
func processIsLive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
