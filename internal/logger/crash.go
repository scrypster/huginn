package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

// WriteCrashFile writes a crash report to dir/<timestamp>.txt.
func WriteCrashFile(dir, panicMsg, stack string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	ts := time.Now().Format("2006-01-02T15-04-05")
	path := filepath.Join(dir, ts+".txt")
	content := fmt.Sprintf("Huginn crash report\nTime: %s\n\nPanic: %s\n\nStack:\n%s\n",
		time.Now().Format(time.RFC3339), panicMsg, stack)
	return os.WriteFile(path, []byte(content), 0600)
}

// InstallPanicHandler installs a deferred panic handler that writes crash files to crashDir.
// Usage: defer InstallPanicHandler(crashDir)()
func InstallPanicHandler(crashDir string) func() {
	return func() {
		r := recover()
		if r == nil {
			return
		}
		stack := string(debug.Stack())
		_ = WriteCrashFile(crashDir, fmt.Sprintf("%v", r), stack)
		// Re-panic so the process exits with a non-zero status
		panic(r)
	}
}
