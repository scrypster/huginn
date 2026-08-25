package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHomeIsSandboxed guards the whole package. Handlers such as
// handleUpdateConfig and handleRestoreActiveState persist via config.Save(),
// which resolves os.UserHomeDir(). If TestMain stops redirecting HOME, running
// `go test ./...` silently overwrites the developer's real
// ~/.huginn/config.json — this has happened.
func TestHomeIsSandboxed(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	if !strings.Contains(filepath.Base(home), "huginn-server-test-") {
		t.Fatalf("os.UserHomeDir() = %q — TestMain must redirect HOME into a temp dir, "+
			"or handlers calling config.Save() will overwrite the real ~/.huginn/config.json", home)
	}
}

// TestConfigSaveLandsInSandbox proves the redirect actually captures a real
// handler write, not merely that the env var is set.
func TestConfigSaveLandsInSandbox(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	target := filepath.Join(home, ".huginn", "config.json")
	_ = os.Remove(target)

	srv, _ := newTestServer(t)
	if err := srv.cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, err := os.Stat(target); err != nil {
		t.Fatalf("config.Save() did not write inside the sandbox home (%s): %v", target, err)
	}
}
