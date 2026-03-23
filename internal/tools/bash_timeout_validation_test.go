package tools

import (
	"context"
	"testing"
)

// TestBashTool_NegativeTimeout verifies that a negative timeout value is
// rejected with an error rather than silently accepted.
func TestBashTool_NegativeTimeout_Float64(t *testing.T) {
	t.Parallel()

	tool := &BashTool{SandboxRoot: t.TempDir()}
	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo hello",
		"timeout": float64(-1),
	})

	if !result.IsError {
		t.Fatal("expected error for negative timeout, got success")
	}
	if result.Error != "bash: timeout must be non-negative" {
		t.Errorf("unexpected error message: %q", result.Error)
	}
}

// TestBashTool_NegativeTimeoutInt verifies the int type path also rejects negatives.
func TestBashTool_NegativeTimeoutInt(t *testing.T) {
	t.Parallel()

	tool := &BashTool{SandboxRoot: t.TempDir()}
	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo hello",
		"timeout": int(-5),
	})

	if !result.IsError {
		t.Fatal("expected error for negative int timeout, got success")
	}
}

// TestBashTool_ZeroTimeoutUsesDefault verifies that timeout=0 falls through to
// the default (120s) without error.
func TestBashTool_ZeroTimeoutValidation(t *testing.T) {
	t.Parallel()

	tool := &BashTool{SandboxRoot: t.TempDir()}
	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo ok",
		"timeout": float64(0),
	})

	if result.IsError {
		t.Fatalf("expected success for timeout=0, got error: %s", result.Error)
	}
}

// TestBashTool_TimeoutExceedsMaxIsCapped verifies that a timeout larger than
// bashMaxTimeout (3600 s) is silently capped and execution still succeeds for
// a fast command. We cannot assert the exact duration used, but we can assert
// no error is returned due to the cap logic itself.
func TestBashTool_TimeoutExceedsMaxIsCapped(t *testing.T) {
	t.Parallel()

	tool := &BashTool{SandboxRoot: t.TempDir()}
	// 7200 > bashMaxTimeout (3600) → should be capped, not rejected
	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo capped",
		"timeout": float64(7200),
	})

	if result.IsError {
		t.Fatalf("expected capped execution to succeed, got error: %s", result.Error)
	}
}

// TestBashTool_PositiveTimeoutRespected verifies a positive timeout allows
// normal command execution.
func TestBashTool_PositiveTimeoutRespected(t *testing.T) {
	t.Parallel()

	tool := &BashTool{SandboxRoot: t.TempDir()}
	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo hello",
		"timeout": float64(10),
	})

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
}
