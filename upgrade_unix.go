//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// platformStopProcess sends SIGTERM to the process, waits up to 15 seconds for a
// graceful exit, then force-kills it. The PID file is removed afterward.
func platformStopProcess(pid int, pidPath string) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil // already gone
	}
	_ = proc.Signal(syscall.SIGTERM)

	// Poll with a bounded iteration count (150 × 100 ms = 15 s).
	// Using a counter instead of time.Now() avoids sensitivity to NTP/VM clock jumps.
	for i := 0; i < 150; i++ {
		if proc.Signal(syscall.Signal(0)) != nil {
			break // process has exited
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Force-kill if still alive after the grace period.
	if proc.Signal(syscall.Signal(0)) == nil {
		_ = proc.Kill()
		time.Sleep(500 * time.Millisecond)
	}

	// Brief pause to let the OS release file locks (e.g. SQLite WAL) before
	// the new binary attempts to open the same data directory.
	time.Sleep(200 * time.Millisecond)
	os.Remove(pidPath)
	return nil
}

// platformDetachStart starts cmd in a new session so it survives after the
// upgrade command exits.
func platformDetachStart(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
