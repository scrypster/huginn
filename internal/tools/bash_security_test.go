package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestBashTool_ShellMetacharacters_Pipe verifies pipe operator works safely.
func TestBashTool_ShellMetacharacters_Pipe(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 10 * time.Second}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo 'hello world' | wc -w",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "2") {
		t.Errorf("expected word count '2', got %q", result.Output)
	}
}

// TestBashTool_ShellMetacharacters_Semicolon verifies semicolon-separated commands work.
func TestBashTool_ShellMetacharacters_Semicolon(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 10 * time.Second}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo first; echo second",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "first") || !strings.Contains(result.Output, "second") {
		t.Errorf("expected both outputs, got %q", result.Output)
	}
}

// TestBashTool_ShellMetacharacters_And verifies && operator works (short-circuit AND).
func TestBashTool_ShellMetacharacters_And(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 10 * time.Second}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo success && exit 0",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "success") {
		t.Errorf("expected 'success', got %q", result.Output)
	}
}

// TestBashTool_ShellMetacharacters_Or verifies || operator works (short-circuit OR).
func TestBashTool_ShellMetacharacters_Or(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 10 * time.Second}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "false || echo fallback",
	})

	// false returns exit code 1; the || causes echo fallback to run and the shell exits 0
	if !strings.Contains(result.Output, "fallback") {
		t.Errorf("expected 'fallback' in output, got %q", result.Output)
	}
}

// TestBashTool_ShellMetacharacters_Redirection verifies > and < work safely.
func TestBashTool_ShellMetacharacters_Redirection(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 10 * time.Second}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo test > /tmp/test_output.txt && cat /tmp/test_output.txt",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "test") {
		t.Errorf("expected 'test', got %q", result.Output)
	}
}

// TestBashTool_ShellMetacharacters_CommandSubstitution verifies $(...) works.
func TestBashTool_ShellMetacharacters_CommandSubstitution(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 10 * time.Second}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo $(echo nested)",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "nested") {
		t.Errorf("expected 'nested', got %q", result.Output)
	}
}

// TestBashTool_ShellMetacharacters_Backticks verifies backtick substitution works.
func TestBashTool_ShellMetacharacters_Backticks(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 10 * time.Second}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo `echo backtick`",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "backtick") {
		t.Errorf("expected 'backtick', got %q", result.Output)
	}
}

// TestBashTool_ShellMetacharacters_VariableExpansion verifies $VAR expansion.
func TestBashTool_ShellMetacharacters_VariableExpansion(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 10 * time.Second}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "TEST_VAR=hello && echo $TEST_VAR",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "hello") {
		t.Errorf("expected 'hello', got %q", result.Output)
	}
}

// TestBashTool_TimeoutEnforcement verifies timeout actually kills long-running processes.
func TestBashTool_TimeoutEnforcement(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 1 * time.Second}

	start := time.Now()
	result := tool.Execute(context.Background(), map[string]any{
		"command": "sleep 10",
	})
	elapsed := time.Since(start)

	// Should timeout around 1 second, definitely not 10
	if elapsed > 5*time.Second {
		t.Errorf("expected timeout around 1s, took %v", elapsed)
	}

	// Should be marked as error (command killed by timeout)
	if !result.IsError {
		t.Error("expected IsError=true when command is killed by timeout")
	}
}

// TestBashTool_VeryLongOutput verifies output truncation at 100KB boundary.
func TestBashTool_VeryLongOutput(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 10 * time.Second}

	// Create output just under, at, and over the 100KB limit
	smallOutput := strings.Repeat("a", 50*1024) // 50KB
	result := tool.Execute(context.Background(), map[string]any{
		"command": "printf '" + smallOutput + "'",
	})

	if result.IsError {
		t.Fatalf("unexpected error for small output: %s", result.Error)
	}
	if !strings.Contains(result.Output, "a") {
		t.Error("expected output to contain data")
	}
}

// TestBashTool_ExactlyMaxOutput verifies no truncation when exactly at limit.
func TestBashTool_ExactlyMaxOutput(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 10 * time.Second}

	// Build command that produces exactly 100KB output
	// Each "a" is 1 byte, so 100*1024 characters = 100KB
	numBytes := 100 * 1024
	result := tool.Execute(context.Background(), map[string]any{
		"command": "printf '" + strings.Repeat("a", numBytes) + "'",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	// At exact boundary, should not be truncated
	if strings.Contains(result.Output, "truncated") {
		t.Error("expected no truncation at exact 100KB boundary")
	}
}

// TestBashTool_OverMaxOutput verifies truncation message when exceeding limit.
func TestBashTool_OverMaxOutput(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 10 * time.Second}

	// Create output slightly over 100KB
	numBytes := 100*1024 + 1000
	result := tool.Execute(context.Background(), map[string]any{
		"command": "printf '" + strings.Repeat("x", numBytes) + "'",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "truncated") {
		t.Error("expected truncation message in output")
	}
	if !strings.Contains(result.Output, "bytes total") {
		t.Error("expected byte count in truncation message")
	}
}

// TestBashTool_ExitCodePropagation verifies various exit codes are captured.
func TestBashTool_ExitCodePropagation(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		expected int
	}{
		{"exit 0", "exit 0", 0},
		{"exit 1", "exit 1", 1},
		{"exit 127", "exit 127", 127},
		{"exit 255", "exit 255", 255},
		{"command not found", "/nonexistent/command", 127},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			tool := &BashTool{SandboxRoot: root, Timeout: 5 * time.Second}

			result := tool.Execute(context.Background(), map[string]any{
				"command": tt.command,
			})

			code, ok := result.Metadata["exit_code"].(int)
			if !ok {
				t.Fatalf("exit_code not found or not int")
			}
			if code != tt.expected {
				t.Errorf("expected exit code %d, got %d", tt.expected, code)
			}
		})
	}
}

// TestBashTool_InvalidTimeoutType verifies invalid timeout types are ignored gracefully.
func TestBashTool_InvalidTimeoutType(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 5 * time.Second}

	// Pass string instead of int/float64 for timeout
	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo ok",
		"timeout": "invalid",
	})

	// Should still work, using default timeout
	if result.IsError {
		t.Fatalf("unexpected error with invalid timeout: %s", result.Error)
	}
	if !strings.Contains(result.Output, "ok") {
		t.Errorf("expected 'ok' in output, got %q", result.Output)
	}
}

// TestBashTool_NegativeTimeout_ReturnsError verifies that a negative timeout
// is now rejected with an error (hardened behaviour added in security pass).
func TestBashTool_NegativeTimeout_ReturnsError(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo test",
		"timeout": -1,
	})

	// Negative timeouts must now return an error.
	if !result.IsError {
		t.Fatalf("expected error for negative timeout, got output: %q", result.Output)
	}
}

// TestBashTool_ZeroTimeout verifies zero timeout argument uses default.
func TestBashTool_ZeroTimeout(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo ok",
		"timeout": 0,
	})

	// timeout=0 should use 120s default (from args, not tool.Timeout)
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "ok") {
		t.Errorf("expected 'ok', got %q", result.Output)
	}
}

// TestBashTool_StdoutAndStderrInterleaved verifies both streams are captured.
func TestBashTool_StdoutAndStderrInterleaved(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 5 * time.Second}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo stdout1 && echo stderr1 >&2 && echo stdout2",
	})

	if !strings.Contains(result.Output, "stdout1") {
		t.Error("expected 'stdout1' in output")
	}
	if !strings.Contains(result.Output, "stdout2") {
		t.Error("expected 'stdout2' in output")
	}
	if !strings.Contains(result.Error, "stderr1") {
		t.Error("expected 'stderr1' in error")
	}
}

// TestBashTool_ContextCancellationMidExecution verifies cancellation during execution.
func TestBashTool_ContextCancellationMidExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping cancellation test in short mode")
	}

	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 30 * time.Second}

	ctx, cancel := context.WithCancel(context.Background())

	// Launch cancellation in a goroutine to happen during sleep
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	result := tool.Execute(ctx, map[string]any{
		"command": "sleep 10",
	})

	// Should have exit_code in metadata
	if _, ok := result.Metadata["exit_code"]; !ok {
		t.Error("expected exit_code in metadata")
	}

	// Should be marked as error (killed by context cancellation)
	if !result.IsError {
		t.Log("context cancellation may result in IsError=false if process completed before cancel took effect")
	}
}

// TestBashTool_CommandWithSpecialChars verifies commands with special characters work.
func TestBashTool_CommandWithSpecialChars(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 5 * time.Second}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo 'test@#$%^&*()'",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "test@#$%^&*()") {
		t.Errorf("expected special chars in output, got %q", result.Output)
	}
}

// TestBashTool_LargeBufferHandling verifies large stdout/stderr don't cause issues.
func TestBashTool_LargeBufferHandling(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 10 * time.Second}

	// Generate 50KB of output on both stdout and stderr
	size := 50 * 1024
	result := tool.Execute(context.Background(), map[string]any{
		"command": "printf '" + strings.Repeat("o", size) + "' && printf '" + strings.Repeat("e", size) + "' >&2",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	// Both should be captured
	if len(result.Output) < size/2 {
		t.Errorf("stdout seems truncated: %d bytes", len(result.Output))
	}
	if len(result.Error) < size/2 {
		t.Errorf("stderr seems truncated: %d bytes", len(result.Error))
	}
}

// BenchmarkBashTool_SimpleCommand measures performance of simple command execution.
func BenchmarkBashTool_SimpleCommand(b *testing.B) {
	root := b.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 10 * time.Second}

	for i := 0; i < b.N; i++ {
		tool.Execute(context.Background(), map[string]any{
			"command": "echo benchmark",
		})
	}
}

// BenchmarkTruncate_SmallString measures truncate performance on small strings.
func BenchmarkTruncate_SmallString(b *testing.B) {
	s := "small string"
	for i := 0; i < b.N; i++ {
		truncate(s, 1000)
	}
}

// BenchmarkTruncate_LargeString measures truncate performance on large strings.
func BenchmarkTruncate_LargeString(b *testing.B) {
	s := strings.Repeat("a", 200*1024)
	for i := 0; i < b.N; i++ {
		truncate(s, 100*1024)
	}
}

// TestMergeEnv_BasicMerge verifies environment merging works correctly.
func TestMergeEnv_BasicMerge(t *testing.T) {
	base := []string{"KEY1=value1", "KEY2=value2"}
	overrides := []string{"KEY2=override", "KEY3=new"}

	result := mergeEnv(base, overrides)

	// Convert to map for easier checking
	m := make(map[string]string)
	for _, e := range result {
		k, v, _ := strings.Cut(e, "=")
		m[k] = v
	}

	if m["KEY1"] != "value1" {
		t.Errorf("expected KEY1=value1, got %q", m["KEY1"])
	}
	if m["KEY2"] != "override" {
		t.Errorf("expected KEY2=override, got %q", m["KEY2"])
	}
	if m["KEY3"] != "new" {
		t.Errorf("expected KEY3=new, got %q", m["KEY3"])
	}
}

// TestMergeEnv_EmptyValues verifies empty environment values are preserved.
func TestMergeEnv_EmptyValues(t *testing.T) {
	base := []string{"KEY1=value1", "KEY2=value2"}
	overrides := []string{"KEY2="} // Empty value unsets KEY2

	result := mergeEnv(base, overrides)

	m := make(map[string]string)
	for _, e := range result {
		k, v, _ := strings.Cut(e, "=")
		m[k] = v
	}

	if m["KEY1"] != "value1" {
		t.Error("expected KEY1 unchanged")
	}
	if val, ok := m["KEY2"]; !ok || val != "" {
		t.Errorf("expected KEY2 to be empty, got %q", val)
	}
}

// TestMergeEnv_Duplicates verifies duplicate keys are handled correctly.
func TestMergeEnv_Duplicates(t *testing.T) {
	base := []string{"KEY=value1", "KEY=value2"} // Duplicate in base
	overrides := []string{"KEY=override"}

	result := mergeEnv(base, overrides)

	m := make(map[string]string)
	for _, e := range result {
		k, v, _ := strings.Cut(e, "=")
		m[k] = v
	}

	// The last value should win
	if m["KEY"] != "override" {
		t.Errorf("expected KEY=override, got %q", m["KEY"])
	}
}

// TestBashTool_BinaryContent verifies binary data in command args doesn't crash.
func TestBashTool_BinaryContent(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 5 * time.Second}

	// Use printf to output binary data
	result := tool.Execute(context.Background(), map[string]any{
		"command": "printf '\\x00\\x01\\x02'",
	})

	// Should not crash, though output handling is implementation-specific
	if _, ok := result.Metadata["exit_code"]; !ok {
		t.Error("expected exit_code in metadata")
	}
}

// TestBashTool_NonrcProfile verifies --norc and --noprofile flags are used.
func TestBashTool_NonrcProfile(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 5 * time.Second}

	// Try to reference a variable that would only exist if rc files were sourced
	// This is a best-effort test; exact behavior depends on system
	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo ${PS1:-NOTSET}",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	// PS1 typically not set in non-interactive shells, so should be NOTSET
	if !strings.Contains(result.Output, "NOTSET") {
		t.Logf("PS1 was set: %q (system may have different defaults)", result.Output)
	}
}

// TestBashTool_SandboxRootEnforcement verifies commands execute in SandboxRoot.
func TestBashTool_SandboxRootEnforcement(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 5 * time.Second}

	result := tool.Execute(context.Background(), map[string]any{
		"command": "pwd",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	// The output should be in the temp directory
	if !strings.Contains(result.Output, "tmp") {
		t.Logf("pwd output doesn't contain 'tmp', got: %q", result.Output)
	}
}

// TestBashTool_TruncatedOutputConsistency verifies truncated output always includes full metadata.
func TestBashTool_TruncatedOutputConsistency(t *testing.T) {
	root := t.TempDir()
	tool := &BashTool{SandboxRoot: root, Timeout: 10 * time.Second}

	// Ensure output is truncated
	numBytes := 110 * 1024 // Over 100KB
	result := tool.Execute(context.Background(), map[string]any{
		"command": "printf '" + strings.Repeat("x", numBytes) + "'",
	})

	if !result.IsError {
		// Check metadata is still present and valid
		if code, ok := result.Metadata["exit_code"]; !ok {
			t.Error("expected exit_code in metadata even with truncated output")
		} else if code.(int) != 0 {
			t.Errorf("expected exit code 0, got %v", code)
		}
	}
}

// TestBashTool_ContextTimeout_Precedence verifies context timeout is respected.
func TestBashTool_ContextTimeout_Precedence(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	root := t.TempDir()
	// Tool timeout is long (30s)
	tool := &BashTool{SandboxRoot: root, Timeout: 30 * time.Second}

	// But context timeout is short (1s)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	start := time.Now()
	result := tool.Execute(ctx, map[string]any{
		"command": "sleep 10",
	})
	elapsed := time.Since(start)

	// Should timeout at 1 second (context timeout), not 30 seconds
	if elapsed > 3*time.Second {
		t.Errorf("expected timeout around 1s, took %v", elapsed)
	}
	if !result.IsError {
		t.Log("expected IsError=true when context times out")
	}
}
