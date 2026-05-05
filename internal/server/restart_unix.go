//go:build !windows

package server

import (
	"os"
	"path/filepath"
	"syscall"
)

// platformExec replaces the current process image with the binary at exePath,
// passing the original args and environment. On success it never returns.
func platformExec(exePath string, args []string, env []string) error {
	return syscall.Exec(exePath, args, env)
}

// execSupported reports whether in-place restart is available on this platform.
const execSupported = true

// currentExePath returns the resolved path of the running binary,
// following symlinks (important for Homebrew installs).
func currentExePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}
