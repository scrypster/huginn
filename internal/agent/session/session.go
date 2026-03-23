// Package session manages per-agent-session isolation: fake HOME directory,
// scoped CLI credentials, and PATH deny-shims for non-toolbelt tools.
package session

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Session holds the temp directory and env vars for one agent session.
type Session struct {
	// Dir is the session's temp HOME directory (mode 0700).
	Dir string
	// Env is the list of "KEY=VALUE" strings to inject into all child processes.
	Env []string
}

// Config carries the parameters needed to set up a session.
// Populate Provider-specific fields before calling Setup.
type Config struct {
}

// Setup creates the session temp directory and returns a populated Session.
// The caller must call Teardown when the session ends.
func Setup(cfg Config) (*Session, error) {
	dir, err := os.MkdirTemp(os.TempDir(), "huginn-session-")
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}

	// Create bin/ for deny shims.
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0700); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}

	sess := &Session{Dir: dir}

	// Write PID lockfile to mark this session as live.
	pidFile := filepath.Join(dir, ".pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("session: write pid file: %w", err)
	}

	// Base env: HOME and PATH overrides. BASH_ENV and ENV must be cleared
	// to prevent rc files from overriding our injected env vars.
	realPath := os.Getenv("PATH")
	sess.Env = []string{
		"HOME=" + dir,
		"PATH=" + binDir + ":" + realPath,
		"BASH_ENV=", // explicitly unset — prevents bash sourcing arbitrary files
		"ENV=",      // explicitly unset — POSIX sh equivalent of BASH_ENV
	}

	return sess, nil
}

// Teardown removes the session temp directory.
func (s *Session) Teardown() {
	if s.Dir != "" {
		os.RemoveAll(s.Dir)
	}
}

// pidIsAlive checks if the process with the given PID in the directory's .pid file is still running.
// Returns false if the .pid file doesn't exist, can't be read, or the process is not alive.
func pidIsAlive(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, ".pid"))
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// SweepStale removes any leftover huginn-session-* directories in the system
// temp dir, except those with live pids. Call once at Huginn startup to clean up after crashes.
func SweepStale() {
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "huginn-session-") {
			candidate := filepath.Join(os.TempDir(), e.Name())
			// Skip removal if the session has a live PID.
			if pidIsAlive(candidate) {
				continue
			}
			os.RemoveAll(candidate)
		}
	}
}

// contextKey is the unexported key for session env stored in context.
type contextKey struct{}

// WithEnv returns a new context carrying the given env slice.
func WithEnv(ctx context.Context, env []string) context.Context {
	return context.WithValue(ctx, contextKey{}, env)
}

// EnvFrom retrieves the session env from context, or nil if not set.
func EnvFrom(ctx context.Context) []string {
	if env, ok := ctx.Value(contextKey{}).([]string); ok {
		return env
	}
	return nil
}

// ToolbeltEntry carries the provider and profile information needed by
// BuildAndSetup. It mirrors agents.ToolbeltEntry to avoid an import cycle
// (agents → tools → session).
type ToolbeltEntry struct {
	Provider string
	Profile  string
}

// BuildAndSetup creates a fully configured session for the given toolbelt.
// It detects available CLI tools, populates scoped credentials for toolbelt
// tools, and installs deny-shims for non-toolbelt tools.
// The caller must call sess.Teardown() when the agent session ends.
func BuildAndSetup(toolbelt []ToolbeltEntry) (*Session, error) {
	sess, err := Setup(Config{})
	if err != nil {
		return nil, err
	}

	// If no toolbelt, no scoping needed — still create the session for HOME isolation.
	if len(toolbelt) == 0 {
		return sess, nil
	}

	// Build set of toolbelt binary names (for shim exclusion).
	toolbeltBinaries := make(map[string]bool)
	for _, entry := range toolbelt {
		if binary := BinaryForProvider(entry.Provider); binary != "" {
			toolbeltBinaries[binary] = true
		}
	}

	// Detect available CLIs before installing shims.
	detected := DetectAvailableCLIs()

	// Setup scoped credentials for each toolbelt entry.
	for _, entry := range toolbelt {
		switch entry.Provider {
		case "aws":
			if err := SetupAWS(sess, entry.Profile); err != nil {
				// Non-fatal: log and continue.
				slog.Warn("aws credential setup failed", "err", err)
			}
		case "github_cli":
			// gh binary path resolved from real PATH (before shims).
			if ghPath, err := exec.LookPath("gh"); err == nil {
				if err := SetupGH(sess, ghPath, entry.Profile); err != nil {
					slog.Warn("github_cli credential setup failed", "err", err)
				}
			}
		case "gcloud":
			SetupGCloud(sess, entry.Profile)
		}
	}

	// Install deny-shims for detected but non-toolbelt tools.
	InstallShims(sess, detected, toolbeltBinaries)

	return sess, nil
}
