package tools_test

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/agent/session"
	"github.com/scrypster/huginn/internal/tools"
)

func TestBashTool_InjectsSessionEnv(t *testing.T) {
	tool := &tools.BashTool{SandboxRoot: t.TempDir()}
	ctx := session.WithEnv(context.Background(), []string{"HUGINN_TEST_VAR=hello123"})

	result := tool.Execute(ctx, map[string]any{"command": "echo $HUGINN_TEST_VAR"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Output != "hello123\n" {
		t.Errorf("expected hello123, got %q", result.Output)
	}
}

func TestBashTool_BashENVIsUnset(t *testing.T) {
	t.Setenv("BASH_ENV", "/tmp/evil-rc") // prove override fires against live parent value
	tool := &tools.BashTool{SandboxRoot: t.TempDir()}
	// Even if BASH_ENV is set in parent process, it must be cleared
	ctx := session.WithEnv(context.Background(), []string{"BASH_ENV="})

	result := tool.Execute(ctx, map[string]any{"command": "echo ${BASH_ENV:-EMPTY}"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Output != "EMPTY\n" {
		t.Errorf("expected BASH_ENV to be empty, got %q", result.Output)
	}
}

func TestBashTool_NoSessionEnv_StillWorks(t *testing.T) {
	tool := &tools.BashTool{SandboxRoot: t.TempDir()}
	result := tool.Execute(context.Background(), map[string]any{"command": "echo ok"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Output != "ok\n" {
		t.Errorf("expected ok, got %q", result.Output)
	}
}
