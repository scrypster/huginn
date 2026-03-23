package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLogger_RotationSizeCap verifies that when more than 10MB is written,
// the logger rotates the file and creates a fresh log file.
func TestLogger_RotationSizeCap(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "test.log")

	l, err := New(logPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	// Lower the rotation threshold to 64KB for the test to avoid writing 10MB.
	l.maxSizeBytes = 64 * 1024

	// Write just over the threshold.
	chunk := []byte(strings.Repeat("x", 1024)) // 1 KB
	for i := 0; i < 70; i++ {                   // 70 KB total > 64 KB threshold
		if _, err := l.Write(chunk); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	// The rotated file (.1) should exist.
	rotated := logPath + ".1"
	if _, err := os.Stat(rotated); os.IsNotExist(err) {
		t.Error("expected rotated file huginn.log.1 to exist after size cap exceeded")
	}

	// The active log file should still exist.
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Error("active log file should still exist after rotation")
	}
}

// TestLogger_RotationKeeps3Files verifies that multiple rotation cycles keep
// at most maxRotatedFiles rotated copies.
func TestLogger_RotationKeeps3Files(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "multi.log")

	l, err := New(logPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	// Use a tiny threshold so each write cycle rotates.
	l.maxSizeBytes = 128

	chunk := make([]byte, 200) // always exceeds threshold

	// Trigger 5 rotations.
	for i := 0; i < 5; i++ {
		if _, err := l.Write(chunk); err != nil {
			t.Fatalf("Write round %d: %v", i, err)
		}
	}

	// .4 should NOT exist (we keep at most maxRotatedFiles=3).
	for i := maxRotatedFiles + 1; i <= 5; i++ {
		extra := filepath.Join(dir, "multi.log."+string(rune('0'+i)))
		if _, err := os.Stat(extra); err == nil {
			t.Errorf("unexpected rotated file multi.log.%d — should only keep %d rotated files", i, maxRotatedFiles)
		}
	}
}

// TestLogger_NoRotationBelowCap verifies that files below the threshold are
// not rotated.
func TestLogger_NoRotationBelowCap(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "small.log")

	l, err := New(logPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	// Write a tiny amount.
	if _, err := l.Write([]byte("hello world\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	rotated := logPath + ".1"
	if _, err := os.Stat(rotated); err == nil {
		t.Error("should not rotate when below size cap")
	}
}
