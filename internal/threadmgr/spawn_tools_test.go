package threadmgr

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
)

// stubToolRegistry is a ToolRegistryIface that returns configurable schemas
// and records Execute calls.
type stubToolRegistry struct {
	builtins []backend.Tool
	byName   map[string]backend.Tool
	executed []string
	result   string
	execErr  error
}

func (s *stubToolRegistry) AllBuiltinSchemas() []backend.Tool { return s.builtins }
func (s *stubToolRegistry) SchemasByNames(names []string) []backend.Tool {
	var out []backend.Tool
	for _, name := range names {
		if t, ok := s.byName[name]; ok {
			out = append(out, t)
		}
	}
	return out
}
func (s *stubToolRegistry) Execute(_ context.Context, name string, _ map[string]any) (string, error) {
	s.executed = append(s.executed, name)
	return s.result, s.execErr
}

func stubTool(name string) backend.Tool {
	return backend.Tool{Type: "function", Function: backend.ToolFunction{Name: name}}
}

// toolCallBackend returns responses: first call invokes a named tool,
// second call returns a finish tool call.
type toolCallBackend struct {
	toolName string
	calls    int
}

func (b *toolCallBackend) ChatCompletion(_ context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
	b.calls++
	if b.calls == 1 {
		return &backend.ChatResponse{
			Content:    "",
			DoneReason: "tool_use",
			ToolCalls: []backend.ToolCall{
				{
					ID: "call-1",
					Function: backend.ToolCallFunction{
						Name:      b.toolName,
						Arguments: map[string]any{"input": "hello"},
					},
				},
			},
		}, nil
	}
	// Second call: finish
	return &backend.ChatResponse{
		Content:    "",
		DoneReason: "tool_use",
		ToolCalls: []backend.ToolCall{
			{
				ID: "finish-1",
				Function: backend.ToolCallFunction{
					Name:      "finish",
					Arguments: map[string]any{"summary": "done", "status": "success"},
				},
			},
		},
	}, nil
}
func (b *toolCallBackend) Health(_ context.Context) error   { return nil }
func (b *toolCallBackend) Shutdown(_ context.Context) error { return nil }
func (b *toolCallBackend) ContextWindow() int               { return 8192 }

// TestSpawnThread_ExecutorDispatchesLocalTool verifies that when an agent has
// local_tools configured and a toolExecutor is wired, the executor is called
// for the tool and the result is appended to history.
func TestSpawnThread_ExecutorDispatchesLocalTool(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-haiku-4")

	toolReg := &stubToolRegistry{
		byName: map[string]backend.Tool{
			"read_file": stubTool("read_file"),
		},
		result: "file contents here",
	}
	tm.SetToolRegistry(toolReg)

	var executorCalled []string
	tm.SetToolExecutor(func(_ context.Context, name string, _ map[string]any) (string, error) {
		executorCalled = append(executorCalled, name)
		return toolReg.result, nil
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
		Task:      "read a file",
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
		t.Fatal("timed out waiting for thread to complete")
	}

	if len(executorCalled) == 0 {
		t.Fatal("tool executor was never called; local tool dispatch is broken")
	}
	if executorCalled[0] != "read_file" {
		t.Errorf("expected executor called with read_file, got %v", executorCalled)
	}
}

// TestSpawnThread_WildcardLocalToolsGetAllBuiltins verifies that an agent with
// local_tools=["*"] receives AllBuiltinSchemas in the LLM request.
func TestSpawnThread_WildcardLocalToolsGetAllBuiltins(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-haiku-4")

	builtins := []backend.Tool{
		stubTool("bash"),
		stubTool("read_file"),
		stubTool("write_file"),
	}
	toolReg := &stubToolRegistry{builtins: builtins}
	tm.SetToolRegistry(toolReg)
	tm.SetToolExecutor(func(_ context.Context, _ string, _ map[string]any) (string, error) {
		return "ok", nil
	})

	// Capture the tools sent to the backend on the first LLM call.
	var capturedTools []backend.Tool
	captureBackend := &capturingToolBackend{captured: &capturedTools}

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:       "AllTools",
		ModelID:    "claude-haiku-4",
		LocalTools: []string{"*"},
	})

	thread, _ := tm.Create(CreateParams{
		SessionID: sess.ID,
		AgentID:   "AllTools",
		Task:      "do stuff",
	})

	done := make(chan struct{})
	broadcast := func(_, msgType string, _ map[string]any) {
		if msgType == "thread_done" {
			close(done)
		}
	}

	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, captureBackend, broadcast, NewCostAccumulator(0), nil)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for thread to complete")
	}

	// The first LLM call should have included bash, read_file, write_file.
	toolNames := map[string]bool{}
	for _, tool := range capturedTools {
		toolNames[tool.Function.Name] = true
	}
	for _, wantName := range []string{"bash", "read_file", "write_file"} {
		if !toolNames[wantName] {
			t.Errorf("expected tool %q in LLM request tools, got %v", wantName, toolNames)
		}
	}
}

// TestSpawnThread_NoExecutorReturnsUnknownTool verifies that without a wired
// executor, calls to unknown tools return "unknown tool: X" (backwards compat).
func TestSpawnThread_NoExecutorReturnsUnknownTool(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-haiku-4")
	// No SetToolExecutor called.

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Bob", ModelID: "claude-haiku-4"})

	fb := &toolCallBackend{toolName: "some_mystery_tool"}

	var toolDonePayload map[string]any
	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bob", Task: "test"})

	done := make(chan struct{})
	broadcast := func(_, msgType string, payload map[string]any) {
		if msgType == "thread_tool_done" {
			toolDonePayload = payload
		}
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

	if toolDonePayload == nil {
		t.Fatal("no thread_tool_done broadcast received")
	}
	summary, _ := toolDonePayload["result_summary"].(string)
	if !strings.Contains(summary, "unknown tool") {
		t.Errorf("expected 'unknown tool' in result_summary, got %q", summary)
	}
}

// TestSpawnThread_ExecutorErrorPropagatesToHistory verifies that an executor
// error produces "tool error: ..." in the LLM history via the broadcast.
func TestSpawnThread_ExecutorErrorPropagatesToHistory(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-haiku-4")

	toolReg := &stubToolRegistry{
		byName: map[string]backend.Tool{"bash": stubTool("bash")},
	}
	tm.SetToolRegistry(toolReg)
	tm.SetToolExecutor(func(_ context.Context, _ string, _ map[string]any) (string, error) {
		return "", fmt.Errorf("permission denied: sandbox violation")
	})

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:       "Hacker",
		ModelID:    "claude-haiku-4",
		LocalTools: []string{"bash"},
	})

	fb := &toolCallBackend{toolName: "bash"}

	var toolDonePayload map[string]any
	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Hacker", Task: "test"})

	done := make(chan struct{})
	broadcast := func(_, msgType string, payload map[string]any) {
		if msgType == "thread_tool_done" {
			toolDonePayload = payload
		}
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

	if toolDonePayload == nil {
		t.Fatal("no thread_tool_done broadcast received")
	}
	summary, _ := toolDonePayload["result_summary"].(string)
	if !strings.Contains(summary, "tool error") {
		t.Errorf("expected 'tool error' in result_summary, got %q", summary)
	}
}

// capturingToolBackend records the Tools field from the first ChatCompletion
// request, then returns a finish tool call to end the thread.
type capturingToolBackend struct {
	captured *[]backend.Tool
	calls    int
}

func (b *capturingToolBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	b.calls++
	if b.calls == 1 {
		// Capture the tool schemas sent to the LLM on first call.
		*b.captured = append(*b.captured, req.Tools...)
		// Return a finish tool call so the thread exits cleanly.
		return &backend.ChatResponse{
			DoneReason: "tool_use",
			ToolCalls: []backend.ToolCall{
				{
					ID: "f1",
					Function: backend.ToolCallFunction{
						Name:      "finish",
						Arguments: map[string]any{"summary": "done", "status": "success"},
					},
				},
			},
		}, nil
	}
	return &backend.ChatResponse{Content: "done", DoneReason: "stop"}, nil
}
func (b *capturingToolBackend) Health(_ context.Context) error   { return nil }
func (b *capturingToolBackend) Shutdown(_ context.Context) error { return nil }
func (b *capturingToolBackend) ContextWindow() int               { return 8192 }
