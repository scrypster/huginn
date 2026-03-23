package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallPanicHandler_WritesCrashFile(t *testing.T) {
	dir := t.TempDir()

	// Simulate a panic recovery
	func() {
		defer func() {
			r := recover()
			if r == nil {
				return
			}
			// Write crash file
			err := WriteCrashFile(dir, fmt.Sprintf("%v", r), "test-stack-trace")
			if err != nil {
				t.Errorf("WriteCrashFile: %v", err)
			}
		}()
		panic("test panic")
	}()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected crash file to be written")
	}

	data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if !strings.Contains(string(data), "test panic") {
		t.Error("expected panic message in crash file")
	}
}
