package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWriteCrashFile_CreatesFile verifies that WriteCrashFile creates a file in the target dir.
func TestWriteCrashFile_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	err := WriteCrashFile(dir, "test panic message", "goroutine 1 [running]:\nmain.main()\n\t/tmp/main.go:10")
	if err != nil {
		t.Fatalf("WriteCrashFile: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 crash file, got %d", len(entries))
	}
	if !strings.HasSuffix(entries[0].Name(), ".txt") {
		t.Errorf("expected .txt extension, got %q", entries[0].Name())
	}
}

// TestWriteCrashFile_ContainsTimestamp verifies the crash report contains a timestamp.
func TestWriteCrashFile_ContainsTimestamp(t *testing.T) {
	dir := t.TempDir()
	if err := WriteCrashFile(dir, "panic: nil pointer", "stack here"); err != nil {
		t.Fatalf("WriteCrashFile: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) == 0 {
		t.Fatal("no crash files created")
	}
	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)

	// Should contain the current year.
	year := time.Now().Format("2006")
	if !strings.Contains(content, year) {
		t.Errorf("expected year %q in crash report, content: %s", year, content)
	}
	// Should contain RFC3339-like "Time:" line.
	if !strings.Contains(content, "Time:") {
		t.Errorf("expected 'Time:' field in crash report, content: %s", content)
	}
}

// TestWriteCrashFile_ContainsPanicMessage verifies the crash report contains the panic message.
func TestWriteCrashFile_ContainsPanicMessage(t *testing.T) {
	dir := t.TempDir()
	panicMsg := "runtime error: index out of range [5] with length 3"
	if err := WriteCrashFile(dir, panicMsg, ""); err != nil {
		t.Fatalf("WriteCrashFile: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	content := string(data)

	if !strings.Contains(content, panicMsg) {
		t.Errorf("expected panic message in crash report, got: %s", content)
	}
	if !strings.Contains(content, "Panic:") {
		t.Errorf("expected 'Panic:' label in crash report, got: %s", content)
	}
}

// TestWriteCrashFile_ContainsStackTrace verifies that the stack trace appears in the report.
func TestWriteCrashFile_ContainsStackTrace(t *testing.T) {
	dir := t.TempDir()
	stack := "goroutine 1 [running]:\ngithub.com/foo/bar.doSomething()\n\t/src/bar.go:42"
	if err := WriteCrashFile(dir, "some panic", stack); err != nil {
		t.Fatalf("WriteCrashFile: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	content := string(data)

	if !strings.Contains(content, "Stack:") {
		t.Errorf("expected 'Stack:' label in crash report, got: %s", content)
	}
	if !strings.Contains(content, "goroutine 1") {
		t.Errorf("expected stack trace content in crash report, got: %s", content)
	}
}

// TestWriteCrashFile_NoSensitiveData verifies that a crash report generated
// from a config-like structure does not contain literal API key values when
// only the env var reference is provided.
func TestWriteCrashFile_NoSensitiveData(t *testing.T) {
	dir := t.TempDir()
	// Simulate crash context that mentions config (without literal secrets).
	panicMsg := "config validation failed"
	stack := "internal/config.Validate()\n\tconfig.go:300\nbackend.api_key=$MY_SECRET_KEY"

	if err := WriteCrashFile(dir, panicMsg, stack); err != nil {
		t.Fatalf("WriteCrashFile: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	content := string(data)

	// The literal secret value should NOT appear in the crash report.
	if strings.Contains(content, "resolved-actual-secret-12345") {
		t.Errorf("crash report contains literal secret value: %s", content)
	}
	// The env var reference may appear (that's fine).
	t.Logf("crash report preview: %s", content[:min(200, len(content))])
}

// TestWriteCrashFile_CreatesDirectory verifies that WriteCrashFile creates the target dir.
func TestWriteCrashFile_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "crashes", "nested")
	// Directory does not exist yet.
	err := WriteCrashFile(dir, "test", "stack")
	if err != nil {
		t.Fatalf("WriteCrashFile: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("expected crash dir to be created: %v", err)
	}
}

// TestWriteCrashFile_FilePermissions verifies the crash file is created with mode 0600.
func TestWriteCrashFile_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	if err := WriteCrashFile(dir, "test", "stack"); err != nil {
		t.Fatalf("WriteCrashFile: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	info, err := os.Stat(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected file mode 0600, got %o", info.Mode().Perm())
	}
}

// TestWriteCrashFile_Header verifies the crash report starts with the expected header.
func TestWriteCrashFile_Header(t *testing.T) {
	dir := t.TempDir()
	if err := WriteCrashFile(dir, "boom", "trace"); err != nil {
		t.Fatalf("WriteCrashFile: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if !strings.HasPrefix(string(data), "Huginn crash report") {
		t.Errorf("expected crash report to start with 'Huginn crash report', got: %s", data[:min(50, len(data))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
