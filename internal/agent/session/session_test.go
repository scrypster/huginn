package session_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agent/session"
)

func TestSetup_CreatesTempDirWith0700(t *testing.T) {
	sess, err := session.Setup(session.Config{})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer sess.Teardown()

	info, err := os.Stat(sess.Dir)
	if err != nil {
		t.Fatalf("stat session dir: %v", err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("expected 0700, got %o", info.Mode().Perm())
	}
}

func TestSetup_DirNameHasPrefix(t *testing.T) {
	sess, err := session.Setup(session.Config{})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer sess.Teardown()

	base := filepath.Base(sess.Dir)
	if !strings.HasPrefix(base, "huginn-session-") {
		t.Fatalf("expected huginn-session-* prefix, got %q", base)
	}
}

func TestTeardown_RemovesDir(t *testing.T) {
	sess, err := session.Setup(session.Config{})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	dir := sess.Dir
	sess.Teardown()

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("expected dir to be removed after Teardown")
	}
}

func TestContextRoundTrip(t *testing.T) {
	env := []string{"FOO=bar", "BAZ=qux"}
	ctx := session.WithEnv(context.Background(), env)
	got := session.EnvFrom(ctx)
	if !reflect.DeepEqual(got, env) {
		t.Fatalf("expected %v, got %v", env, got)
	}
}

func TestEnvFrom_NilWhenNoSession(t *testing.T) {
	got := session.EnvFrom(context.Background())
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestSweepStale_RemovesOrphanedDirs(t *testing.T) {
	// Capture an isolated dir before redirecting TMPDIR
	isolatedTmp := t.TempDir()
	// Redirect os.TempDir() to our isolated dir
	t.Setenv("TMPDIR", isolatedTmp)

	// Create a fake stale session dir inside the isolated tmp
	staleDir := filepath.Join(isolatedTmp, "huginn-session-stale-test-12345")
	if err := os.MkdirAll(staleDir, 0700); err != nil {
		t.Fatalf("create stale dir: %v", err)
	}

	// Also create a non-session dir that should NOT be removed
	otherDir := filepath.Join(isolatedTmp, "not-a-huginn-session")
	if err := os.MkdirAll(otherDir, 0700); err != nil {
		t.Fatalf("create other dir: %v", err)
	}

	session.SweepStale()

	// Verify stale session dir was removed
	if _, err := os.Stat(staleDir); !os.IsNotExist(err) {
		t.Error("expected stale session dir to be removed by SweepStale")
	}

	// Verify non-session dir was preserved
	if _, err := os.Stat(otherDir); err != nil {
		t.Error("expected non-session dir to be preserved by SweepStale")
	}
}

func TestSweepStale_PreservesLiveSession(t *testing.T) {
	// Set up isolated TMPDIR
	isolatedTmp := t.TempDir()
	t.Setenv("TMPDIR", isolatedTmp)

	// Create a live session dir
	liveDir := filepath.Join(isolatedTmp, "huginn-session-live")
	if err := os.MkdirAll(liveDir, 0700); err != nil {
		t.Fatalf("create live dir: %v", err)
	}

	// Write the current process PID to the .pid file
	pidFile := filepath.Join(liveDir, ".pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatalf("write pid file: %v", err)
	}

	// Call SweepStale
	session.SweepStale()

	// Verify the live session dir still exists
	if _, err := os.Stat(liveDir); err != nil {
		t.Error("expected live session dir to be preserved by SweepStale")
	}
}

func TestSetup_EnvContainsExpectedVars(t *testing.T) {
	sess, err := session.Setup(session.Config{})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer sess.Teardown()

	// Build a map from the session's Env slice
	envMap := make(map[string]string)
	for _, pair := range sess.Env {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		} else if len(parts) == 1 {
			envMap[parts[0]] = ""
		}
	}

	// Assert: HOME equals sess.Dir
	if home, ok := envMap["HOME"]; !ok || home != sess.Dir {
		t.Fatalf("expected HOME=%s, got %q", sess.Dir, home)
	}

	// Assert: PATH starts with sess.Dir + "/bin:"
	if path, ok := envMap["PATH"]; !ok {
		t.Fatal("PATH not set in env")
	} else if !strings.HasPrefix(path, sess.Dir+"/bin:") {
		t.Fatalf("expected PATH to start with %q, got %q", sess.Dir+"/bin:", path)
	}

	// Assert: BASH_ENV is present and empty
	if bashEnv, ok := envMap["BASH_ENV"]; !ok {
		t.Fatal("BASH_ENV not set in env")
	} else if bashEnv != "" {
		t.Fatalf("expected BASH_ENV to be empty, got %q", bashEnv)
	}

	// Assert: ENV is present and empty
	if envVar, ok := envMap["ENV"]; !ok {
		t.Fatal("ENV not set in env")
	} else if envVar != "" {
		t.Fatalf("expected ENV to be empty, got %q", envVar)
	}
}

func TestSetup_PIDFileWritten(t *testing.T) {
	sess, err := session.Setup(session.Config{})
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer sess.Teardown()

	// Read the .pid file
	pidFilePath := filepath.Join(sess.Dir, ".pid")
	data, err := os.ReadFile(pidFilePath)
	if err != nil {
		t.Fatalf("failed to read .pid file: %v", err)
	}

	// Parse the PID from the file
	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		t.Fatalf("failed to parse PID from file: %v", err)
	}

	// Assert the PID equals the current process PID
	if pid != os.Getpid() {
		t.Fatalf("expected .pid file to contain %d, got %d", os.Getpid(), pid)
	}
}

func TestTeardown_ZeroValueSession(t *testing.T) {
	// Create a zero-value Session and call Teardown
	s := session.Session{}
	// This should not panic
	s.Teardown()
	// If we reach here without panic, the test passes
}
