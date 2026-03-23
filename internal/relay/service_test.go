package relay_test

import (
	"os"
	"testing"

	"github.com/scrypster/huginn/internal/relay"
)

func TestServiceManager_InstallUninstall(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := relay.NewServiceManagerForDir(tmpDir)

	binaryPath := "/usr/local/bin/huginn"
	if err := mgr.Install(binaryPath); err != nil {
		t.Fatalf("Install: %v", err)
	}

	files, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("Install wrote no files")
	}

	if err := mgr.Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	files, err = os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("Uninstall left %d files", len(files))
	}
}

// TestServiceManager_Install_Overwrite verifies that calling Install() twice
// with different binary paths overwrites the existing plist idempotently.
func TestServiceManager_Install_Overwrite(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := relay.NewServiceManagerForDir(tmpDir)

	binaryPath1 := "/usr/local/bin/huginn"
	binaryPath2 := "/opt/huginn/bin/huginn"

	// First install
	if err := mgr.Install(binaryPath1); err != nil {
		t.Fatalf("first Install: %v", err)
	}

	files, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("first Install wrote no files")
	}

	// Second install with different binary path (overwrite)
	if err := mgr.Install(binaryPath2); err != nil {
		t.Fatalf("second Install: %v", err)
	}

	// Verify still one file (overwritten, not duplicated)
	files, err = os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file after overwrite, got %d", len(files))
	}

	// Verify IsInstalled still returns true (idempotent overwrite)
	if !mgr.IsInstalled() {
		t.Fatal("expected IsInstalled to return true after second Install")
	}
}
