package agent

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// --- isIndependentTool tests ---

func TestIsIndependentTool_BashAlwaysSerial(t *testing.T) {
	calls := []backend.ToolCall{
		{Function: backend.ToolCallFunction{Name: "bash", Arguments: map[string]any{"command": "echo hi"}}},
	}
	if isIndependentTool("bash", calls[0].Function.Arguments, calls) {
		t.Error("bash must always be serial")
	}
}

func TestIsIndependentTool_ReadToolsAlwaysIndependent(t *testing.T) {
	reads := []string{"read_file", "grep", "list_dir", "search_files", "web_search", "fetch_url",
		"git_status", "git_log", "git_blame", "git_diff", "git_branch"}
	for _, name := range reads {
		calls := []backend.ToolCall{
			{Function: backend.ToolCallFunction{Name: name, Arguments: map[string]any{}}},
		}
		if !isIndependentTool(name, calls[0].Function.Arguments, calls) {
			t.Errorf("%q should be independent", name)
		}
	}
}

func TestIsIndependentTool_GitWritesAlwaysSerial(t *testing.T) {
	for _, name := range []string{"git_commit", "git_stash"} {
		calls := []backend.ToolCall{
			{Function: backend.ToolCallFunction{Name: name, Arguments: map[string]any{}}},
		}
		if isIndependentTool(name, calls[0].Function.Arguments, calls) {
			t.Errorf("%q should be serial", name)
		}
	}
}

func TestIsIndependentTool_WriteFileSamePathIsSerial(t *testing.T) {
	calls := []backend.ToolCall{
		{Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{"file_path": "/a/b/c.go"}}},
		{Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{"file_path": "/a/b/c.go"}}},
	}
	if isIndependentTool("write_file", calls[0].Function.Arguments, calls) {
		t.Error("write_file to same path should be serial")
	}
}

func TestIsIndependentTool_WriteFileDifferentPathsAreIndependent(t *testing.T) {
	calls := []backend.ToolCall{
		{Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{"file_path": "/a/b/c.go"}}},
		{Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{"file_path": "/a/b/d.go"}}},
	}
	if !isIndependentTool("write_file", calls[0].Function.Arguments, calls) {
		t.Error("write_file to different paths should be independent")
	}
}

func TestIsIndependentTool_EditFileSamePathIsSerial(t *testing.T) {
	calls := []backend.ToolCall{
		{Function: backend.ToolCallFunction{Name: "edit_file", Arguments: map[string]any{"file_path": "/x/y.go"}}},
		{Function: backend.ToolCallFunction{Name: "edit_file", Arguments: map[string]any{"file_path": "/x/y.go"}}},
	}
	if isIndependentTool("edit_file", calls[0].Function.Arguments, calls) {
		t.Error("edit_file to same path should be serial")
	}
}

func TestIsIndependentTool_MCPToolAlwaysSerial(t *testing.T) {
	calls := []backend.ToolCall{
		{Function: backend.ToolCallFunction{Name: "mcp_some_server_action", Arguments: map[string]any{}}},
	}
	if isIndependentTool("mcp_some_server_action", calls[0].Function.Arguments, calls) {
		t.Error("mcp_ prefixed tools should always be serial")
	}
}

func TestIsIndependentTool_UnknownToolIsSerial(t *testing.T) {
	calls := []backend.ToolCall{
		{Function: backend.ToolCallFunction{Name: "some_future_tool", Arguments: map[string]any{}}},
	}
	if isIndependentTool("some_future_tool", calls[0].Function.Arguments, calls) {
		t.Error("unknown tools should default to serial")
	}
}

// --- dispatchTools integration tests ---

type slowMockTool struct {
	name      string
	delay     time.Duration
	callCount int32
	mu        sync.Mutex
	callTimes []time.Time
}

func (t *slowMockTool) Name() string                      { return t.name }
func (t *slowMockTool) Description() string               { return "" }
func (t *slowMockTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *slowMockTool) Schema() backend.Tool {
	return backend.Tool{Type: "function", Function: backend.ToolFunction{Name: t.name}}
}
func (t *slowMockTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	now := time.Now()
	t.mu.Lock()
	t.callTimes = append(t.callTimes, now)
	atomic.AddInt32(&t.callCount, 1)
	t.mu.Unlock()
	time.Sleep(t.delay)
	return tools.ToolResult{Output: "ok"}
}

func TestDispatchTools_IndependentRunConcurrently(t *testing.T) {
	delay := 80 * time.Millisecond

	toolA := &slowMockTool{name: "read_file", delay: delay}
	toolB := &slowMockTool{name: "grep", delay: delay}

	reg := newRegistryWith(toolA, toolB)
	cfg := &RunLoopConfig{Tools: reg}

	calls := []backend.ToolCall{
		{ID: "c1", Function: backend.ToolCallFunction{Name: "read_file", Arguments: map[string]any{}}},
		{ID: "c2", Function: backend.ToolCallFunction{Name: "grep", Arguments: map[string]any{}}},
	}

	start := time.Now()
	results := cfg.dispatchTools(context.Background(), calls)
	elapsed := time.Since(start)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if elapsed > time.Duration(float64(delay)*1.8) {
		t.Errorf("tools appear to have run serially: elapsed=%v, expected<%v", elapsed, time.Duration(float64(delay)*1.8))
	}
	if atomic.LoadInt32(&toolA.callCount) != 1 {
		t.Errorf("toolA called %d times, want 1", atomic.LoadInt32(&toolA.callCount))
	}
	if atomic.LoadInt32(&toolB.callCount) != 1 {
		t.Errorf("toolB called %d times, want 1", atomic.LoadInt32(&toolB.callCount))
	}
}

func TestDispatchTools_BashIsSerial(t *testing.T) {
	delay := 50 * time.Millisecond
	bashA := &slowMockTool{name: "bash", delay: delay}

	reg := newRegistryWith(bashA)
	cfg := &RunLoopConfig{Tools: reg}

	calls := []backend.ToolCall{
		{ID: "b1", Function: backend.ToolCallFunction{Name: "bash", Arguments: map[string]any{"command": "echo 1"}}},
		{ID: "b2", Function: backend.ToolCallFunction{Name: "bash", Arguments: map[string]any{"command": "echo 2"}}},
	}

	start := time.Now()
	_ = cfg.dispatchTools(context.Background(), calls)
	elapsed := time.Since(start)

	if elapsed < 2*delay-10*time.Millisecond {
		t.Errorf("bash calls appear to have run in parallel: elapsed=%v, want>=%v", elapsed, 2*delay)
	}
	if atomic.LoadInt32(&bashA.callCount) != 2 {
		t.Errorf("bash called %d times, want 2", atomic.LoadInt32(&bashA.callCount))
	}
}

func TestDispatchTools_OriginalOrderPreserved(t *testing.T) {
	toolA := &mockTool{name: "read_file", result: tools.ToolResult{Output: "file-result"}}
	toolB := &mockTool{name: "grep", result: tools.ToolResult{Output: "grep-result"}}

	reg := newRegistryWith(toolA, toolB)
	cfg := &RunLoopConfig{Tools: reg}

	calls := []backend.ToolCall{
		{ID: "first", Function: backend.ToolCallFunction{Name: "read_file", Arguments: map[string]any{}}},
		{ID: "second", Function: backend.ToolCallFunction{Name: "grep", Arguments: map[string]any{}}},
	}

	results := cfg.dispatchTools(context.Background(), calls)

	if results[0].tc.ID != "first" {
		t.Errorf("results[0].tc.ID = %q, want %q", results[0].tc.ID, "first")
	}
	if results[1].tc.ID != "second" {
		t.Errorf("results[1].tc.ID = %q, want %q", results[1].tc.ID, "second")
	}
	if results[0].content != "file-result" {
		t.Errorf("results[0].content = %q, want %q", results[0].content, "file-result")
	}
	if results[1].content != "grep-result" {
		t.Errorf("results[1].content = %q, want %q", results[1].content, "grep-result")
	}
}

func TestDispatchTools_OnBeforeWriteSerializedAcrossParallelWrites(t *testing.T) {
	var (
		mu            sync.Mutex
		concurrent    int
		maxConcurrent int
	)

	onBeforeWrite := func(path string, old, new []byte) bool {
		mu.Lock()
		concurrent++
		if concurrent > maxConcurrent {
			maxConcurrent = concurrent
		}
		mu.Unlock()

		time.Sleep(30 * time.Millisecond)

		mu.Lock()
		concurrent--
		mu.Unlock()
		return true
	}

	toolW := &mockTool{name: "write_file", result: tools.ToolResult{Output: "ok"}}
	reg := newRegistryWith(toolW)
	cfg := &RunLoopConfig{
		Tools:         reg,
		OnBeforeWrite: onBeforeWrite,
	}

	calls := []backend.ToolCall{
		{ID: "w1", Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{"file_path": "/a/one.go", "content": "x"}}},
		{ID: "w2", Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{"file_path": "/a/two.go", "content": "y"}}},
	}

	_ = cfg.dispatchTools(context.Background(), calls)

	mu.Lock()
	defer mu.Unlock()
	if maxConcurrent > 1 {
		t.Errorf("OnBeforeWrite was called concurrently: maxConcurrent=%d, want 1", maxConcurrent)
	}
}

func TestRunLoop_ParallelDispatchIntegration(t *testing.T) {
	toolA := &mockTool{name: "read_file", result: tools.ToolResult{Output: "a-out"}}
	toolB := &mockTool{name: "grep", result: tools.ToolResult{Output: "b-out"}}

	multiCall := &backend.ChatResponse{
		DoneReason: "tool_calls",
		ToolCalls: []backend.ToolCall{
			{ID: "c1", Function: backend.ToolCallFunction{Name: "read_file", Arguments: map[string]any{}}},
			{ID: "c2", Function: backend.ToolCallFunction{Name: "grep", Arguments: map[string]any{}}},
		},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{multiCall, stopResponse("done")},
	}
	reg := newRegistryWith(toolA, toolB)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "read and grep"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want stop", result.StopReason)
	}

	var foundA, foundB bool
	for _, msg := range result.Messages {
		if msg.Role == "tool" && msg.ToolCallID == "c1" && msg.Content == "a-out" {
			foundA = true
		}
		if msg.Role == "tool" && msg.ToolCallID == "c2" && msg.Content == "b-out" {
			foundB = true
		}
	}
	if !foundA {
		t.Error("missing tool result for c1 (read_file)")
	}
	if !foundB {
		t.Error("missing tool result for c2 (grep)")
	}
}
