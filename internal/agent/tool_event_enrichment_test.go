package agent

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/stats"
	"github.com/scrypster/huginn/internal/tools"
)

// TestChatWithAgent_ToolResultEvent_HasArgsAndResult verifies that when
// ChatWithAgent uses the onEvent path (onToolEvent == nil), the StreamToolResult
// event includes the tool call args and result content.
//
// Regression test: previously NewToolResultEvent only carried {tool, success}
// with no args or result text, so the frontend ToolCall chip always showed
// empty args and no result.
func TestChatWithAgent_ToolResultEvent_HasArgsAndResult(t *testing.T) {
	t.Parallel()

	b := &obsBackendWithTool{} // first call → tool call, second → "done"
	toolReg := tools.NewRegistry()
	toolReg.Register(&obsTool{
		name:   "obs_test_tool",
		result: tools.ToolResult{Output: "file.txt\nfoo.go\n"},
	})

	models := modelconfig.DefaultModels()
	o, err := NewOrchestrator(b, models, nil, nil, stats.NoopCollector{}, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	o.SetTools(toolReg, nil)

	sess, sessErr := o.NewSession("")
	if sessErr != nil {
		t.Fatalf("NewSession: %v", sessErr)
	}

	ag := agents.FromDef(agents.AgentDef{Name: "test-agent", Model: "test-model"})

	var capturedEvents []backend.StreamEvent
	chatErr := o.ChatWithAgent(context.Background(), ag, "use the tool", sess.ID, nil, nil, func(ev backend.StreamEvent) {
		capturedEvents = append(capturedEvents, ev)
	})
	if chatErr != nil {
		t.Fatalf("ChatWithAgent: %v", chatErr)
	}

	// Find the tool_result event.
	var toolResultEv *backend.StreamEvent
	for i := range capturedEvents {
		if capturedEvents[i].Type == backend.StreamToolResult {
			toolResultEv = &capturedEvents[i]
			break
		}
	}
	if toolResultEv == nil {
		t.Fatal("expected StreamToolResult event but none was captured")
	}
	p := toolResultEv.Payload
	if p == nil {
		t.Fatal("tool_result event has nil payload")
	}

	// args must be present — obsBackendWithTool sends {"key": "val"}.
	args, hasArgs := p["args"]
	if !hasArgs || args == nil {
		t.Errorf("expected 'args' in tool_result payload, got payload=%v", p)
	} else {
		argsMap, ok := args.(map[string]any)
		if !ok {
			t.Errorf("expected args to be map[string]any, got %T", args)
		} else if argsMap["key"] != "val" {
			t.Errorf("expected args[key]=val, got %v", argsMap["key"])
		}
	}

	// result must be the tool output.
	result, _ := p["result"].(string)
	if result != "file.txt\nfoo.go\n" {
		t.Errorf("expected result 'file.txt\\nfoo.go\\n', got %q", result)
	}
}

// TestChatWithAgent_ToolResultEvent_FailedToolSurfacesError verifies that when
// a tool call fails (IsError=true, empty Output), the StreamToolResult event's
// "result" field carries the error text instead of an empty string.
//
// Regression: chatOnToolDone built the UI/model-facing payload from
// result.Output only, never result.Error — a failed tool call (e.g. read_file
// hitting a sandbox/not-found error) surfaced as result:"" on the wire, live
// captured on a running server as {"result":"","success":false,"tool":"read_file"}
// with the real error text silently dropped. The model's own next-turn context
// gets "error: <message>" (loop.go), but the UI/onEvent payload did not.
func TestChatWithAgent_ToolResultEvent_FailedToolSurfacesError(t *testing.T) {
	t.Parallel()

	b := &obsBackendWithTool{}
	toolReg := tools.NewRegistry()
	toolReg.Register(&obsTool{
		name:   "obs_test_tool",
		result: tools.ToolResult{IsError: true, Error: "read_file: open mathutil.go: no such file or directory"},
	})

	models := modelconfig.DefaultModels()
	o, err := NewOrchestrator(b, models, nil, nil, stats.NoopCollector{}, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	o.SetTools(toolReg, nil)

	sess, sessErr := o.NewSession("")
	if sessErr != nil {
		t.Fatalf("NewSession: %v", sessErr)
	}

	ag := agents.FromDef(agents.AgentDef{Name: "test-agent", Model: "test-model"})

	var capturedEvents []backend.StreamEvent
	chatErr := o.ChatWithAgent(context.Background(), ag, "use the tool", sess.ID, nil, nil, func(ev backend.StreamEvent) {
		capturedEvents = append(capturedEvents, ev)
	})
	if chatErr != nil {
		t.Fatalf("ChatWithAgent: %v", chatErr)
	}

	var toolResultEv *backend.StreamEvent
	for i := range capturedEvents {
		if capturedEvents[i].Type == backend.StreamToolResult {
			toolResultEv = &capturedEvents[i]
			break
		}
	}
	if toolResultEv == nil {
		t.Fatal("expected StreamToolResult event but none was captured")
	}
	p := toolResultEv.Payload
	result, _ := p["result"].(string)
	if result == "" {
		t.Fatal("expected non-empty result carrying the error text, got empty string")
	}
	if result != "read_file: open mathutil.go: no such file or directory" {
		t.Errorf("expected result to be the error text, got %q", result)
	}
	if success, _ := p["success"].(bool); success {
		t.Error("expected success=false for a failed tool call")
	}
}

// TestChatWithAgent_ToolCallEvent_HasArgs verifies that the StreamToolCall
// event also includes the tool arguments, so the frontend can show a
// "running..." chip with context before the result arrives.
func TestChatWithAgent_ToolCallEvent_HasArgs(t *testing.T) {
	t.Parallel()

	b := &obsBackendWithTool{}
	toolReg := tools.NewRegistry()
	toolReg.Register(&obsTool{
		name:   "obs_test_tool",
		result: tools.ToolResult{Output: "ok"},
	})

	models := modelconfig.DefaultModels()
	o, err := NewOrchestrator(b, models, nil, nil, stats.NoopCollector{}, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	o.SetTools(toolReg, nil)

	sess, sessErr := o.NewSession("")
	if sessErr != nil {
		t.Fatalf("NewSession: %v", sessErr)
	}

	ag := agents.FromDef(agents.AgentDef{Name: "test-agent", Model: "test-model"})

	var capturedEvents []backend.StreamEvent
	chatErr := o.ChatWithAgent(context.Background(), ag, "use the tool", sess.ID, nil, nil, func(ev backend.StreamEvent) {
		capturedEvents = append(capturedEvents, ev)
	})
	if chatErr != nil {
		t.Fatalf("ChatWithAgent: %v", chatErr)
	}

	// Find the tool_call event.
	var toolCallEv *backend.StreamEvent
	for i := range capturedEvents {
		if capturedEvents[i].Type == backend.StreamToolCall {
			toolCallEv = &capturedEvents[i]
			break
		}
	}
	if toolCallEv == nil {
		t.Fatal("expected StreamToolCall event but none was captured")
	}
	p := toolCallEv.Payload
	if p == nil {
		t.Fatal("tool_call event has nil payload")
	}

	// args must be present.
	args, hasArgs := p["args"]
	if !hasArgs || args == nil {
		t.Errorf("expected 'args' in tool_call payload, got payload=%v", p)
	}
}
