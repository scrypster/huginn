package runtime

import (
	"fmt"
	"net"
	"os"
	"testing"
)

func TestFindFreePort_ReturnsValidPort(t *testing.T) {
	port, err := FindFreePort()
	if err != nil {
		t.Fatalf("FindFreePort: %v", err)
	}
	if port < 1024 || port > 65535 {
		t.Errorf("unexpected port: %d", port)
	}
}

func TestFindFreePort_PortIsActuallyFree(t *testing.T) {
	port, err := FindFreePort()
	if err != nil {
		t.Fatalf("FindFreePort: %v", err)
	}
	// Verify we can bind to it immediately after
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Errorf("port %d not free after FindFreePort: %v", port, err)
		return
	}
	l.Close()
}

func TestFindFreePort_IsAvailable(t *testing.T) {
	port, err := FindFreePort()
	if err != nil {
		t.Fatalf("FindFreePort: %v", err)
	}
	if port < 1024 || port > 65535 {
		t.Errorf("port out of range: %d", port)
	}
	// Verify port is actually usable
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Errorf("port %d not usable: %v", port, err)
		return
	}
	l.Close()
}

func TestFindFreePort_LoopbackOnly(t *testing.T) {
	port, err := FindFreePort()
	if err != nil {
		t.Fatalf("FindFreePort: %v", err)
	}
	// Should be bindable on 127.0.0.1 specifically
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Skipf("port %d not available: %v", port, err)
	}
	l.Close()
}

func TestWriteAndReadPIDFile(t *testing.T) {
	f, err := os.CreateTemp("", "huginn-pid-test-*")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	os.Remove(f.Name())

	if err := WritePIDFile(f.Name(), 12345, 8765); err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}
	pid, port, err := ReadPIDFile(f.Name())
	if err != nil {
		t.Fatalf("ReadPIDFile: %v", err)
	}
	if pid != 12345 || port != 8765 {
		t.Errorf("got pid=%d port=%d, want 12345 8765", pid, port)
	}
	os.Remove(f.Name())
}

func TestReadPIDFile_Missing(t *testing.T) {
	pid, port, err := ReadPIDFile("/nonexistent/path/huginn.pid")
	if err != nil {
		t.Errorf("expected nil error for missing file, got %v", err)
	}
	if pid != 0 || port != 0 {
		t.Errorf("expected 0,0 for missing file, got %d,%d", pid, port)
	}
}

func TestCleanupZombie_MissingFile(t *testing.T) {
	// Should not panic on missing file
	CleanupZombie("/nonexistent/huginn.pid")
}

func TestReadPIDFile_Corrupt(t *testing.T) {
	f, err := os.CreateTemp("", "huginn-pid-corrupt-*")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("not valid pid file content with more words")
	f.Close()
	defer os.Remove(f.Name())

	pid, port, err := ReadPIDFile(f.Name())
	if err != nil {
		t.Errorf("corrupt file should return 0,0,nil; got err=%v", err)
	}
	if pid != 0 || port != 0 {
		t.Errorf("corrupt file: got %d,%d, want 0,0", pid, port)
	}
}

func TestWritePIDFile_Permissions(t *testing.T) {
	f, err := os.CreateTemp("", "huginn-pid-perm-*")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	os.Remove(f.Name())

	if err := WritePIDFile(f.Name(), 99999, 9999); err != nil {
		t.Fatalf("WritePIDFile: %v", err)
	}
	defer os.Remove(f.Name())

	info, err := os.Stat(f.Name())
	if err != nil {
		t.Fatalf("stat pid file: %v", err)
	}
	// File should be readable/writable by owner only (0600)
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected file mode 0600, got %04o", info.Mode().Perm())
	}
}

func TestIsProcessAlive_CurrentProcess(t *testing.T) {
	// The current process is definitely alive.
	if !IsProcessAlive(os.Getpid()) {
		t.Error("IsProcessAlive should return true for the current process")
	}
}

func TestIsProcessAlive_InvalidPID(t *testing.T) {
	// PID 0 or negative PID should not be alive (or at least not panic).
	// On most Unix systems, signal(0, 0) to PID 0 sends to the process group,
	// so we use a very large unlikely PID instead.
	// We just verify it doesn't panic.
	_ = IsProcessAlive(999999999)
}

func TestCleanupZombie_RemovesPIDFile(t *testing.T) {
	f, err := os.CreateTemp("", "huginn-zombie-*")
	if err != nil {
		t.Fatal(err)
	}
	// Write a PID that definitely doesn't exist (use an absurdly large number).
	f.WriteString("999999999 8765\n")
	f.Close()

	CleanupZombie(f.Name())

	// File should be removed.
	if _, err := os.Stat(f.Name()); !os.IsNotExist(err) {
		os.Remove(f.Name()) // clean up in case test fails
		t.Error("expected PID file to be removed after CleanupZombie")
	}
}

func TestReadPIDFile_InvalidPIDFormat(t *testing.T) {
	f, err := os.CreateTemp("", "huginn-pid-invalid-*")
	if err != nil {
		t.Fatal(err)
	}
	// Write non-numeric PID/port
	f.WriteString("notanumber 8765\n")
	f.Close()
	defer os.Remove(f.Name())

	pid, port, err := ReadPIDFile(f.Name())
	if err != nil {
		t.Errorf("expected nil error for file with non-numeric values, got %v", err)
	}
	// strconv.Atoi returns 0 for non-numeric strings
	if pid != 0 || port != 8765 {
		t.Errorf("expected 0,8765; got %d,%d", pid, port)
	}
}

func TestReadPIDFile_OnlyOnePart(t *testing.T) {
	f, err := os.CreateTemp("", "huginn-pid-onepart-*")
	if err != nil {
		t.Fatal(err)
	}
	// Write only one value
	f.WriteString("12345\n")
	f.Close()
	defer os.Remove(f.Name())

	pid, port, err := ReadPIDFile(f.Name())
	if err != nil {
		t.Errorf("expected nil error for one-part file, got %v", err)
	}
	// Should return 0,0 for malformed file
	if pid != 0 || port != 0 {
		t.Errorf("expected 0,0; got %d,%d", pid, port)
	}
}

func TestReadPIDFile_ExtraWhitespace(t *testing.T) {
	f, err := os.CreateTemp("", "huginn-pid-whitespace-*")
	if err != nil {
		t.Fatal(err)
	}
	// Write with extra whitespace
	f.WriteString("  12345    8765  \n")
	f.Close()
	defer os.Remove(f.Name())

	pid, port, err := ReadPIDFile(f.Name())
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if pid != 12345 || port != 8765 {
		t.Errorf("expected 12345,8765; got %d,%d", pid, port)
	}
}

func TestIsProcessAlive_NonExistentProcess(t *testing.T) {
	// Use an absurdly large PID that shouldn't exist
	alive := IsProcessAlive(999999999)
	if alive {
		t.Error("non-existent process should not be alive")
	}
}

func TestCleanupZombie_WithValidReadError(t *testing.T) {
	// Test when ReadPIDFile returns error (other than not existing)
	// Create a file with read error (we'll make it unreadable)
	f, err := os.CreateTemp("", "huginn-zombie-read-*")
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("12345 8765\n")
	f.Close()

	// Remove read permissions
	os.Chmod(f.Name(), 0000)
	defer func() {
		os.Chmod(f.Name(), 0600)
		os.Remove(f.Name())
	}()

	// CleanupZombie should handle read errors gracefully
	// It should attempt to remove the file
	CleanupZombie(f.Name())

	// Restore permissions for cleanup
	os.Chmod(f.Name(), 0600)
}
