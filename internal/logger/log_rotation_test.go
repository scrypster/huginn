package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRotation_ThreeFiles verifies that rotation creates .1, .2, .3 files.
func TestRotation_ThreeFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huginn.log")

	l, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	l.maxSizeBytes = 100 // tiny threshold to trigger rotation quickly

	// Write enough data to trigger 3+ rotations.
	// Each write of ~120 bytes exceeds the 100-byte threshold.
	payload := strings.Repeat("X", 120) + "\n"
	for i := 0; i < 10; i++ {
		if _, err := l.Write([]byte(payload)); err != nil {
			t.Fatalf("Write: %v", err)
		}
		// Give async rotation goroutine time to run.
		time.Sleep(20 * time.Millisecond)
	}

	l.Close()

	// Verify rotated files exist.
	for _, suffix := range []string{".1", ".2", ".3"} {
		if _, err := os.Stat(path + suffix); os.IsNotExist(err) {
			t.Errorf("expected rotated file %s to exist", path+suffix)
		}
	}
}

// TestTailLog_ReadsFromRotatedFiles verifies TailLog reads across .1/.2/.3.
func TestTailLog_ReadsFromRotatedFiles(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(logDir, "huginn.log")

	// Write distinct content to each file.
	os.WriteFile(logPath+".3", []byte("line-from-3\n"), 0o600)
	os.WriteFile(logPath+".2", []byte("line-from-2\n"), 0o600)
	os.WriteFile(logPath+".1", []byte("line-from-1\n"), 0o600)
	os.WriteFile(logPath, []byte("line-from-current\n"), 0o600)

	lines, err := TailLog(dir, 10)
	if err != nil {
		t.Fatalf("TailLog: %v", err)
	}

	// Should include lines from all files.
	if len(lines) < 4 {
		t.Fatalf("expected >= 4 lines, got %d: %v", len(lines), lines)
	}

	// Verify all sources are present.
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"line-from-3", "line-from-2", "line-from-1", "line-from-current"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected TailLog to contain %q, got %q", want, joined)
		}
	}
}

// TestWrite_ReturnsFastDuringRotation checks that Write returns quickly even
// when rotation is triggered.
func TestWrite_ReturnsFastDuringRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huginn.log")

	l, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	l.maxSizeBytes = 50 // trigger rotation easily

	// Pre-fill the file to exceed threshold.
	os.WriteFile(path, []byte(strings.Repeat("X", 100)), 0o600)

	start := time.Now()
	for i := 0; i < 5; i++ {
		l.Write([]byte("small\n"))
	}
	elapsed := time.Since(start)

	// Give rotation goroutine time to finish before closing.
	time.Sleep(30 * time.Millisecond)
	l.Close()

	// Writes should complete in well under 100ms total even with rotation.
	if elapsed > 100*time.Millisecond {
		t.Errorf("expected Write to return fast, took %v", elapsed)
	}
}
