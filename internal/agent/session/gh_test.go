package session_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agent/session"
)

func TestExtractGHToken_ReturnsError_WhenGHMissing(t *testing.T) {
	_, err := session.ExtractGHToken("/nonexistent/gh", "")
	if err == nil {
		t.Fatal("expected error when gh binary missing")
	}
}

func TestSetupGH_AddsGHTokenEnv(t *testing.T) {
	// Only run if gh is installed and authed
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		t.Skip("gh not installed, skipping")
	}

	sess, err := session.Setup(session.Config{})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer sess.Teardown()

	if err := session.SetupGH(sess, ghPath, ""); err != nil {
		t.Skipf("gh not authed (%v), skipping", err)
	}

	hasToken := false
	for _, e := range sess.Env {
		if strings.HasPrefix(e, "GH_TOKEN=") && len(e) > len("GH_TOKEN=") {
			hasToken = true
		}
	}
	if !hasToken {
		t.Error("expected GH_TOKEN= in session env with non-empty value")
	}

	hasConfigDir := false
	for _, e := range sess.Env {
		if strings.HasPrefix(e, "GH_CONFIG_DIR=") {
			val := strings.TrimPrefix(e, "GH_CONFIG_DIR=")
			if strings.HasSuffix(val, ".config/gh") {
				hasConfigDir = true
			}
		}
	}
	if !hasConfigDir {
		t.Error("expected GH_CONFIG_DIR= ending in .config/gh in session env")
	}

	// Verify the config dir was physically created
	for _, e := range sess.Env {
		if strings.HasPrefix(e, "GH_CONFIG_DIR=") {
			dirPath := strings.TrimPrefix(e, "GH_CONFIG_DIR=")
			info, err := os.Stat(dirPath)
			if err != nil {
				t.Fatalf("GH_CONFIG_DIR not created on disk: %v", err)
			}
			if info.Mode().Perm() != 0700 {
				t.Errorf("expected 0700 perms on config dir, got %o", info.Mode().Perm())
			}
			break
		}
	}
}

func TestSetupGH_CreatesConfigDir(t *testing.T) {
	// Create a fake gh binary that prints a fake token
	tmpDir := t.TempDir()
	fakeGH := filepath.Join(tmpDir, "gh")
	script := "#!/bin/bash\necho 'fake-token-12345'\n"
	if err := os.WriteFile(fakeGH, []byte(script), 0755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}

	sess, err := session.Setup(session.Config{})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer sess.Teardown()

	if err := session.SetupGH(sess, fakeGH, ""); err != nil {
		t.Fatalf("SetupGH: %v", err)
	}

	// Find GH_CONFIG_DIR in env
	var configDir string
	for _, e := range sess.Env {
		if strings.HasPrefix(e, "GH_CONFIG_DIR=") {
			configDir = strings.TrimPrefix(e, "GH_CONFIG_DIR=")
			break
		}
	}
	if configDir == "" {
		t.Fatal("GH_CONFIG_DIR not in session env")
	}

	// Verify directory was created with 0700 perms
	info, err := os.Stat(configDir)
	if err != nil {
		t.Fatalf("GH_CONFIG_DIR not created on disk: %v", err)
	}
	if info.Mode().Perm() != 0700 {
		t.Errorf("expected 0700 perms on config dir, got %o", info.Mode().Perm())
	}

	// Also verify GH_TOKEN is present and non-empty
	hasToken := false
	for _, e := range sess.Env {
		if strings.HasPrefix(e, "GH_TOKEN=") && len(e) > len("GH_TOKEN=") {
			hasToken = true
		}
	}
	if !hasToken {
		t.Error("expected GH_TOKEN= in session env with non-empty value")
	}
}

func TestExtractGHToken_PassesAccountFlag(t *testing.T) {
	// Create a fake gh binary that echoes a token only if -u someuser is passed
	tmpDir := t.TempDir()
	fakeGH := filepath.Join(tmpDir, "gh")
	// Script checks for "token" + "-u" args and prints token if account matches
	script := `#!/bin/bash
# Expects: auth token -u someuser
if [[ "$3" == "-u" && "$4" == "someuser" ]]; then
    echo "user-specific-token"
else
    echo "default-token"
fi
`
	if err := os.WriteFile(fakeGH, []byte(script), 0755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}

	token, err := session.ExtractGHToken(fakeGH, "someuser")
	if err != nil {
		t.Fatalf("ExtractGHToken: %v", err)
	}
	if token != "user-specific-token" {
		t.Errorf("expected user-specific-token, got %q (account flag may not be passed correctly)", token)
	}
}

func TestExtractGHToken_WhitespaceOutput(t *testing.T) {
	// Create a fake gh binary that exits 0 but prints only whitespace
	tmpDir := t.TempDir()
	fakeGH := filepath.Join(tmpDir, "gh")
	script := "#!/bin/sh\necho '   '\n"
	if err := os.WriteFile(fakeGH, []byte(script), 0755); err != nil {
		t.Fatalf("write fake gh: %v", err)
	}

	_, err := session.ExtractGHToken(fakeGH, "")
	if err == nil {
		t.Fatal("expected error for whitespace-only token output")
	}
}
