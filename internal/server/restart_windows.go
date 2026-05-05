//go:build windows

package server

import (
	"errors"
	"os"
	"path/filepath"
)

// platformExec is not supported on Windows.
func platformExec(_ string, _ []string, _ []string) error {
	return errors.New("in-place restart is not supported on Windows")
}

const execSupported = false

func currentExePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}
