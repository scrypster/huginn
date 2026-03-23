package notepad

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDefaultManager_NoProjectRoot verifies that DefaultManager works without a
// project root, building only a global dir path.
func TestDefaultManager_NoProjectRoot(t *testing.T) {
	m, err := DefaultManager("")
	if err != nil {
		t.Fatalf("DefaultManager: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil Manager")
	}
	// projectDir should be empty when no projectRoot given.
	if m.projectDir != "" {
		t.Errorf("expected empty projectDir, got %q", m.projectDir)
	}
}

// TestDefaultManager_WithProjectRoot verifies that DefaultManager sets
// projectDir when a project root is provided.
func TestDefaultManager_WithProjectRoot(t *testing.T) {
	dir := t.TempDir()
	m, err := DefaultManager(dir)
	if err != nil {
		t.Fatalf("DefaultManager: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil Manager")
	}
	want := filepath.Join(dir, ".huginn", "notepads")
	if m.projectDir != want {
		t.Errorf("projectDir = %q, want %q", m.projectDir, want)
	}
}

// TestManager_loadDir_ReadError covers the non-NotExist error branch in loadDir
// by making the directory unreadable.
func TestManager_loadDir_ReadError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permission checks")
	}
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "notepads")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// Remove execute+read permissions so ReadDir fails.
	if err := os.Chmod(dir, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0755) //nolint:errcheck

	m := NewManager(dir, "")
	_, err := m.Load()
	if err == nil {
		t.Error("expected error when directory is unreadable, got nil")
	}
}
