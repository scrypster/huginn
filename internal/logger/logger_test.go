package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogger_WritesToFile(t *testing.T) {
	dir := t.TempDir()
	l, err := New(filepath.Join(dir, "test.log"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	l.Info("test message", "key", "value")

	data, err := os.ReadFile(filepath.Join(dir, "test.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "test message") {
		t.Errorf("expected log message in file, got: %s", data)
	}
}

func TestLogger_RotatesAtSizeLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	l, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	l.maxSizeBytes = 100 // tiny limit for test
	defer l.Close()

	for i := 0; i < 20; i++ {
		l.Info("a very long log message that will eventually exceed the rotation limit")
	}

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Error("expected rotated log file .1 to exist")
	}
}

func TestLogger_DiscardDoesNotPanic(t *testing.T) {
	l := Discard()
	l.Info("this should not panic", "key", "val")
}

func TestInit_CreatesLogDir(t *testing.T) {
	// Reset global state for this test.
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
		t.Fatalf("Init: %v", err)
	}
	logDir := filepath.Join(dir, "logs")
	if _, err := os.Stat(logDir); err != nil {
		t.Errorf("log dir not created: %v", err)
	}
}

func TestInit_Idempotent(t *testing.T) {
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
	if err := Init(dir); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	// Second call must not fail or change the logger.
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
		t.Error("Init changed globalLogger on second call (not idempotent)")
	}
}

func TestL_BeforeInit_NoopNotNil(t *testing.T) {
	// Reset global state.
	globalMu.Lock()
	prev := globalLogger
	globalLogger = nil
	globalMu.Unlock()
	defer func() {
		globalMu.Lock()
		globalLogger = prev
		globalMu.Unlock()
	}()

	l := L()
	if l == nil {
		t.Error("L() returned nil before Init")
	}
	// Should not panic.
	l.Info("test before init")
}

func TestTailLog_Empty(t *testing.T) {
	dir := t.TempDir()
	lines, err := TailLog(dir, 10)
	if err != nil {
		t.Errorf("TailLog on empty dir: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("expected 0 lines, got %d", len(lines))
	}
}

func TestTailLog_ReturnsLastN(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(logDir, "huginn.log")

	var sb strings.Builder
	for i := 1; i <= 100; i++ {
		sb.WriteString(fmt.Sprintf("line %d\n", i))
	}
	if err := os.WriteFile(logPath, []byte(sb.String()), 0600); err != nil {
		t.Fatal(err)
	}

	lines, err := TailLog(dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 10 {
		t.Errorf("expected 10 lines, got %d", len(lines))
	}
	if !strings.Contains(lines[9], "line 100") {
		t.Errorf("last line should be 'line 100', got %q", lines[9])
	}
}

func TestTailLog_FewerLinesThanN(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(logDir, "huginn.log")

	content := "line1\nline2\nline3\n"
	if err := os.WriteFile(logPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	lines, err := TailLog(dir, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
}

func TestLogPath(t *testing.T) {
	baseDir := "/tmp/huginn-test"
	got := LogPath(baseDir)
	want := "/tmp/huginn-test/logs/huginn.log"
	if got != want {
		t.Errorf("LogPath(%q) = %q, want %q", baseDir, got, want)
	}
}

func TestGlobalHelpers_DoNotPanic(t *testing.T) {
	// Reset global state so we go through the Discard path.
	globalMu.Lock()
	prev := globalLogger
	globalLogger = nil
	globalMu.Unlock()
	defer func() {
		globalMu.Lock()
		globalLogger = prev
		globalMu.Unlock()
	}()

	// None of these should panic.
	Info("info test")
	Warn("warn test")
	Error("error test")
	Debug("debug test")
}
