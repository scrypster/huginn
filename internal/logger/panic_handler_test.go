package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstallPanicHandler_NoPanic verifies that InstallPanicHandler returns a
// no-op when no panic occurred (the r == nil branch).
func TestInstallPanicHandler_NoPanic(t *testing.T) {
	dir := t.TempDir()
	fn := InstallPanicHandler(dir)
	// Call the returned func without a panic — should do nothing.
	fn()
	// No crash file should have been written.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no crash file, found %d entries", len(entries))
	}
}

// TestInstallPanicHandler_WritesCrashFileAndRepanics verifies the full panic
// handler path: crash file written then re-panic occurs.
func TestInstallPanicHandler_WritesCrashFileAndRepanics(t *testing.T) {
	dir := t.TempDir()

	// We need to catch the re-panic from InstallPanicHandler.
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Error("expected re-panic from InstallPanicHandler but got none")
			}
		}()
		// This goroutine will panic, then InstallPanicHandler will write a crash
		// file and re-panic.
		func() {
			defer InstallPanicHandler(dir)()
			panic("boom from test")
		}()
	}()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected crash file written by InstallPanicHandler")
	}
	data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if !strings.Contains(string(data), "boom from test") {
		t.Errorf("expected panic message in crash file, got: %s", data)
	}
}

// TestNew_InvalidPath verifies that New returns an error when the path cannot
// be created (e.g. parent is a file, not a directory).
func TestNew_InvalidPath(t *testing.T) {
	dir := t.TempDir()
	// Create a regular file where New would try to create a directory.
	blocker := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// Attempt to create a logger whose parent directory is the file above.
	_, err := New(filepath.Join(blocker, "subdir", "test.log"))
	if err == nil {
		t.Error("expected error when parent path is a file, got nil")
	}
}

// TestClose_NilFile verifies that calling Close on a Logger with a nil file
// (i.e. Discard logger) does not panic and returns nil.
func TestClose_NilFile(t *testing.T) {
	l := Discard()
	if err := l.Close(); err != nil {
		t.Errorf("Close on Discard logger: %v", err)
	}
}

// TestTailLog_ReadError verifies that TailLog returns an error when the log
// file exists but cannot be read (permissions set to 000).
func TestTailLog_ReadError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permission checks")
	}
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(logDir, "huginn.log")
	if err := os.WriteFile(logPath, []byte("hello\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Remove read permission so ReadFile fails.
	if err := os.Chmod(logPath, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(logPath, 0600) //nolint:errcheck

	_, err := TailLog(dir, 10)
	if err == nil {
		t.Error("expected error from TailLog when file is unreadable, got nil")
	}
}

// TestInit_InvalidDir verifies that Init returns an error when baseDir is
// completely invalid (e.g. a path under a file rather than a directory).
func TestInit_InvalidDir(t *testing.T) {
	// Reset global state.
	globalMu.Lock()
	prev := globalLogger
	globalLogger = nil
	globalMu.Unlock()
	defer func() {
		globalMu.Lock()
		if globalLogger != nil {
			_ = globalLogger.Close()
		}
		globalLogger = prev
		globalMu.Unlock()
	}()

	dir := t.TempDir()
	// Put a regular file where Init would try to create the logs directory.
	logsFile := filepath.Join(dir, "logs")
	if err := os.WriteFile(logsFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	err := Init(dir)
	if err == nil {
		t.Error("expected error from Init when logs path is a file, got nil")
	}
}
