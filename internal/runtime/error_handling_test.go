package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestManager_BinaryPath_Construction verifies the binary path is correctly constructed.
func TestManager_BinaryPath_Construction(t *testing.T) {
	huginnDir := "/home/user/.huginn"
	m := &Manager{
		huginnDir: huginnDir,
		manifest: &RuntimeManifest{
			LlamaServerVersion: "1.2.3",
		},
	}

	binPath := m.BinaryPath()
	expected := filepath.Join(huginnDir, "bin", "llama-server-1.2.3", "llama-server")
	if binPath != expected {
		t.Errorf("expected BinaryPath=%q, got %q", expected, binPath)
	}
}

// TestManager_IsInstalled_NotExists verifies IsInstalled returns false for non-existent binary.
func TestManager_IsInstalled_NotExists(t *testing.T) {
	m := &Manager{
		huginnDir: t.TempDir(),
		manifest: &RuntimeManifest{
			LlamaServerVersion: "1.2.3",
		},
	}

	if m.IsInstalled() {
		t.Error("IsInstalled should return false when binary does not exist")
	}
}

// TestManager_IsInstalled_Exists verifies IsInstalled returns true when binary exists.
func TestManager_IsInstalled_Exists(t *testing.T) {
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "bin", "llama-server-1.0.0")
	if err := os.MkdirAll(binPath, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	binaryFile := filepath.Join(binPath, "llama-server")
	if err := os.WriteFile(binaryFile, []byte("mock binary"), 0755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	m := &Manager{
		huginnDir: tmpDir,
		manifest: &RuntimeManifest{
			LlamaServerVersion: "1.0.0",
		},
	}

	if !m.IsInstalled() {
		t.Error("IsInstalled should return true when binary exists")
	}
}

// TestManager_Download_NoBinaryForPlatform verifies Download errors when no binary is available.
func TestManager_Download_NoBinaryForPlatform(t *testing.T) {
	m := &Manager{
		huginnDir: t.TempDir(),
		platform:  Platform{OS: "unknown-os", Arch: "unknown-arch"},
		manifest: &RuntimeManifest{
			Binaries: make(map[string]BinaryEntry), // no binaries
		},
	}

	err := m.Download(context.Background(), nil)
	if err == nil {
		t.Error("Download should error when no binary available for platform")
	}
}

// TestManager_Shutdown_WithoutStart verifies Shutdown on unstarted manager is safe.
func TestManager_Shutdown_WithoutStart(t *testing.T) {
	m := &Manager{
		huginnDir: t.TempDir(),
		manifest: &RuntimeManifest{
			LlamaServerVersion: "1.0.0",
		},
	}

	err := m.Shutdown()
	if err != nil {
		t.Errorf("Shutdown without Start should not error, got: %v", err)
	}
}

// TestManager_Port_UnsetInitially verifies port is unset initially.
func TestManager_Port_Unset(t *testing.T) {
	m := &Manager{
		huginnDir: t.TempDir(),
		manifest: &RuntimeManifest{
			LlamaServerVersion: "1.0.0",
		},
		port: 0,
	}

	if m.port != 0 {
		t.Errorf("expected port=0 initially, got %d", m.port)
	}
}

// TestManager_ModelPath_Storage verifies modelPath can be set and retrieved.
func TestManager_ModelPath_Storage(t *testing.T) {
	m := &Manager{
		huginnDir:  t.TempDir(),
		modelPath: "",
		manifest: &RuntimeManifest{
			LlamaServerVersion: "1.0.0",
		},
	}

	if m.modelPath != "" {
		t.Error("modelPath should initially be empty")
	}

	m.modelPath = "/path/to/model.gguf"
	if m.modelPath != "/path/to/model.gguf" {
		t.Errorf("expected modelPath=/path/to/model.gguf, got %q", m.modelPath)
	}
}
