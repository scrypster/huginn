package agent

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/agent/session"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/tools"
)

// captureEnvTool records the session env from context when Execute is called.
type captureEnvTool struct {
	mu          sync.Mutex
	capturedEnv []string
	callCount   int
}

func (t *captureEnvTool) Name() string                      { return "capture_env" }
func (t *captureEnvTool) Description() string               { return "captures session env from context" }
func (t *captureEnvTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *captureEnvTool) Schema() backend.Tool {
	return backend.Tool{Function: backend.ToolFunction{Name: t.Name()}}
}
func (t *captureEnvTool) Execute(ctx context.Context, _ map[string]any) tools.ToolResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.capturedEnv = session.EnvFrom(ctx)
	t.callCount++
	return tools.ToolResult{Output: "env captured"}
}

// envMap converts []string "KEY=VALUE" pairs into a map for easy assertions.
func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	return m
}

// TestCodeWithAgent_SessionEnvReachesTool verifies session env flows from
// CodeWithAgent context to tool.Execute.
func TestCodeWithAgent_SessionEnvReachesTool(t *testing.T) {
	capTool := &captureEnvTool{}
	reg := newRegistryWith(capTool)

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{
				DoneReason: "tool_calls",
				ToolCalls: []backend.ToolCall{
					{
						ID: "call_capture",
						Function: backend.ToolCallFunction{
							Name:      "capture_env",
							Arguments: map[string]any{},
						},
					},
				},
			},
			{Content: "done", DoneReason: "stop"},
		},
	}

	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	gate := permissions.NewGate(true, nil)
	o.SetTools(reg, gate)

	ag := &agents.Agent{
		Name:    "test-agent",
		ModelID: "test-model",
	}

	err := o.CodeWithAgent(
		context.Background(),
		ag,
		"test message",
		5,
		nil, nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("CodeWithAgent: %v", err)
	}

	if capTool.callCount == 0 {
		t.Fatal("captureEnvTool was never called")
	}
	if capTool.capturedEnv == nil {
		t.Fatal("session env was nil in captureEnvTool.Execute")
	}

	em := envMap(capTool.capturedEnv)

	home, ok := em["HOME"]
	if !ok {
		t.Error("session env missing HOME")
	} else if !strings.Contains(home, "huginn-session") {
		t.Errorf("HOME = %q, expected path containing 'huginn-session'", home)
	}

	path, ok := em["PATH"]
	if !ok {
		t.Error("session env missing PATH")
	} else if home != "" && !strings.HasPrefix(path, home+"/bin:") {
		t.Errorf("PATH = %q, expected prefix %q", path, home+"/bin:")
	}

	bashEnv, ok := em["BASH_ENV"]
	if !ok {
		t.Error("session env missing BASH_ENV")
	} else if bashEnv != "" {
		t.Errorf("BASH_ENV = %q, want empty string", bashEnv)
	}
}

// TestCodeWithAgent_SessionTempDirCleanedUp verifies Teardown removes the
// session temp dir after CodeWithAgent completes.
func TestCodeWithAgent_SessionTempDirCleanedUp(t *testing.T) {
	isolated := t.TempDir()
	t.Setenv("TMPDIR", isolated)

	mb := newMockBackend("all done")
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	gate := permissions.NewGate(true, nil)
	o.SetTools(tools.NewRegistry(), gate)

	ag := &agents.Agent{
		Name:    "cleanup-test-agent",
		ModelID: "test-model",
	}

	err := o.CodeWithAgent(
		context.Background(),
		ag,
		"hello",
		2,
		nil, nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("CodeWithAgent: %v", err)
	}

	entries, readErr := os.ReadDir(isolated)
	if readErr != nil {
		t.Fatalf("ReadDir: %v", readErr)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "huginn-session-") {
			t.Errorf("session temp dir not cleaned up: %s", e.Name())
		}
	}
}
