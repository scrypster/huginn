package runtime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManager_IsInstalled_false(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	if mgr.IsInstalled() {
		t.Error("expected not installed on empty temp dir")
	}
}

func TestManager_BinaryPath(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	bp := mgr.BinaryPath()
	if bp == "" {
		t.Error("BinaryPath should not be empty")
	}
}

func TestManager_Endpoint(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}
	mgr.port = 8765
	if mgr.Endpoint() != "http://localhost:8765" {
		t.Errorf("unexpected endpoint: %s", mgr.Endpoint())
	}
}

func TestManager_IsInstalled_true(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Create the expected binary path structure.
	binPath := mgr.BinaryPath()
	if err := os.MkdirAll(filepath.Dir(binPath), 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	f, err := os.Create(binPath)
	if err != nil {
		t.Fatalf("Create binary: %v", err)
	}
	f.Close()

	if !mgr.IsInstalled() {
		t.Error("expected IsInstalled() == true after creating binary file")
	}
}

func TestWaitForReady_ProcessDies(t *testing.T) {
	dir := t.TempDir()
	mgr, err := NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Use a command that exits immediately with failure.
	mgr.cmd = exec.Command("sh", "-c", "exit 1")
	mgr.port = 19999 // port nothing is listening on

	if err := mgr.cmd.Start(); err != nil {
		t.Fatalf("cmd.Start: %v", err)
	}

	// Wait for the process to actually exit so ProcessState is set.
	// We call cmd.Wait() here in the test; WaitForReady uses ProcessState polling.
	done := make(chan error, 1)
	go func() { done <- mgr.cmd.Wait() }()
	select {
	case <-done:
		// process exited — ProcessState is now set
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit within 5s")
	}

	start := time.Now()
	err = mgr.WaitForReady(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected WaitForReady to return an error, got nil")
	}
	if !strings.Contains(err.Error(), "exited") {
		t.Errorf("expected error to mention process exit, got: %v", err)
	}
	// Should fail fast — well under the 30s timeout.
	if elapsed > 5*time.Second {
		t.Errorf("WaitForReady took too long (%v), expected fast failure", elapsed)
	}
}
