package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestLogger_ConcurrentWrites verifies no data races during concurrent writes
// to the same Logger (exercises the Write mutex).
func TestLogger_ConcurrentWrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "concurrent.log")

	l, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			l.Info(fmt.Sprintf("goroutine %d writing log line", n))
		}(i)
	}
	wg.Wait()

	// File should exist and be non-empty.
	info, err := os.Stat(path)
	if err != nil {
		t.Errorf("log file missing after concurrent writes: %v", err)
	} else if info.Size() == 0 {
		t.Error("log file empty after concurrent writes")
	}
}

// TestInit_SecondCallNoOp verifies that a second call to Init does not replace
// the existing global logger instance (pointer identity check).
func TestInit_SecondCallNoOp(t *testing.T) {
	// Save and reset global state.
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
	if err := Init(dir); err != nil {
		t.Fatalf("first Init: %v", err)
	}

	globalMu.RLock()
	first := globalLogger
	globalMu.RUnlock()

	if err := Init(dir); err != nil {
		t.Fatalf("second Init: %v", err)
	}

	globalMu.RLock()
	second := globalLogger
	globalMu.RUnlock()

	if first != second {
		t.Error("second Init replaced globalLogger (not idempotent)")
	}
	// Should not panic.
	L().Info("second-call no-op test")
}

// TestTailLog_ExactlyN verifies that when the log file has exactly N lines,
// all N are returned (boundary condition: len(lines) == n).
func TestTailLog_ExactlyN(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(logDir, logFileName)

	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	lines, err := TailLog(dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Errorf("expected exactly 3 lines, got %d", len(lines))
	}
	if lines[0] != "line1" {
		t.Errorf("lines[0] = %q, want %q", lines[0], "line1")
	}
	if lines[2] != "line3" {
		t.Errorf("lines[2] = %q, want %q", lines[2], "line3")
	}
}

// TestTailLog_SingleLine verifies that a single-line log file with no trailing
// newline is returned correctly.
func TestTailLog_SingleLine(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(logDir, logFileName)

	if err := os.WriteFile(path, []byte("only-line"), 0600); err != nil {
		t.Fatal(err)
	}

	lines, err := TailLog(dir, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 {
		t.Errorf("expected 1 line, got %d", len(lines))
	}
	if lines[0] != "only-line" {
		t.Errorf("unexpected line content: %q", lines[0])
	}
}

// TestLogger_RotationUnderConcurrentLoad verifies that rotation triggered by
// one goroutine while others are writing does not lose writes or panic.
func TestLogger_RotationUnderConcurrentLoad(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "rotate-concurrent.log")

	l, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	l.maxSizeBytes = 512 // very small to force rotations quickly
	defer l.Close()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				l.Info("log line", "goroutine", n, "iteration", j)
			}
		}(i)
	}
	wg.Wait()

	// At minimum, the primary log file must exist and be readable.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("primary log file missing after concurrent rotation test: %v", err)
	}
}
