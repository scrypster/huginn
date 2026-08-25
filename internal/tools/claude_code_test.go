package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/claudecode"
)

func enabledCfg(t *testing.T) claudecode.Config {
	t.Helper()
	cfg := claudecode.DefaultConfig()
	cfg.Enabled = true
	p, err := filepath.Abs(filepath.Join("..", "claudecode", "testdata", "fake-claude.sh"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	cfg.Binary = p
	return cfg
}

func TestClaudeCodeToolMetadata(t *testing.T) {
	tool := NewClaudeCodeTool(enabledCfg(t), "/tmp", nil)
	if tool.Name() != "claude_code" {
		t.Errorf("Name() = %q, want claude_code", tool.Name())
	}
	if tool.Permission() != PermExec {
		t.Errorf("Permission() = %v, want PermExec — delegation runs arbitrary tools", tool.Permission())
	}
	if tool.Description() == "" {
		t.Error("Description() is empty")
	}
	if tool.Schema().Function.Name != "claude_code" {
		t.Errorf("Schema function name = %q", tool.Schema().Function.Name)
	}
}

func TestClaudeCodeToolExecute(t *testing.T) {
	tool := NewClaudeCodeTool(enabledCfg(t), t.TempDir(), nil)
	res := tool.Execute(context.Background(), map[string]any{
		"prompt": "rename the function",
	})
	if res.IsError {
		t.Fatalf("Execute errored: %s", res.Error)
	}
	if !strings.Contains(res.Output, "Done: renamed the function.") {
		t.Errorf("Output = %q", res.Output)
	}
	if res.Metadata["cost_usd"] != 0.0123 {
		t.Errorf("cost_usd = %v, want 0.0123", res.Metadata["cost_usd"])
	}
	if res.Metadata["num_turns"] != 3 {
		t.Errorf("num_turns = %v, want 3", res.Metadata["num_turns"])
	}
	if res.Metadata["claude_session_id"] == "" {
		t.Error("claude_session_id missing from metadata")
	}
}

func errorCfg(t *testing.T) claudecode.Config {
	t.Helper()
	cfg := claudecode.DefaultConfig()
	cfg.Enabled = true
	p, err := filepath.Abs(filepath.Join("..", "claudecode", "testdata", "fake-claude-is-error.sh"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	cfg.Binary = p
	return cfg
}

func TestClaudeCodeToolAllowedPermissionModePassesThrough(t *testing.T) {
	tool := NewClaudeCodeTool(enabledCfg(t), t.TempDir(), nil)
	res := tool.Execute(context.Background(), map[string]any{
		"prompt":          "rename the function",
		"permission_mode": "plan",
	})
	if res.IsError {
		t.Fatalf("Execute errored on an allowed permission_mode: %s", res.Error)
	}
	if !strings.Contains(res.Output, "Done: renamed the function.") {
		t.Errorf("Output = %q", res.Output)
	}
}

func TestClaudeCodeToolRejectsBypassPermissions(t *testing.T) {
	tool := NewClaudeCodeTool(enabledCfg(t), t.TempDir(), nil)
	res := tool.Execute(context.Background(), map[string]any{
		"prompt":          "rename the function",
		"permission_mode": "bypassPermissions",
	})
	if !res.IsError {
		t.Fatal("Execute with permission_mode=bypassPermissions should be an error")
	}
	if !strings.Contains(res.Error, "bypassPermissions") {
		t.Errorf("Error = %q, want it to name the offending value", res.Error)
	}
}

func TestClaudeCodeToolRejectsNonsensePermissionMode(t *testing.T) {
	tool := NewClaudeCodeTool(enabledCfg(t), t.TempDir(), nil)
	res := tool.Execute(context.Background(), map[string]any{
		"prompt":          "rename the function",
		"permission_mode": "yolo-mode",
	})
	if !res.IsError {
		t.Fatal("Execute with a nonsense permission_mode should be an error")
	}
	if !strings.Contains(res.Error, "yolo-mode") {
		t.Errorf("Error = %q, want it to name the offending value", res.Error)
	}
}

func TestClaudeCodeToolMapsDelegateIsError(t *testing.T) {
	tool := NewClaudeCodeTool(errorCfg(t), t.TempDir(), nil)
	res := tool.Execute(context.Background(), map[string]any{
		"prompt": "do the impossible",
	})
	if !res.IsError {
		t.Fatal("Execute should surface IsError from a stream reporting is_error:true, even on exit 0")
	}
	if res.Error == "" {
		t.Error("Error should be populated from DelegateResult.ErrorText")
	}
}

func TestClaudeCodeToolRequiresPrompt(t *testing.T) {
	tool := NewClaudeCodeTool(enabledCfg(t), "/tmp", nil)
	res := tool.Execute(context.Background(), map[string]any{})
	if !res.IsError {
		t.Error("Execute with no prompt should be an error")
	}
}

func TestRegisterClaudeCodeToolRespectsConfig(t *testing.T) {
	reg := NewRegistry()
	off := claudecode.DefaultConfig() // Enabled=false
	RegisterClaudeCodeTool(reg, off, "/tmp", nil)
	if _, ok := reg.Get("claude_code"); ok {
		t.Error("claude_code registered while the bridge is disabled")
	}

	RegisterClaudeCodeTool(reg, enabledCfg(t), "/tmp", nil)
	if _, ok := reg.Get("claude_code"); !ok {
		t.Error("claude_code not registered while the bridge is enabled")
	}
}
