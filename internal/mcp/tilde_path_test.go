package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestExpandTilde_HomeDir verifies the canonical ~/ expansion helper resolves
// a leading "~/" against the user's home directory, and leaves other paths
// (absolute, relative, bare names) untouched.
func TestExpandTilde_HomeDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir available: %v", err)
	}

	got := expandTilde("~/.huginn/mcp-bin/node_modules/.bin/playwright-mcp")
	want := filepath.Join(home, ".huginn/mcp-bin/node_modules/.bin/playwright-mcp")
	if got != want {
		t.Errorf("expandTilde(~/...) = %q, want %q", got, want)
	}

	// Absolute paths are unchanged.
	if got := expandTilde("/usr/local/bin/node"); got != "/usr/local/bin/node" {
		t.Errorf("expandTilde(absolute) = %q, want unchanged", got)
	}

	// Bare $PATH names are unchanged.
	if got := expandTilde("node"); got != "node" {
		t.Errorf("expandTilde(bare) = %q, want unchanged", got)
	}

	// A lone "~" (no trailing slash) is left alone — only "~/" expands.
	if got := expandTilde("~"); got != "~" {
		t.Errorf("expandTilde(~) = %q, want unchanged", got)
	}
}

// TestCommandResolvable_TildePath verifies commandResolvable expands a
// leading "~/" before checking the filesystem, using a fake HOME with a
// real executable planted at the tilde-relative path.
func TestCommandResolvable_TildePath(t *testing.T) {
	fakeHome := t.TempDir()
	binDir := filepath.Join(fakeHome, ".huginn", "mcp-bin", "node_modules", ".bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(binDir, "playwright-mcp")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", fakeHome)

	cfg := MCPServerConfig{
		Name:    "playwright",
		Command: "~/.huginn/mcp-bin/node_modules/.bin/playwright-mcp",
	}
	if !commandResolvable(cfg) {
		t.Errorf("commandResolvable(~ path) = false, want true (binary exists at expanded path)")
	}

	// Absolute path behavior is unchanged: a real file still resolves.
	cfgAbs := MCPServerConfig{Name: "abs", Command: binPath}
	if !commandResolvable(cfgAbs) {
		t.Errorf("commandResolvable(absolute path) = false, want true")
	}

	// Absolute path behavior is unchanged: a missing file still fails.
	cfgMissing := MCPServerConfig{Name: "missing", Command: filepath.Join(fakeHome, "nope")}
	if commandResolvable(cfgMissing) {
		t.Errorf("commandResolvable(missing absolute path) = true, want false")
	}
}

// TestNewStdioTransport_TildePath verifies the spawn path (used by
// defaultClientFactory) also expands a leading "~/" so a configured tilde
// command actually executes instead of failing with "no such file".
func TestNewStdioTransport_TildePath(t *testing.T) {
	fakeHome := t.TempDir()
	binDir := filepath.Join(fakeHome, ".huginn", "mcp-bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(binDir, "true-ish.sh")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", fakeHome)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tr, err := NewStdioTransport(ctx, "~/.huginn/mcp-bin/true-ish.sh", nil, nil)
	if err != nil {
		t.Fatalf("NewStdioTransport(~ path) failed to spawn: %v", err)
	}
	defer tr.Close()
}
