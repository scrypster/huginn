package session_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agent/session"
)

func TestWriteDenyShim_CreatesExecutableScript(t *testing.T) {
	sess, err := session.Setup(session.Config{})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer sess.Teardown()

	if err := session.WriteDenyShim(sess, "faketool"); err != nil {
		t.Fatalf("WriteDenyShim: %v", err)
	}

	shimPath := filepath.Join(sess.Dir, "bin", "faketool")
	info, err := os.Stat(shimPath)
	if err != nil {
		t.Fatalf("shim not created: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Error("shim should be executable")
	}

	// Running it should exit non-zero
	cmd := exec.Command(shimPath)
	if err := cmd.Run(); err == nil {
		t.Error("expected deny shim to exit non-zero")
	}
}

func TestWriteDenyShim_OutputMentionsTool(t *testing.T) {
	sess, err := session.Setup(session.Config{})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer sess.Teardown()

	session.WriteDenyShim(sess, "aws")

	shimPath := filepath.Join(sess.Dir, "bin", "aws")
	b, _ := os.ReadFile(shimPath)
	if !strings.Contains(string(b), "aws") {
		t.Error("shim content should mention the tool name")
	}
	if !strings.Contains(string(b), "toolbelt") {
		t.Error("shim content should mention toolbelt")
	}
}

func TestInstallShims_BlocksDetectedNonToolbeltTools(t *testing.T) {
	sess, err := session.Setup(session.Config{})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer sess.Teardown()

	detected := []string{"faketool", "allowedtool"}
	toolbeltBinaries := map[string]bool{"allowedtool": true}

	session.InstallShims(sess, detected, toolbeltBinaries)

	if _, err := os.Stat(filepath.Join(sess.Dir, "bin", "faketool")); err != nil {
		t.Error("expected deny shim for faketool")
	}
	if _, err := os.Stat(filepath.Join(sess.Dir, "bin", "allowedtool")); err == nil {
		t.Error("allowedtool should NOT have a deny shim (it's in toolbelt)")
	}
}

func TestBinaryForProvider_KnownProviders(t *testing.T) {
	cases := []struct {
		provider string
		want     string
	}{
		{"aws", "aws"},
		{"github_cli", "gh"},
		{"gcloud", "gcloud"},
		{"kubectl", "kubectl"},
		{"terraform", "terraform"},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			got := session.BinaryForProvider(tc.provider)
			if got != tc.want {
				t.Errorf("BinaryForProvider(%q) = %q, want %q", tc.provider, got, tc.want)
			}
		})
	}
}

func TestBinaryForProvider_UnknownProvider(t *testing.T) {
	got := session.BinaryForProvider("unknown_provider")
	if got != "" {
		t.Errorf("BinaryForProvider(unknown) = %q, want empty string", got)
	}
}

func TestDetectAvailableCLIs_ReturnsOnlyInstalledBinaries(t *testing.T) {
	// Create a temp dir with a fake "aws" binary
	binDir := t.TempDir()
	fakeAWS := filepath.Join(binDir, "aws")
	if err := os.WriteFile(fakeAWS, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	// Prepend our fake bindir to PATH so LookPath finds it
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath)

	detected := session.DetectAvailableCLIs()

	found := false
	for _, name := range detected {
		if name == "aws" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'aws' in DetectAvailableCLIs output, got %v", detected)
	}
}

func TestDetectAvailableCLIs_ResultIsSorted(t *testing.T) {
	// Create fake binaries for two known CLIs: gh and aws
	binDir := t.TempDir()
	for _, name := range []string{"gh", "aws"} {
		p := filepath.Join(binDir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath)

	result := session.DetectAvailableCLIs()
	if !sort.StringsAreSorted(result) {
		t.Errorf("DetectAvailableCLIs result is not sorted: %v", result)
	}
}

func TestWriteDenyShim_NilSession(t *testing.T) {
	err := session.WriteDenyShim(nil, "aws")
	if err == nil {
		t.Error("expected error for nil session")
	}
}

func TestInstallShims_NilSession(t *testing.T) {
	// Should not panic
	session.InstallShims(nil, []string{"aws"}, map[string]bool{"aws": true})
}

func TestWriteDenyShim_ExecutionBlocksWithCorrectExitAndMessage(t *testing.T) {
	sess, err := session.Setup(session.Config{})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer sess.Teardown()

	if err := session.WriteDenyShim(sess, "terraform"); err != nil {
		t.Fatalf("WriteDenyShim: %v", err)
	}

	shimPath := filepath.Join(sess.Dir, "bin", "terraform")
	cmd := exec.Command(shimPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err = cmd.Run()

	// Must exit non-zero
	if err == nil {
		t.Fatal("expected deny shim to exit non-zero, but it exited 0")
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitCode() != 1 {
		t.Errorf("exit code = %d, want 1", exitErr.ExitCode())
	}

	// Stderr must mention the binary name and "toolbelt"
	msg := stderr.String()
	if !strings.Contains(msg, "terraform") {
		t.Errorf("stderr missing binary name 'terraform'; got: %q", msg)
	}
	if !strings.Contains(msg, "toolbelt") {
		t.Errorf("stderr missing word 'toolbelt'; got: %q", msg)
	}
}
