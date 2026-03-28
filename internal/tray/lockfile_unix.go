//go:build !windows

package tray

import (
	"os"
	"syscall"
)

// processIsLive returns true if the process with the given PID is running.
// Uses POSIX signal 0, which checks process existence without sending a signal.
func processIsLive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
