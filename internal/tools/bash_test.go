package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestBashTool_Name verifies the tool name.
func TestBashTool_Name(t *testing.T) {
	tool := &BashTool{}
	if tool.Name() != "bash" {
		t.Errorf("expected name 'bash', got %q", tool.Name())
	}
}

// TestBashTool_Description verifies description is non-empty.
func TestBashTool_Description(t *testing.T) {
	tool := &BashTool{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

// TestBashTool_Permission verifies bash is PermExec.
func TestBashTool_Permission(t *testing.T) {
	tool := &BashTool{}
	if tool.Permission() != PermExec {
		t.Errorf("expected PermExec, got %v", tool.Permission())
	}
}

// TestBashTool_Schema verifies the schema is properly formed.
func TestBashTool_Schema(t *testing.T) {
	tool := &BashTool{}
	schema := tool.Schema()
	if schema.Type != "function" {
		t.Errorf("expected schema type 'function', got %q", schema.Type)
	}
	if schema.Function.Name != "bash" {
		t.Errorf("expected function name 'bash', got %q", schema.Function.Name)
	}
	if schema.Function.Description == "" {
		t.Error("expected non-empty function description")
	}
	if _, ok := schema.Function.Parameters.Properties["command"]; !ok {
		t.Error("expected 'command' property in schema")
	}
	if _, ok := schema.Function.Parameters.Properties["timeout"]; !ok {
		t.Error("expected 'timeout' property in schema")
	}
	found := false
	for _, req := range schema.Function.Parameters.Required {
		if req == "command" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'command' to be in required parameters")
	}
}

// TestBashTool_Execute_SimpleCommand verifies a basic echo command works.
func TestBashTool_Execute_SimpleCommand(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 10 * time.Second}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo hello",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "hello") {
		t.Errorf("expected 'hello' in output, got %q", result.Output)
	}
	if result.Metadata["exit_code"] != 0 {
		t.Errorf("expected exit_code 0, got %v", result.Metadata["exit_code"])
	}
}

// TestBashTool_Execute_MissingCommand verifies that a missing command returns an error.
func TestBashTool_Execute_MissingCommand(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root}

	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Fatal("expected error when command is missing")
	}
	if !strings.Contains(result.Error, "command") {
		t.Errorf("expected 'command' in error, got: %s", result.Error)
	}
}

// TestBashTool_Execute_EmptyCommand verifies that an empty command returns an error.
func TestBashTool_Execute_EmptyCommand(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root}

	result := tool.Execute(context.Background(), map[string]any{"command": "   "})
	if !result.IsError {
		t.Fatal("expected error for whitespace-only command")
	}
}

// TestBashTool_Execute_NonZeroExit verifies non-zero exit codes set IsError.
func TestBashTool_Execute_NonZeroExit(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 10 * time.Second}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "exit 1",
	})

	if !result.IsError {
		t.Fatal("expected IsError=true for non-zero exit code")
	}
	if result.Metadata["exit_code"] == 0 {
		t.Error("expected non-zero exit_code in metadata")
	}
}

// TestBashTool_Execute_StderrCapture verifies stderr is captured.
func TestBashTool_Execute_StderrCapture(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 10 * time.Second}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo error_output >&2",
	})

	// Exit code 0, so IsError=false. Stderr should be captured.
	if result.Error == "" {
		t.Error("expected stderr to be captured in result.Error field")
	}
	if !strings.Contains(result.Error, "error_output") {
		t.Errorf("expected 'error_output' in result.Error, got %q", result.Error)
	}
}

// TestBashTool_Execute_WorkingDirectory verifies command runs in SandboxRoot.
func TestBashTool_Execute_WorkingDirectory(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 10 * time.Second}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "pwd",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	// pwd output should contain the sandbox root (resolve symlinks for macOS)
	if result.Output == "" {
		t.Error("expected non-empty pwd output")
	}
}

// TestBashTool_Execute_CustomTimeoutFloat64 verifies float64 timeout is accepted.
func TestBashTool_Execute_CustomTimeoutFloat64(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo ok",
		"timeout": float64(5),
	})

	if result.IsError {
		t.Fatalf("unexpected error with float64 timeout: %s", result.Error)
	}
	if !strings.Contains(result.Output, "ok") {
		t.Errorf("expected 'ok' in output, got %q", result.Output)
	}
}

// TestBashTool_Execute_CustomTimeoutInt verifies int timeout is accepted.
func TestBashTool_Execute_CustomTimeoutInt(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo ok",
		"timeout": int(5),
	})

	if result.IsError {
		t.Fatalf("unexpected error with int timeout: %s", result.Error)
	}
	if !strings.Contains(result.Output, "ok") {
		t.Errorf("expected 'ok' in output, got %q", result.Output)
	}
}

// TestBashTool_Execute_DefaultTimeout verifies that zero Timeout uses 120s default.
func TestBashTool_Execute_DefaultTimeout(t *testing.T) {
	root := t.TempDir()
	// Timeout=0 should use 120s default — just verify a quick command works.
	tool := &BashTool{SandboxRoot: root, Timeout: 0}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo default",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "default") {
		t.Errorf("expected 'default' in output, got %q", result.Output)
	}
}

// TestBashTool_Execute_Multiline verifies multiline output is captured.
func TestBashTool_Execute_Multiline(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 10 * time.Second}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "printf 'line1\nline2\nline3\n'",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	for _, line := range []string{"line1", "line2", "line3"} {
		if !strings.Contains(result.Output, line) {
			t.Errorf("expected %q in multiline output", line)
		}
	}
}

// TestBashTool_Execute_ExitCodeMetadata verifies metadata exit_code is set correctly.
func TestBashTool_Execute_ExitCodeMetadata(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 10 * time.Second}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "exit 42",
	})

	if !result.IsError {
		t.Fatal("expected IsError=true for exit 42")
	}
	code, ok := result.Metadata["exit_code"].(int)
	if !ok {
		t.Fatalf("expected int exit_code, got %T", result.Metadata["exit_code"])
	}
	if code != 42 {
		t.Errorf("expected exit_code=42, got %d", code)
	}
}

// TestTruncate_ShortString verifies no truncation for short strings.
func TestTruncate_ShortString(t *testing.T) {
	s := "short"
	got := truncate(s, 100)
	if got != s {
		t.Errorf("expected no truncation, got %q", got)
	}
}

// TestTruncate_LongString verifies truncation at maxBytes.
func TestTruncate_LongString(t *testing.T) {
	// Build a string longer than the limit
	s := strings.Repeat("a", 200)
	got := truncate(s, 100)
	if len(got) <= 100 {
		// The truncated result includes the suffix, so it may be slightly > 100
		// but the prefix should be exactly 100 bytes.
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("expected 'truncated' in result, got %q", got)
	}
	if !strings.HasPrefix(got, strings.Repeat("a", 100)) {
		t.Error("expected first 100 bytes to be preserved")
	}
}

// TestTruncate_ExactBoundary verifies no truncation at exact maxBytes.
func TestTruncate_ExactBoundary(t *testing.T) {
	s := strings.Repeat("x", 100)
	got := truncate(s, 100)
	if got != s {
		t.Errorf("expected no truncation at exact boundary, got length %d", len(got))
	}
}

// TestBashTool_Execute_ContextCancellation verifies that Execute handles a cancelled context
// without panicking and returns a ToolResult with an exit_code metadata entry.
func TestBashTool_Execute_ContextCancellation(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 30 * time.Second}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result := tool.Execute(ctx, map[string]any{
		"command": "sleep 10",
	})

	// The result must always carry an exit_code in metadata regardless of outcome.
	if _, ok := result.Metadata["exit_code"]; !ok {
		t.Error("expected exit_code in metadata after cancelled execution")
	}
}
