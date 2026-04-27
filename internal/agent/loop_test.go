package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// mockBackend is a test double for backend.Backend.
type mockBackend struct {
	responses []*backend.ChatResponse
	errors    []error
	callCount int
	mu        sync.Mutex
	// lastRequests captures the ChatRequest passed on each call for inspection.
	lastRequests []backend.ChatRequest
}

func (m *mockBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastRequests = append(m.lastRequests, req)
	idx := m.callCount
	m.callCount++
	if idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}
	if idx < len(m.responses) {
		resp := m.responses[idx]
		// Fire any OnToken callback with the response content so streaming tests work.
		if req.OnToken != nil && resp != nil && resp.Content != "" {
			req.OnToken(resp.Content)
		}
		return resp, nil
	}
	return &backend.ChatResponse{Content: "done", DoneReason: "stop"}, nil
}

func (m *mockBackend) Health(_ context.Context) error   { return nil }
func (m *mockBackend) Shutdown(_ context.Context) error { return nil }
func (m *mockBackend) ContextWindow() int               { return 128_000 }

// mockTool is a test double for tools.Tool.
type mockTool struct {
	name        string
	result      tools.ToolResult
	callCount   int
	mu          sync.Mutex
	shouldPanic bool
}

func (t *mockTool) Name() string                      { return t.name }
func (t *mockTool) Description() string               { return "" }
func (t *mockTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *mockTool) Schema() backend.Tool {
	return backend.Tool{Function: backend.ToolFunction{Name: t.name}}
}
func (t *mockTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	if t.shouldPanic {
		panic(fmt.Sprintf("mockTool %s intentional panic", t.name))
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.callCount++
	return t.result
}

// newRegistryWith creates a tools.Registry with the supplied tools pre-registered.
func newRegistryWith(ts ...tools.Tool) *tools.Registry {
	reg := tools.NewRegistry()
	for _, t := range ts {
		reg.Register(t)
	}
	return reg
}

// toolCallResponse builds a ChatResponse that contains a single tool call.
func toolCallResponse(toolName, callID string) *backend.ChatResponse {
	return &backend.ChatResponse{
		Content:    "",
		DoneReason: "tool_calls",
		ToolCalls: []backend.ToolCall{
			{
				ID: callID,
				Function: backend.ToolCallFunction{
					Name:      toolName,
					Arguments: map[string]any{},
				},
			},
		},
	}
}

// stopResponse builds a ChatResponse that ends the loop.
func stopResponse(content string) *backend.ChatResponse {
	return &backend.ChatResponse{
		Content:    content,
		DoneReason: "stop",
	}
}

// --------------------------------------------------------------------------
// Tests
// --------------------------------------------------------------------------

// TestRunLoop_StopsWhenNoToolCalls verifies that a backend response with no
// tool calls ends the loop immediately with StopReason="stop".
func TestRunLoop_StopsWhenNoToolCalls(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			stopResponse("hello world"),
		},
	}
	reg := newRegistryWith()

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "stop")
	}
	if result.TurnCount != 1 {
		t.Errorf("TurnCount = %d, want 1", result.TurnCount)
	}
	if result.FinalContent != "hello world" {
		t.Errorf("FinalContent = %q, want %q", result.FinalContent, "hello world")
	}
}

// TestRunLoop_ToolCallExecuted verifies a tool is executed and the loop runs a
// second turn after getting tool results.
func TestRunLoop_ToolCallExecuted(t *testing.T) {
	tool := &mockTool{
		name:   "mytool",
		result: tools.ToolResult{Output: "tool output"},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("mytool", "call-1"),
			stopResponse("done"),
		},
	}
	reg := newRegistryWith(tool)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "use mytool"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "stop")
	}
	if result.TurnCount != 2 {
		t.Errorf("TurnCount = %d, want 2", result.TurnCount)
	}
	if tool.callCount != 1 {
		t.Errorf("tool.callCount = %d, want 1", tool.callCount)
	}
	// Verify the tool result message is in the conversation history.
	found := false
	for _, msg := range result.Messages {
		if msg.Role == "tool" && msg.ToolName == "mytool" && strings.Contains(msg.Content, "tool output") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected tool result message in Messages history")
	}
}

// TestRunLoop_MaxTurnsReached verifies that a backend that never stops tool calls
// is cut off at MaxTurns with StopReason="max_turns".
func TestRunLoop_MaxTurnsReached(t *testing.T) {
	// Always return a tool call.
	mb := &mockBackend{}
	for i := 0; i < 10; i++ {
		mb.responses = append(mb.responses, toolCallResponse("mytool", "call"))
	}
	tool := &mockTool{name: "mytool", result: tools.ToolResult{Output: "ok"}}
	reg := newRegistryWith(tool)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 3,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "loop forever"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "max_turns" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "max_turns")
	}
	if result.TurnCount != 3 {
		t.Errorf("TurnCount = %d, want 3", result.TurnCount)
	}
}

// TestRunLoop_BackendError verifies that a backend error sets StopReason="error"
// and propagates the error to the caller.
func TestRunLoop_BackendError(t *testing.T) {
	mb := &mockBackend{
		errors: []error{errors.New("backend exploded")},
	}
	reg := newRegistryWith()

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result == nil {
		t.Fatal("expected non-nil result even on error")
	}
	if result.StopReason != "error" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "error")
	}
	if result.TurnCount != 1 {
		t.Errorf("TurnCount = %d, want 1", result.TurnCount)
	}
	if !strings.Contains(err.Error(), "backend exploded") {
		t.Errorf("error does not contain original message: %v", err)
	}
}

// TestRunLoop_UnknownToolName verifies that an unknown tool name does not crash
// the loop and the error is fed back as a tool message.
func TestRunLoop_UnknownToolName(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("nonexistent_tool", "call-1"),
			stopResponse("done"),
		},
	}
	reg := newRegistryWith() // empty registry

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "call unknown tool"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Loop should have continued and ended normally.
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "stop")
	}
	// Verify the second backend call included an "unknown tool" error message.
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) < 2 {
		t.Fatalf("expected at least 2 backend calls, got %d", len(mb.lastRequests))
	}
	secondReqMsgs := mb.lastRequests[1].Messages
	found := false
	for _, msg := range secondReqMsgs {
		if msg.Role == "tool" && strings.Contains(msg.Content, "unknown tool") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'unknown tool' error in second backend call messages")
	}
}

// TestRunLoop_EmptyToolName verifies that a tool call with an empty function name
// does not crash and the error is fed back as a tool message.
func TestRunLoop_EmptyToolName(t *testing.T) {
	emptyNameResp := &backend.ChatResponse{
		DoneReason: "tool_calls",
		ToolCalls: []backend.ToolCall{
			{
				ID: "call-empty",
				Function: backend.ToolCallFunction{
					Name:      "", // intentionally empty
					Arguments: map[string]any{},
				},
			},
		},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			emptyNameResp,
			stopResponse("recovered"),
		},
	}
	reg := newRegistryWith()

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "empty tool name"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "stop")
	}
	// The second backend call should include the empty-name error message.
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) < 2 {
		t.Fatalf("expected at least 2 backend calls, got %d", len(mb.lastRequests))
	}
	secondReqMsgs := mb.lastRequests[1].Messages
	found := false
	for _, msg := range secondReqMsgs {
		if msg.Role == "tool" && strings.Contains(msg.Content, "empty function name") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'empty function name' error in second backend call messages")
	}
}

// TestRunLoop_ToolReturnsError verifies that a tool returning IsError=true does
// not stop the loop; the error content is forwarded and the loop continues.
func TestRunLoop_ToolReturnsError(t *testing.T) {
	tool := &mockTool{
		name: "errtool",
		result: tools.ToolResult{
			IsError: true,
			Error:   "something went wrong",
		},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("errtool", "call-1"),
			stopResponse("I handled the error"),
		},
	}
	reg := newRegistryWith(tool)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "call errtool"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Loop should not be stopped by a tool error.
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "stop")
	}
	if result.TurnCount != 2 {
		t.Errorf("TurnCount = %d, want 2", result.TurnCount)
	}
	// Verify the tool message contains the "error: " prefix.
	found := false
	for _, msg := range result.Messages {
		if msg.Role == "tool" && strings.HasPrefix(msg.Content, "error: ") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'error: ' prefixed tool result message in history")
	}
}

// TestRunLoop_TokenStreamingCallback verifies that the OnToken callback is
// forwarded to the backend via the ChatRequest.
func TestRunLoop_TokenStreamingCallback(t *testing.T) {
	var tokens []string
	var mu sync.Mutex

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			stopResponse("streamed content"),
		},
	}
	reg := newRegistryWith()

	_, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "stream me"}},
		OnToken: func(tok string) {
			mu.Lock()
			tokens = append(tokens, tok)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(tokens) == 0 {
		t.Error("expected OnToken to be called at least once")
	}
	combined := strings.Join(tokens, "")
	if combined != "streamed content" {
		t.Errorf("combined tokens = %q, want %q", combined, "streamed content")
	}
}

// TestRunLoop_DefaultMaxTurns verifies that MaxTurns=0 defaults to 50.
func TestRunLoop_DefaultMaxTurns(t *testing.T) {
	// Always return a tool call — we rely on the loop's default MaxTurns=50 cap.
	tool := &mockTool{name: "inf", result: tools.ToolResult{Output: "ok"}}
	mb := &mockBackend{}
	for i := 0; i < 55; i++ {
		mb.responses = append(mb.responses, toolCallResponse("inf", "call"))
	}
	reg := newRegistryWith(tool)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 0, // should default to 50
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "go"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "max_turns" {
		t.Errorf("expected max_turns, got %q", result.StopReason)
	}
	if result.TurnCount != 50 {
		t.Errorf("expected TurnCount=50, got %d", result.TurnCount)
	}
}

// TestRunLoop_OnToolCallAndDoneCallbacks verifies that OnToolCall and OnToolDone
// are invoked when a tool is executed.
func TestRunLoop_OnToolCallAndDoneCallbacks(t *testing.T) {
	var calledNames []string
	var doneCalled bool

	tool := &mockTool{
		name:   "callback_tool",
		result: tools.ToolResult{Output: "output"},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("callback_tool", "c1"),
			stopResponse("done"),
		},
	}
	reg := newRegistryWith(tool)

	_, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "run callback_tool"}},
		OnToolCall: func(callID string, name string, args map[string]any) {
			calledNames = append(calledNames, name)
		},
		OnToolDone: func(callID string, name string, result tools.ToolResult) {
			doneCalled = true
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calledNames) != 1 || calledNames[0] != "callback_tool" {
		t.Errorf("expected OnToolCall for callback_tool, got %v", calledNames)
	}
	if !doneCalled {
		t.Error("expected OnToolDone to be called")
	}
}

// TestRunLoop_MultipleToolCallsPerTurn verifies that multiple tool calls in a
// single response are all executed.
func TestRunLoop_MultipleToolCallsPerTurn(t *testing.T) {
	toolA := &mockTool{name: "toolA", result: tools.ToolResult{Output: "a"}}
	toolB := &mockTool{name: "toolB", result: tools.ToolResult{Output: "b"}}

	multiCallResp := &backend.ChatResponse{
		Content:    "",
		DoneReason: "tool_calls",
		ToolCalls: []backend.ToolCall{
			{ID: "c1", Function: backend.ToolCallFunction{Name: "toolA", Arguments: map[string]any{}}},
			{ID: "c2", Function: backend.ToolCallFunction{Name: "toolB", Arguments: map[string]any{}}},
		},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			multiCallResp,
			stopResponse("done"),
		},
	}
	reg := newRegistryWith(toolA, toolB)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "use both"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("expected stop, got %q", result.StopReason)
	}
	if toolA.callCount != 1 {
		t.Errorf("expected toolA.callCount=1, got %d", toolA.callCount)
	}
	if toolB.callCount != 1 {
		t.Errorf("expected toolB.callCount=1, got %d", toolB.callCount)
	}
}

// TestRunLoop_MessagesCopied verifies that RunLoop does not modify the input
// Messages slice (uses a copy internally).
func TestRunLoop_MessagesCopied(t *testing.T) {
	original := []backend.Message{{Role: "user", Content: "hi"}}
	originalLen := len(original)

	mb := &mockBackend{
		responses: []*backend.ChatResponse{stopResponse("ok")},
	}
	reg := newRegistryWith()

	_, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: original,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(original) != originalLen {
		t.Errorf("input Messages slice was modified: len=%d, want %d", len(original), originalLen)
	}
}

// TestRunLoop_ToolOutputEmptyAndError verifies the content fallback for tools
// that return Output="" and IsError=false but Error != "".
func TestRunLoop_ToolOutputEmptyAndError(t *testing.T) {
	tool := &mockTool{
		name: "quirky_tool",
		result: tools.ToolResult{
			IsError: false,
			Output:  "",
			Error:   "non-fatal warning",
		},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("quirky_tool", "c1"),
			stopResponse("done"),
		},
	}
	reg := newRegistryWith(tool)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "use quirky_tool"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Find the tool message and verify content is the error fallback.
	found := false
	for _, msg := range result.Messages {
		if msg.Role == "tool" && msg.ToolName == "quirky_tool" {
			if strings.Contains(msg.Content, "non-fatal warning") {
				found = true
			}
			break
		}
	}
	if !found {
		t.Error("expected 'non-fatal warning' in tool message content")
	}
}

// TestRunLoop_OnBeforeWriteApproved verifies that OnBeforeWrite is called
// for write_file and that returning true allows the write.
func TestRunLoop_OnBeforeWriteApproved(t *testing.T) {
	var approvals int
	onBeforeWrite := func(path string, oldContent, newContent []byte) bool {
		approvals++
		if path != "test.go" {
			t.Errorf("expected path 'test.go', got %q", path)
		}
		if oldContent != nil {
			t.Errorf("expected oldContent=nil for new file, got %v", oldContent)
		}
		if string(newContent) != "new content" {
			t.Errorf("expected newContent='new content', got %q", string(newContent))
		}
		return true // approve
	}

	tool := &mockTool{
		name:   "write_file",
		result: tools.ToolResult{Output: "written"},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{
				Content:    "",
				DoneReason: "tool_calls",
				ToolCalls: []backend.ToolCall{
					{
						ID: "c1",
						Function: backend.ToolCallFunction{
							Name: "write_file",
							Arguments: map[string]any{
								"file_path": "test.go",
								"content":   "new content",
							},
						},
					},
				},
			},
			stopResponse("done"),
		},
	}
	reg := newRegistryWith(tool)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns:      5,
		Backend:       mb,
		Tools:         reg,
		Messages:      []backend.Message{{Role: "user", Content: "write file"}},
		OnBeforeWrite: onBeforeWrite,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("expected stop, got %q", result.StopReason)
	}
	if approvals != 1 {
		t.Errorf("expected OnBeforeWrite called once, got %d", approvals)
	}
	if tool.callCount != 1 {
		t.Errorf("expected tool executed once, got %d", tool.callCount)
	}
}

// TestRunLoop_OnBeforeWriteRejected verifies that returning false from OnBeforeWrite
// rejects the tool call without executing it.
func TestRunLoop_OnBeforeWriteRejected(t *testing.T) {
	var rejections int
	onBeforeWrite := func(path string, oldContent, newContent []byte) bool {
		rejections++
		return false // reject
	}

	tool := &mockTool{
		name:   "write_file",
		result: tools.ToolResult{Output: "written"},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{
				Content:    "",
				DoneReason: "tool_calls",
				ToolCalls: []backend.ToolCall{
					{
						ID: "c1",
						Function: backend.ToolCallFunction{
							Name: "write_file",
							Arguments: map[string]any{
								"file_path": "test.go",
								"content":   "new content",
							},
						},
					},
				},
			},
			stopResponse("done"),
		},
	}
	reg := newRegistryWith(tool)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns:      5,
		Backend:       mb,
		Tools:         reg,
		Messages:      []backend.Message{{Role: "user", Content: "write file"}},
		OnBeforeWrite: onBeforeWrite,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("expected stop, got %q", result.StopReason)
	}
	if rejections != 1 {
		t.Errorf("expected OnBeforeWrite called once, got %d", rejections)
	}
	if tool.callCount != 0 {
		t.Errorf("expected tool NOT executed, got %d calls", tool.callCount)
	}
	// Verify rejection message is in history
	found := false
	for _, msg := range result.Messages {
		if msg.Role == "tool" && strings.Contains(msg.Content, "user rejected") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected rejection message in tool result")
	}
}

// TestRunLoop_OnBeforeWriteNil verifies that when OnBeforeWrite is nil,
// write tools execute without calling it.
func TestRunLoop_OnBeforeWriteNil(t *testing.T) {
	tool := &mockTool{
		name:   "write_file",
		result: tools.ToolResult{Output: "written"},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{
				Content:    "",
				DoneReason: "tool_calls",
				ToolCalls: []backend.ToolCall{
					{
						ID: "c1",
						Function: backend.ToolCallFunction{
							Name: "write_file",
							Arguments: map[string]any{
								"file_path": "test.go",
								"content":   "new content",
							},
						},
					},
				},
			},
			stopResponse("done"),
		},
	}
	reg := newRegistryWith(tool)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "write file"}},
		// OnBeforeWrite is nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("expected stop, got %q", result.StopReason)
	}
	if tool.callCount != 1 {
		t.Errorf("expected tool executed once, got %d", tool.callCount)
	}
}

// TestRunLoop_ToolNotInSchemaBlocked verifies that a tool present in the
// registry but absent from ToolSchemas is blocked at runtime.
func TestRunLoop_ToolNotInSchemaBlocked(t *testing.T) {
	// Register the tool in the registry so the lookup succeeds.
	tool := &mockTool{
		name:   "secret_tool",
		result: tools.ToolResult{Output: "should not appear"},
	}
	reg := newRegistryWith(tool)

	// ToolSchemas only lists "allowed_tool" — not "secret_tool".
	schemas := []backend.Tool{
		{Type: "function", Function: backend.ToolFunction{Name: "allowed_tool"}},
	}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("secret_tool", "call-1"),
			stopResponse("done"),
		},
	}

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns:    5,
		Backend:     mb,
		Tools:       reg,
		ToolSchemas: schemas,
		Messages:    []backend.Message{{Role: "user", Content: "call secret_tool"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "stop")
	}
	// The tool must NOT have been executed.
	if tool.callCount != 0 {
		t.Errorf("expected tool NOT executed, got %d calls", tool.callCount)
	}
	// Verify the denial message is in the conversation history.
	found := false
	for _, msg := range result.Messages {
		if msg.Role == "tool" && strings.Contains(msg.Content, "not available") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'not available' error in tool result message")
	}
}

// TestRunLoop_OnBeforeWrite_ContextCancel verifies the fix from b2211e0: when
// the context is cancelled while OnBeforeWrite is blocking (e.g. waiting for
// user approval in the TUI), the RunLoop should exit without hanging.
func TestRunLoop_OnBeforeWrite_ContextCancel(t *testing.T) {
	t.Parallel()

	tool := &mockTool{
		name:   "write_file",
		result: tools.ToolResult{Output: "written"},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{
				Content:    "",
				DoneReason: "tool_calls",
				ToolCalls: []backend.ToolCall{
					{
						ID: "c1",
						Function: backend.ToolCallFunction{
							Name: "write_file",
							Arguments: map[string]any{
								"file_path": "test.go",
								"content":   "new content",
							},
						},
					},
				},
			},
			stopResponse("done"),
		},
	}
	reg := newRegistryWith(tool)

	ctx, cancel := context.WithCancel(context.Background())

	// OnBeforeWrite blocks until context is cancelled, simulating a user
	// who presses ctrl+c while the approval prompt is active.
	onBeforeWrite := func(path string, oldContent, newContent []byte) bool {
		// Cancel context after a short delay (simulates ctrl+c)
		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()
		// Block here until context is done
		<-ctx.Done()
		return false
	}

	done := make(chan struct{})
	var result *LoopResult
	var loopErr error
	go func() {
		defer close(done)
		result, loopErr = RunLoop(ctx, RunLoopConfig{
			MaxTurns:      5,
			Backend:       mb,
			Tools:         reg,
			Messages:      []backend.Message{{Role: "user", Content: "write file"}},
			OnBeforeWrite: onBeforeWrite,
		})
	}()

	select {
	case <-done:
		// RunLoop returned — no goroutine leak
	case <-time.After(3 * time.Second):
		t.Fatal("RunLoop hung after context cancel during OnBeforeWrite (b2211e0 regression)")
	}

	// The write should not have been executed
	if tool.callCount != 0 {
		t.Errorf("expected write_file NOT executed after ctx cancel, got %d calls", tool.callCount)
	}

	// Either an error or a result with rejection is acceptable
	_ = result
	_ = loopErr
}

// TestRunLoop_OnBeforeWriteNonWriteTool verifies that OnBeforeWrite is not called
// for non-write tools.
func TestRunLoop_OnBeforeWriteNonWriteTool(t *testing.T) {
	var callCount int
	onBeforeWrite := func(path string, oldContent, newContent []byte) bool {
		callCount++
		t.Error("OnBeforeWrite should not be called for non-write tools")
		return true
	}

	tool := &mockTool{
		name:   "read_file",
		result: tools.ToolResult{Output: "read output"},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("read_file", "c1"),
			stopResponse("done"),
		},
	}
	reg := newRegistryWith(tool)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns:      5,
		Backend:       mb,
		Tools:         reg,
		Messages:      []backend.Message{{Role: "user", Content: "read file"}},
		OnBeforeWrite: onBeforeWrite,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("expected stop, got %q", result.StopReason)
	}
	if callCount != 0 {
		t.Errorf("expected OnBeforeWrite not called, got %d calls", callCount)
	}
	if tool.callCount != 1 {
		t.Errorf("expected tool executed once, got %d", tool.callCount)
	}
}

// TestRunLoop_SameToolTwiceInOneTurn verifies that when the LLM returns two calls
// to the same tool in a single turn, each OnToolCall/OnToolDone pair carries the
// correct callID and args — no last-write-wins collision.
func TestRunLoop_SameToolTwiceInOneTurn(t *testing.T) {
	t.Parallel()

	tool := &mockTool{
		name:   "echo_tool",
		result: tools.ToolResult{Output: "echoed"},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{
				DoneReason: "tool_use",
				ToolCalls: []backend.ToolCall{
					{ID: "call-aaa", Function: backend.ToolCallFunction{Name: "echo_tool", Arguments: map[string]any{"msg": "first"}}},
					{ID: "call-bbb", Function: backend.ToolCallFunction{Name: "echo_tool", Arguments: map[string]any{"msg": "second"}}},
				},
			},
			stopResponse("done"),
		},
	}
	reg := newRegistryWith(tool)

	type callPair struct {
		callID string
		args   map[string]any
	}
	var mu sync.Mutex
	var calls []callPair
	var dones []callPair

	_, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "run echo_tool twice"}},
		OnToolCall: func(callID string, name string, args map[string]any) {
			mu.Lock()
			calls = append(calls, callPair{callID: callID, args: args})
			mu.Unlock()
		},
		OnToolDone: func(callID string, name string, result tools.ToolResult) {
			mu.Lock()
			dones = append(dones, callPair{callID: callID})
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 OnToolCall invocations, got %d", len(calls))
	}
	if len(dones) != 2 {
		t.Fatalf("expected 2 OnToolDone invocations, got %d", len(dones))
	}
	if calls[0].callID == calls[1].callID {
		t.Errorf("expected distinct callIDs, both were %q", calls[0].callID)
	}
	seenArgs := make(map[string]string)
	for _, c := range calls {
		if msg, ok := c.args["msg"].(string); ok {
			seenArgs[c.callID] = msg
		}
	}
	if seenArgs["call-aaa"] != "first" {
		t.Errorf("expected call-aaa args msg=first, got %q", seenArgs["call-aaa"])
	}
	if seenArgs["call-bbb"] != "second" {
		t.Errorf("expected call-bbb args msg=second, got %q", seenArgs["call-bbb"])
	}
	doneIDs := make(map[string]bool)
	for _, d := range dones {
		doneIDs[d.callID] = true
	}
	if !doneIDs["call-aaa"] {
		t.Error("expected OnToolDone for call-aaa")
	}
	if !doneIDs["call-bbb"] {
		t.Error("expected OnToolDone for call-bbb")
	}
}

// TestRunLoop_PanicPath_OnToolDoneStillFires verifies that when a tool panics,
// OnToolDone is still called with an error result and the correct callID.
func TestRunLoop_PanicPath_OnToolDoneStillFires(t *testing.T) {
	t.Parallel()

	panicTool := &mockTool{
		name:        "panic_tool",
		shouldPanic: true,
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{
				DoneReason: "tool_use",
				ToolCalls: []backend.ToolCall{
					{ID: "call-panic-1", Function: backend.ToolCallFunction{Name: "panic_tool", Arguments: map[string]any{}}},
				},
			},
			stopResponse("recovered"),
		},
	}
	reg := newRegistryWith(panicTool)

	var doneCalled bool
	var doneCallID string
	var doneIsError bool

	_, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "trigger panic"}},
		OnToolCall: func(callID string, name string, args map[string]any) {},
		OnToolDone: func(callID string, name string, result tools.ToolResult) {
			doneCalled = true
			doneCallID = callID
			doneIsError = result.IsError
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !doneCalled {
		t.Fatal("expected OnToolDone to be called after tool panic")
	}
	if doneCallID != "call-panic-1" {
		t.Errorf("expected doneCallID=call-panic-1, got %q", doneCallID)
	}
	if !doneIsError {
		t.Error("expected OnToolDone result.IsError=true after panic")
	}
}
