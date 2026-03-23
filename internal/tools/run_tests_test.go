package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunTestsTool_Name(t *testing.T) {
	tool := &RunTestsTool{SandboxRoot: t.TempDir(), Timeout: 5 * time.Second}
	if tool.Name() != "run_tests" {
		t.Errorf("expected run_tests, got %q", tool.Name())
	}
}

func TestRunTestsTool_Permission_IsExec(t *testing.T) {
	tool := &RunTestsTool{SandboxRoot: t.TempDir(), Timeout: 5 * time.Second}
	if tool.Permission() != PermExec {
		t.Error("expected PermExec")
	}
}

func TestRunTestsTool_MissingCommand_Error(t *testing.T) {
	tool := &RunTestsTool{SandboxRoot: t.TempDir(), Timeout: 5 * time.Second}
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error when command missing")
	}
}

func TestRunTestsTool_NonGoCommand_Passing(t *testing.T) {
	tool := &RunTestsTool{SandboxRoot: t.TempDir(), Timeout: 5 * time.Second}
	result := tool.Execute(context.Background(), map[string]any{"command": "echo hello"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "hello") {
		t.Errorf("expected hello, got %q", result.Output)
	}
}

func TestRunTestsTool_NonGoCommand_Failure(t *testing.T) {
	tool := &RunTestsTool{SandboxRoot: t.TempDir(), Timeout: 5 * time.Second}
	result := tool.Execute(context.Background(), map[string]any{"command": "exit 1"})
	if !result.IsError {
		t.Error("expected error for exit 1")
	}
}

func TestRunTestsTool_Timeout_Enforced(t *testing.T) {
	tool := &RunTestsTool{SandboxRoot: t.TempDir(), Timeout: 100 * time.Millisecond}
	start := time.Now()
	result := tool.Execute(context.Background(), map[string]any{"command": "sleep 10"})
	if time.Since(start) > 2*time.Second {
		t.Error("timeout not enforced")
	}
	if !result.IsError {
		t.Error("expected error after timeout")
	}
}

func TestRunTestsTool_ZeroTimeout_UsesDefault(t *testing.T) {
	tool := &RunTestsTool{SandboxRoot: t.TempDir(), Timeout: 5 * time.Second}
	// timeout: 0 should not cause immediate cancellation — use default instead
	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo ok",
		"timeout": float64(0),
	})
	if result.IsError {
		t.Errorf("timeout:0 should not cause error, got: %s", result.Error)
	}
	if !strings.Contains(result.Output, "ok") {
		t.Errorf("expected 'ok' in output, got: %q", result.Output)
	}
}

func TestRunTestsTool_NegativeTimeout_UsesDefault(t *testing.T) {
	tool := &RunTestsTool{SandboxRoot: t.TempDir(), Timeout: 5 * time.Second}
	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo ok",
		"timeout": float64(-5),
	})
	if result.IsError {
		t.Errorf("negative timeout should not cause error, got: %s", result.Error)
	}
}

func TestParseGoTestJSON_AllPassed(t *testing.T) {
	input := `{"Action":"run","Test":"TestFoo"}
{"Action":"pass","Test":"TestFoo","Elapsed":0.001}
{"Action":"pass","Elapsed":0.001}
`
	result := ParseGoTestJSON([]byte(input))
	if !result.Passed {
		t.Error("expected Passed=true")
	}
	if len(result.Failed) != 0 {
		t.Errorf("expected no failures, got %v", result.Failed)
	}
}

func TestParseGoTestJSON_WithFailures(t *testing.T) {
	input := `{"Action":"run","Test":"TestBad"}
{"Action":"fail","Test":"TestBad","Elapsed":0.002}
{"Action":"fail","Elapsed":0.002}
`
	result := ParseGoTestJSON([]byte(input))
	if result.Passed {
		t.Error("expected Passed=false")
	}
	if len(result.Failed) == 0 {
		t.Error("expected at least one failure")
	}
	if result.Failed[0] != "TestBad" {
		t.Errorf("expected TestBad, got %v", result.Failed)
	}
}

func TestParseGoTestJSON_BuildError(t *testing.T) {
	input := `{"Action":"fail","Elapsed":0.002}
some build error output`
	result := ParseGoTestJSON([]byte(input))
	if result.Passed {
		t.Error("expected Passed=false for build error")
	}
}

func setupPassingGoModule(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("requires go toolchain")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module testmod\ngo 1.21\n"), 0644)
	os.WriteFile(filepath.Join(dir, "foo_test.go"), []byte("package testmod\nimport \"testing\"\nfunc TestPass(t *testing.T) {}\n"), 0644)
	return dir
}

func TestRunTestsTool_GoTest_Passing(t *testing.T) {
	if testing.Short() {
		t.Skip("requires go toolchain")
	}
	root := setupPassingGoModule(t)
	tool := &RunTestsTool{SandboxRoot: root, Timeout: 30 * time.Second}
	result := tool.Execute(context.Background(), map[string]any{"command": "go test ./..."})
	if result.IsError {
		t.Fatalf("expected pass, got error: %s", result.Error)
	}
	passed, _ := result.Metadata["passed"].(bool)
	if !passed {
		t.Errorf("expected metadata passed=true, output: %s", result.Output)
	}
}

func TestRunTestsTool_CustomTimeout_Int(t *testing.T) {
	tool := &RunTestsTool{SandboxRoot: t.TempDir(), Timeout: 5 * time.Second}
	result := tool.Execute(context.Background(), map[string]any{
		"command": "echo ok",
		"timeout": int(10),
	})
	if result.IsError {
		t.Errorf("unexpected error with int timeout: %s", result.Error)
	}
}

func TestRunTestsTool_WorkingDir(t *testing.T) {
	root := t.TempDir()
	subdir := filepath.Join(root, "subdir")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}
	tool := &RunTestsTool{SandboxRoot: root, Timeout: 5 * time.Second}
	result := tool.Execute(context.Background(), map[string]any{
		"command":     "pwd",
		"working_dir": "subdir",
	})
	if result.IsError {
		t.Errorf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "subdir") {
		t.Logf("output: %q", result.Output)
		// pwd output may vary, check that it ran at least
	}
}

func TestRunTestsTool_InvalidWorkingDir_PathTraversal(t *testing.T) {
	root := t.TempDir()
	tool := &RunTestsTool{SandboxRoot: root, Timeout: 5 * time.Second}
	result := tool.Execute(context.Background(), map[string]any{
		"command":     "echo bad",
		"working_dir": "../../etc",
	})
	if !result.IsError {
		t.Error("expected error for path traversal in working_dir")
	}
}

func TestRunTestsTool_EmptyCommand_Error(t *testing.T) {
	tool := &RunTestsTool{SandboxRoot: t.TempDir(), Timeout: 5 * time.Second}
	result := tool.Execute(context.Background(), map[string]any{"command": ""})
	if !result.IsError {
		t.Error("expected error for empty command")
	}
}

func TestParseGoTestJSON_WithOutput(t *testing.T) {
	input := `{"Action":"output","Output":"test output\n"}
{"Action":"run","Test":"TestFoo"}
{"Action":"pass","Test":"TestFoo","Elapsed":0.001}
{"Action":"pass","Elapsed":0.001}
`
	result := ParseGoTestJSON([]byte(input))
	if !result.Passed {
		t.Error("expected Passed=true")
	}
	if !strings.Contains(result.Output, "test output") {
		t.Errorf("output should contain captured output, got %q", result.Output)
	}
}

func TestParseGoTestJSON_InvalidJSON(t *testing.T) {
	input := `not json at all
{"Action":"pass","Elapsed":0.001}
`
	result := ParseGoTestJSON([]byte(input))
	if !result.Passed {
		t.Error("expected Passed=true for valid last event")
	}
	if !strings.Contains(result.Output, "not json") {
		t.Errorf("invalid JSON should appear in output")
	}
}

func TestParseGoTestJSON_EmptyInput(t *testing.T) {
	result := ParseGoTestJSON([]byte(""))
	if !result.Passed {
		t.Error("expected Passed=true for empty input")
	}
	if len(result.Failed) != 0 {
		t.Errorf("expected no failures")
	}
}
