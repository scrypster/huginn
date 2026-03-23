package agent

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// obsBackendNoTools returns a simple response with no tool calls.
type obsBackendNoTools struct{}

func (obsBackendNoTools) ChatCompletion(_ context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
	return &backend.ChatResponse{Content: "hello", DoneReason: "stop"}, nil
}
func (obsBackendNoTools) Health(_ context.Context) error   { return nil }
func (obsBackendNoTools) Shutdown(_ context.Context) error { return nil }
func (obsBackendNoTools) ContextWindow() int               { return 8000 }

// obsBackendWithTool returns one tool call, then a final response.
type obsBackendWithTool struct {
	calls int
}

func (f *obsBackendWithTool) ChatCompletion(_ context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
	f.calls++
	if f.calls == 1 {
		return &backend.ChatResponse{
			Content: "",
			ToolCalls: []backend.ToolCall{
				{ID: "tc1", Function: backend.ToolCallFunction{Name: "obs_test_tool", Arguments: map[string]any{"key": "val"}}},
			},
		}, nil
	}
	return &backend.ChatResponse{Content: "done", DoneReason: "stop"}, nil
}
func (f *obsBackendWithTool) Health(_ context.Context) error   { return nil }
func (f *obsBackendWithTool) Shutdown(_ context.Context) error { return nil }
func (f *obsBackendWithTool) ContextWindow() int               { return 8000 }

// obsTool implements tools.Tool for test purposes.
type obsTool struct {
	name   string
	result tools.ToolResult
}

func (t *obsTool) Name() string                      { return t.name }
func (t *obsTool) Description() string               { return "test tool" }
func (t *obsTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *obsTool) Schema() backend.Tool {
	return backend.Tool{Function: backend.ToolFunction{Name: t.name}}
}
func (t *obsTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	return t.result
}

func TestRunLoop_CompletesWithStopReason(t *testing.T) {
	t.Parallel()

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns:  5,
		ModelName: "test-model",
		Messages:  []backend.Message{{Role: "user", Content: "hi"}},
		Backend:   obsBackendNoTools{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != "stop" {
		t.Errorf("expected stop reason 'stop', got %q", result.StopReason)
	}
	if result.FinalContent != "hello" {
		t.Errorf("expected final content 'hello', got %q", result.FinalContent)
	}
}

func TestRunLoop_CompletesWithToolCall(t *testing.T) {
	t.Parallel()

	toolReg := tools.NewRegistry()
	toolReg.Register(&obsTool{name: "obs_test_tool", result: tools.ToolResult{Output: "ok"}})

	fb := &obsBackendWithTool{}
	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns:    5,
		ModelName:   "test-model",
		Messages:    []backend.Message{{Role: "user", Content: "do stuff"}},
		Backend:     fb,
		Tools:       toolReg,
		ToolSchemas: toolReg.AllSchemas(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TurnCount < 2 {
		t.Errorf("expected at least 2 turns (tool + final), got %d", result.TurnCount)
	}
}

func TestRunLoop_ToolCallCallsOnToolDone(t *testing.T) {
	t.Parallel()

	toolReg := tools.NewRegistry()
	toolReg.Register(&obsTool{name: "obs_test_tool", result: tools.ToolResult{Output: "ok"}})

	var toolsDone []string
	fb := &obsBackendWithTool{}
	_, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns:    5,
		ModelName:   "test-model",
		Messages:    []backend.Message{{Role: "user", Content: "do stuff"}},
		Backend:     fb,
		Tools:       toolReg,
		ToolSchemas: toolReg.AllSchemas(),
		OnToolDone: func(name string, _ tools.ToolResult) {
			toolsDone = append(toolsDone, name)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(toolsDone) == 0 {
		t.Error("OnToolDone should have been called at least once")
	}
	if toolsDone[0] != "obs_test_tool" {
		t.Errorf("expected tool name 'obs_test_tool', got %q", toolsDone[0])
	}
}

func TestRunLoop_ToolErrorCallsOnToolDone(t *testing.T) {
	t.Parallel()

	toolReg := tools.NewRegistry()
	toolReg.Register(&obsTool{name: "obs_test_tool", result: tools.ToolResult{IsError: true, Error: "boom"}})

	var errorSeen bool
	fb := &obsBackendWithTool{}
	_, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns:    5,
		ModelName:   "test-model",
		Messages:    []backend.Message{{Role: "user", Content: "do stuff"}},
		Backend:     fb,
		Tools:       toolReg,
		ToolSchemas: toolReg.AllSchemas(),
		OnToolDone: func(_ string, result tools.ToolResult) {
			if result.IsError {
				errorSeen = true
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !errorSeen {
		t.Error("OnToolDone should have been called with an error result")
	}
}

func TestRunLoop_NilSC_NoPanic(t *testing.T) {
	t.Parallel()
	_, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns:  5,
		ModelName: "test-model",
		Messages:  []backend.Message{{Role: "user", Content: "hi"}},
		Backend:   obsBackendNoTools{},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunLoop_CorrelationID_PropagatedInLogs(t *testing.T) {
	t.Parallel()
	// Verify that CorrelationID is accepted in RunLoopConfig without panic.
	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns:      5,
		ModelName:     "test-model",
		Messages:      []backend.Message{{Role: "user", Content: "hi"}},
		Backend:       obsBackendNoTools{},
		CorrelationID: "test-correlation-123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason == "" {
		t.Error("expected non-empty stop reason")
	}
}
