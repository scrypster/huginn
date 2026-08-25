package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeClaude(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", "fake-claude.sh"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if err := os.Chmod(p, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	return p
}

func TestBuildArgsUsesAssignedSessionID(t *testing.T) {
	cfg := DefaultConfig().Delegate
	args := BuildArgs(cfg, DelegateRequest{Prompt: "hi"}, "SID-1")
	joined := strings.Join(args, " ")

	for _, want := range []string{
		"-p", "hi",
		"--session-id SID-1",
		"--output-format stream-json",
		"--verbose",
		"--permission-mode acceptEdits",
		"--max-turns 30",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %v", want, args)
		}
	}
}

func TestBuildArgsNeverSkipsPermissionsByDefault(t *testing.T) {
	cfg := DefaultConfig().Delegate
	args := BuildArgs(cfg, DelegateRequest{Prompt: "hi"}, "SID-1")
	if strings.Contains(strings.Join(args, " "), "dangerously-skip-permissions") {
		t.Fatal("default args include --dangerously-skip-permissions")
	}

	cfg.SkipPermissions = true
	args = BuildArgs(cfg, DelegateRequest{Prompt: "hi"}, "SID-1")
	if !strings.Contains(strings.Join(args, " "), "--dangerously-skip-permissions") {
		t.Error("SkipPermissions=true did not add the flag")
	}
}

func TestDelegateParsesResult(t *testing.T) {
	var events []StreamEvent
	res, err := Delegate(
		context.Background(),
		DefaultConfig().Delegate,
		fakeClaude(t),
		DelegateRequest{Prompt: "rename the function"},
		"SID-1",
		func(e StreamEvent) { events = append(events, e) },
	)
	if err != nil {
		t.Fatalf("Delegate: %v", err)
	}
	if res.Text != "Done: renamed the function." {
		t.Errorf("Text = %q", res.Text)
	}
	if res.CostUSD != 0.0123 {
		t.Errorf("CostUSD = %v, want 0.0123", res.CostUSD)
	}
	if res.NumTurns != 3 {
		t.Errorf("NumTurns = %d, want 3", res.NumTurns)
	}
	if res.DurationMS != 4210 {
		t.Errorf("DurationMS = %d, want 4210", res.DurationMS)
	}
	if res.IsError {
		t.Error("IsError = true for a successful run")
	}

	var sawText, sawTool bool
	for _, e := range events {
		if e.Type == "text" && e.Text == "Working on it." {
			sawText = true
		}
		if e.Type == "tool_use" && e.ToolName == "Read" {
			sawTool = true
		}
	}
	if !sawText {
		t.Error("no text stream event")
	}
	if !sawTool {
		t.Error("no tool_use stream event")
	}
}

func TestDelegateMissingBinary(t *testing.T) {
	res, err := Delegate(
		context.Background(),
		DefaultConfig().Delegate,
		filepath.Join(t.TempDir(), "no-such-claude"),
		DelegateRequest{Prompt: "hi"},
		"SID-1",
		nil,
	)
	if err == nil {
		t.Fatal("Delegate with a missing binary returned nil error")
	}
	if !res.IsError {
		t.Error("IsError = false for a missing binary")
	}
}

func TestDelegateTimeoutReturnsPartialOutput(t *testing.T) {
	dir := t.TempDir()
	slow := filepath.Join(dir, "slow-claude.sh")
	script := "#!/bin/sh\n" +
		"echo '{\"type\":\"assistant\",\"message\":{\"role\":\"assistant\",\"content\":[{\"type\":\"text\",\"text\":\"starting\"}]}}'\n" +
		"sleep 30\n"
	if err := os.WriteFile(slow, []byte(script), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()

	start := time.Now()
	res, err := Delegate(ctx, DefaultConfig().Delegate, slow,
		DelegateRequest{Prompt: "hi"}, "SID-1", nil)
	if err == nil {
		t.Fatal("Delegate returned nil error after a timeout")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("Delegate took %v after a 700ms deadline — the process was not killed", elapsed)
	}
	if !res.IsError {
		t.Error("IsError = false after a timeout")
	}
}
