package threadmgr

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
)

// runtimeRecordingBackend captures the Tools and Messages from the first
// ChatCompletion request, then issues a tool call for the named agent tool,
// and finally a finish call to terminate the thread.
type runtimeRecordingBackend struct {
	toolToCall    string
	calls         int
	gotTools      []backend.Tool
	gotSystem     string
	gotAllSystems []string
}

func (b *runtimeRecordingBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	b.calls++
	if b.calls == 1 {
		b.gotTools = append(b.gotTools, req.Tools...)
		for _, m := range req.Messages {
			if m.Role == "system" {
				b.gotAllSystems = append(b.gotAllSystems, m.Content)
				if b.gotSystem == "" {
					b.gotSystem = m.Content
				}
			}
		}
		return &backend.ChatResponse{
			DoneReason: "tool_use",
			ToolCalls: []backend.ToolCall{{
				ID: "rt-1",
				Function: backend.ToolCallFunction{
					Name:      b.toolToCall,
					Arguments: map[string]any{"q": "ping"},
				},
			}},
		}, nil
	}
	return &backend.ChatResponse{
		DoneReason: "tool_use",
		ToolCalls: []backend.ToolCall{{
			ID: "rt-fin",
			Function: backend.ToolCallFunction{
				Name:      "finish",
				Arguments: map[string]any{"summary": "done", "status": "success"},
			},
		}},
	}, nil
}
func (b *runtimeRecordingBackend) Health(_ context.Context) error   { return nil }
func (b *runtimeRecordingBackend) Shutdown(_ context.Context) error { return nil }
func (b *runtimeRecordingBackend) ContextWindow() int               { return 8192 }

// TestSpawnThread_AgentRuntimePreparer_AddsSchemasAndExecutor verifies that
// when a per-agent runtime preparer is wired, its schemas are exposed to the
// LLM, its executor handles tool calls, its ExtraSystem text is appended to
// the system prompt, and its Cleanup is invoked when the thread ends.
//
// Regression: spawned worker threads were missing per-agent context (vault
// MuninnDB schemas, toolbelt, memory_mode prompt). They received only the
// global toolbelt with no muninn_* tools, so delegated agents never wrote
// to or read from their memory vault.
func TestSpawnThread_AgentRuntimePreparer_AddsSchemasAndExecutor(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("rt-sess", "/tmp", "claude-haiku-4")

	muninnSchema := backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{Name: "muninn_recall"},
	}
	preparerCalls := int32(0)
	executorCalls := int32(0)
	cleanupCalls := int32(0)
	var preparedAgent string
	var executedTool string

	tm.SetAgentRuntimePreparer(func(_ context.Context, agentName string) (*AgentRuntime, error) {
		atomic.AddInt32(&preparerCalls, 1)
		preparedAgent = agentName
		return &AgentRuntime{
			Schemas: []backend.Tool{muninnSchema},
			ExecuteTool: func(_ context.Context, name string, _ map[string]any) (string, error) {
				atomic.AddInt32(&executorCalls, 1)
				executedTool = name
				return `{"hits":1,"summary":"recalled"}`, nil
			},
			ExtraSystem: "\n\n## Memory Mode\nImmersive recall enabled for vault `worker-vault`.",
			Cleanup:     func() { atomic.AddInt32(&cleanupCalls, 1) },
		}, nil
	})

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:    "Worker",
		ModelID: "claude-haiku-4",
	})

	rb := &runtimeRecordingBackend{toolToCall: "muninn_recall"}

	thread, _ := tm.Create(CreateParams{
		SessionID: sess.ID,
		AgentID:   "Worker",
		Task:      "remember what you can about the user",
	})

	done := make(chan struct{})
	broadcast := func(_, msgType string, _ map[string]any) {
		if msgType == "thread_done" {
			close(done)
		}
	}

	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, rb, broadcast, NewCostAccumulator(0), nil)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for thread to complete")
	}

	if atomic.LoadInt32(&preparerCalls) != 1 {
		t.Fatalf("expected runtime preparer to be called exactly once, got %d", atomic.LoadInt32(&preparerCalls))
	}
	if preparedAgent != "Worker" {
		t.Errorf("expected preparer called with Worker, got %q", preparedAgent)
	}

	// Per-agent schemas (muninn_recall) must be present in the request.
	foundMuninn := false
	for _, tool := range rb.gotTools {
		if tool.Function.Name == "muninn_recall" {
			foundMuninn = true
			break
		}
	}
	if !foundMuninn {
		names := make([]string, 0, len(rb.gotTools))
		for _, tool := range rb.gotTools {
			names = append(names, tool.Function.Name)
		}
		t.Fatalf("expected muninn_recall in LLM tools (per-agent runtime), got %v", names)
	}

	// finish + request_help still always present.
	foundFinish := false
	for _, tool := range rb.gotTools {
		if tool.Function.Name == "finish" {
			foundFinish = true
			break
		}
	}
	if !foundFinish {
		t.Errorf("expected finish tool to remain in LLM tools")
	}

	// Executor wired by the preparer must have been called for muninn_recall.
	if atomic.LoadInt32(&executorCalls) != 1 {
		t.Errorf("expected runtime ExecuteTool to be called once, got %d", atomic.LoadInt32(&executorCalls))
	}
	if executedTool != "muninn_recall" {
		t.Errorf("expected runtime executor to dispatch muninn_recall, got %q", executedTool)
	}

	// ExtraSystem prompt addendum must appear in the system message.
	if !strings.Contains(rb.gotSystem, "Immersive recall enabled") {
		t.Errorf("expected ExtraSystem text in system prompt, got %q", rb.gotSystem)
	}

	// Cleanup must run when the thread ends.
	if atomic.LoadInt32(&cleanupCalls) != 1 {
		t.Errorf("expected runtime Cleanup to be called once, got %d", atomic.LoadInt32(&cleanupCalls))
	}
}

// TestSpawnThread_AgentRuntimePreparer_FallbackOnNilRuntime verifies that
// when the preparer returns (nil, nil) — e.g. for an agent without memory —
// the thread falls back to the legacy global toolRegistry/toolExecutor path.
func TestSpawnThread_AgentRuntimePreparer_FallbackOnNilRuntime(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("rt-fallback", "/tmp", "claude-haiku-4")

	tm.SetAgentRuntimePreparer(func(_ context.Context, _ string) (*AgentRuntime, error) {
		return nil, nil
	})

	toolReg := &stubToolRegistry{
		byName: map[string]backend.Tool{"read_file": stubTool("read_file")},
	}
	tm.SetToolRegistry(toolReg)
	executorCalled := int32(0)
	tm.SetToolExecutor(func(_ context.Context, _ string, _ map[string]any) (string, error) {
		atomic.AddInt32(&executorCalled, 1)
		return "ok", nil
	})

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:       "Reader",
		ModelID:    "claude-haiku-4",
		LocalTools: []string{"read_file"},
	})

	fb := &toolCallBackend{toolName: "read_file"}
	thread, _ := tm.Create(CreateParams{
		SessionID: sess.ID,
		AgentID:   "Reader",
		Task:      "read",
	})

	done := make(chan struct{})
	broadcast := func(_, msgType string, _ map[string]any) {
		if msgType == "thread_done" {
			close(done)
		}
	}

	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, fb, broadcast, NewCostAccumulator(0), nil)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if atomic.LoadInt32(&executorCalled) != 1 {
		t.Errorf("expected fallback toolExecutor to be called once when runtime is nil, got %d", atomic.LoadInt32(&executorCalled))
	}
}
