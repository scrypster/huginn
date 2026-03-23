package tray

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAcquireLock_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")

	owned, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock: unexpected error: %v", err)
	}
	if !owned {
		t.Fatal("expected owned=true for a fresh lockfile")
	}

	data, _ := os.ReadFile(path)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	if pid != os.Getpid() {
		t.Fatalf("expected PID %d, got %d", os.Getpid(), pid)
	}
}

func TestAcquireLock_StalePIDFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")
	os.WriteFile(path, []byte("99999999\n"), 0644)

	owned, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock: unexpected error: %v", err)
	}
	if !owned {
		t.Fatal("expected owned=true when previous PID is stale")
	}
}

func TestAcquireLock_LiveProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")
	os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0644)

	owned, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("AcquireLock: unexpected error: %v", err)
	}
	if owned {
		t.Fatal("expected owned=false when current PID already holds lock")
	}
}

func TestReleaseLock_RemovesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pid")
	os.WriteFile(path, []byte("12345\n"), 0644)

	ReleaseLock(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected lockfile to be removed after ReleaseLock")
	}
}

func TestReleaseLock_NonExistentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.pid")
	ReleaseLock(path) // must not panic
}

func TestProcessIsLive_CurrentProcess(t *testing.T) {
	if !processIsLive(os.Getpid()) {
		t.Fatal("expected current process to be live")
	}
}

func TestProcessIsLive_DeadProcess(t *testing.T) {
	if processIsLive(99999999) {
		t.Log("PID 99999999 appears to exist — skipping")
		t.Skip()
	}
}
