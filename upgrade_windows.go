//go:build windows

package main

import "os/exec"

// platformStopProcess is a no-op on Windows. The upgrade command opens the
// browser and exits early — self-update is not attempted on Windows.
func platformStopProcess(pid int, pidPath string) error {
	return nil
}

// platformDetachStart starts cmd without inheriting the parent's console.
func platformDetachStart(cmd *exec.Cmd) error {
	return cmd.Start()
}
