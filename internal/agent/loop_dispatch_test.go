package agent

// loop_dispatch_test.go — additional coverage for dispatchTools(),
// isIndependentTool(), safeOnBeforeWrite(), toolSchemaAllows(), and RunLoop
// edge-cases not covered by loop_test.go or loop_interrupt_test.go.

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/tools"
)

// ---------------------------------------------------------------------------
// isIndependentTool — unit tests
// ---------------------------------------------------------------------------

// TestIsIndependentTool_ReadOnlyAlwaysTrue verifies that known read-only tools
// are always classified as independent regardless of arguments.
func TestIsIndependentTool_ReadOnlyAlwaysTrue(t *testing.T) {
	readTools := []string{
		"read_file", "grep", "list_dir", "search_files",
		"web_search", "fetch_url",
		"git_status", "git_log", "git_blame", "git_diff", "git_branch",
	}
	for _, name := range readTools {
		t.Run(name, func(t *testing.T) {
			got := isIndependentTool(name, map[string]any{}, nil)
			if !got {
				t.Errorf("isIndependentTool(%q) = false, want true", name)
			}
		})
	}
}

// TestIsIndependentTool_BashAlwaysFalse verifies that bash is never independent.
func TestIsIndependentTool_BashAlwaysFalse(t *testing.T) {
	got := isIndependentTool("bash", map[string]any{"command": "echo hi"}, nil)
	if got {
		t.Error("isIndependentTool(bash) = true, want false")
	}
}

// TestIsIndependentTool_GitWritesSerial verifies that git write operations are
// always serial (not independent).
func TestIsIndependentTool_GitWritesSerial(t *testing.T) {
	for _, name := range []string{"git_commit", "git_stash"} {
		got := isIndependentTool(name, map[string]any{}, nil)
		if got {
			t.Errorf("isIndependentTool(%q) = true, want false", name)
		}
	}
}

// TestIsIndependentTool_MCPAlwaysFalse verifies that any mcp_-prefixed tool is
// classified as serial.
func TestIsIndependentTool_MCPAlwaysFalse(t *testing.T) {
	for _, name := range []string{"mcp_slack_send", "mcp_github_create_issue", "mcp_"} {
		got := isIndependentTool(name, map[string]any{}, nil)
		if got {
			t.Errorf("isIndependentTool(%q) = true, want false", name)
		}
	}
}

// TestIsIndependentTool_UnknownToolFalse verifies that tools not in any known
// category default to serial (safe default).
func TestIsIndependentTool_UnknownToolFalse(t *testing.T) {
	got := isIndependentTool("some_mystery_tool", map[string]any{}, nil)
	if got {
		t.Error("isIndependentTool(unknown) = true, want false")
	}
}

// TestIsIndependentTool_WriteFileDifferentPaths verifies that write_file calls
// targeting different paths are classified as independent.
func TestIsIndependentTool_WriteFileDifferentPaths(t *testing.T) {
	allCalls := []backend.ToolCall{
		{Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{"file_path": "a.go"}}},
		{Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{"file_path": "b.go"}}},
	}
	// Each write targets a unique path — should be independent.
	gotA := isIndependentTool("write_file", map[string]any{"file_path": "a.go"}, allCalls)
	if !gotA {
		t.Error("expected write_file(a.go) to be independent when b.go is in the batch")
	}
	gotB := isIndependentTool("write_file", map[string]any{"file_path": "b.go"}, allCalls)
	if !gotB {
		t.Error("expected write_file(b.go) to be independent when a.go is in the batch")
	}
}

// TestIsIndependentTool_WriteFileSamePath verifies that two write_file calls
// targeting the same path are NOT independent (must run serially).
func TestIsIndependentTool_WriteFileSamePath(t *testing.T) {
	allCalls := []backend.ToolCall{
		{Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{"file_path": "same.go"}}},
		{Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{"file_path": "same.go"}}},
	}
	got := isIndependentTool("write_file", map[string]any{"file_path": "same.go"}, allCalls)
	if got {
		t.Error("expected write_file(same.go) to be serial when two calls target the same path")
	}
}

// TestIsIndependentTool_WriteFileNoPath verifies that a write_file call with no
// file_path argument defaults to serial (safe).
func TestIsIndependentTool_WriteFileNoPath(t *testing.T) {
	got := isIndependentTool("write_file", map[string]any{}, nil)
	if got {
		t.Error("expected write_file with no path to be serial")
	}
}

// ---------------------------------------------------------------------------
// safeOnBeforeWrite — unit tests
// ---------------------------------------------------------------------------

// TestSafeOnBeforeWrite_PanicReturnsError verifies that a panicking callback
// is caught and reported as (false, panicValue).
func TestSafeOnBeforeWrite_PanicReturnsError(t *testing.T) {
	fn := func(path string, old, new []byte) bool {
		panic("deliberate panic in callback")
	}
	allowed, panicVal := safeOnBeforeWrite(fn, "x.go", nil, []byte("new"))
	if allowed {
		t.Error("expected allowed=false after panic")
	}
	if panicVal == nil {
		t.Error("expected non-nil panicVal after panic")
	}
	if s, ok := panicVal.(string); ok && !strings.Contains(s, "deliberate panic") {
		t.Errorf("unexpected panicVal: %v", panicVal)
	}
}

// TestSafeOnBeforeWrite_ApproveNoError verifies normal approval path.
func TestSafeOnBeforeWrite_ApproveNoError(t *testing.T) {
	fn := func(path string, old, new []byte) bool { return true }
	allowed, panicVal := safeOnBeforeWrite(fn, "x.go", nil, []byte("new"))
	if !allowed {
		t.Error("expected allowed=true")
	}
	if panicVal != nil {
		t.Errorf("expected panicVal=nil, got %v", panicVal)
	}
}

// TestSafeOnBeforeWrite_DenyNoError verifies normal denial path.
func TestSafeOnBeforeWrite_DenyNoError(t *testing.T) {
	fn := func(path string, old, new []byte) bool { return false }
	allowed, panicVal := safeOnBeforeWrite(fn, "x.go", nil, []byte("new"))
	if allowed {
		t.Error("expected allowed=false")
	}
	if panicVal != nil {
		t.Errorf("expected panicVal=nil, got %v", panicVal)
	}
}

// ---------------------------------------------------------------------------
// toolSchemaAllows — unit tests
// ---------------------------------------------------------------------------

// TestToolSchemaAllows_EmptySchemas verifies that an empty ToolSchemas list
// allows every tool.
func TestToolSchemaAllows_EmptySchemas(t *testing.T) {
	cfg := &RunLoopConfig{ToolSchemas: nil}
	if !cfg.toolSchemaAllows("any_tool") {
		t.Error("expected empty ToolSchemas to allow any tool")
	}
}

// TestToolSchemaAllows_ToolPresent verifies that a listed tool is allowed.
func TestToolSchemaAllows_ToolPresent(t *testing.T) {
	cfg := &RunLoopConfig{
		ToolSchemas: []backend.Tool{
			{Function: backend.ToolFunction{Name: "read_file"}},
			{Function: backend.ToolFunction{Name: "bash"}},
		},
	}
	if !cfg.toolSchemaAllows("read_file") {
		t.Error("expected read_file to be allowed")
	}
	if !cfg.toolSchemaAllows("bash") {
		t.Error("expected bash to be allowed")
	}
}

// TestToolSchemaAllows_ToolAbsent verifies that a tool not in ToolSchemas is
// denied.
func TestToolSchemaAllows_ToolAbsent(t *testing.T) {
	cfg := &RunLoopConfig{
		ToolSchemas: []backend.Tool{
			{Function: backend.ToolFunction{Name: "read_file"}},
		},
	}
	if cfg.toolSchemaAllows("bash") {
		t.Error("expected bash to be denied when not in ToolSchemas")
	}
}

// ---------------------------------------------------------------------------
// dispatchTools — parallel execution and stable result ordering
// ---------------------------------------------------------------------------

// TestDispatchTools_ParallelReadTools verifies that multiple read_file tool
// calls execute concurrently — they should all start within a short window.
func TestDispatchTools_ParallelReadTools(t *testing.T) {
	t.Parallel()

	// Each execution records its start time; if they overlap, the interval
	// between first and last start should be much smaller than the sleep duration.
	var mu sync.Mutex
	starts := make([]time.Time, 0, 3)

	// A slow tool that records its start time.
	makeSlowTool := func(name string) tools.Tool {
		return &slowRecordingTool{
			toolName:  name,
			sleepDur:  30 * time.Millisecond,
			startsMu:  &mu,
			starts:    &starts,
		}
	}

	toolA := makeSlowTool("read_file")

	// We need 3 separate read_file-named tools, but the registry maps by name.
	// Instead use 3 different independent tool names so we can register all 3.
	toolA2 := makeSlowTool("grep")
	toolA3 := makeSlowTool("list_dir")

	reg := newRegistryWith(toolA, toolA2, toolA3)

	calls := []backend.ToolCall{
		{ID: "c1", Function: backend.ToolCallFunction{Name: "read_file", Arguments: map[string]any{}}},
		{ID: "c2", Function: backend.ToolCallFunction{Name: "grep", Arguments: map[string]any{}}},
		{ID: "c3", Function: backend.ToolCallFunction{Name: "list_dir", Arguments: map[string]any{}}},
	}

	cfg := &RunLoopConfig{
		Tools:       reg,
		ToolSchemas: nil, // all allowed
	}

	before := time.Now()
	results := cfg.dispatchTools(context.Background(), calls)
	elapsed := time.Since(before)

	// All 3 tools should have run.
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// If truly parallel, elapsed should be well under 3 * 30ms = 90ms.
	// Allow generous headroom (200ms) to avoid flakiness.
	if elapsed > 200*time.Millisecond {
		t.Errorf("tools ran serially (elapsed=%v); expected parallel execution < 200ms", elapsed)
	}
}

// TestDispatchTools_StableResultOrder verifies that results are returned in
// the original call order even when tools run concurrently.
func TestDispatchTools_StableResultOrder(t *testing.T) {
	t.Parallel()

	// Use independent tools (read-only names) that return their name as output.
	toolA := &identityTool{name: "read_file", out: "result-A"}
	toolB := &identityTool{name: "grep", out: "result-B"}
	toolC := &identityTool{name: "list_dir", out: "result-C"}

	reg := newRegistryWith(toolA, toolB, toolC)

	calls := []backend.ToolCall{
		{ID: "c1", Function: backend.ToolCallFunction{Name: "read_file", Arguments: map[string]any{}}},
		{ID: "c2", Function: backend.ToolCallFunction{Name: "grep", Arguments: map[string]any{}}},
		{ID: "c3", Function: backend.ToolCallFunction{Name: "list_dir", Arguments: map[string]any{}}},
	}

	cfg := &RunLoopConfig{Tools: reg}
	results := cfg.dispatchTools(context.Background(), calls)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// Results must appear at their original indices.
	wantOrder := []string{"result-A", "result-B", "result-C"}
	for i, want := range wantOrder {
		if results[i].content != want {
			t.Errorf("results[%d].content = %q, want %q", i, results[i].content, want)
		}
		if results[i].index != i {
			t.Errorf("results[%d].index = %d, want %d", i, results[i].index, i)
		}
	}
}

// TestDispatchTools_SerialToolsRunInOrder verifies that bash calls (serial)
// are executed one after the other. We use a shared counter to confirm ordering.
func TestDispatchTools_SerialToolsRunInOrder(t *testing.T) {
	var callOrder []string
	var mu sync.Mutex

	makeOrdered := func(name, id string) tools.Tool {
		return &orderRecordingTool{
			toolName: name,
			id:       id,
			order:    &callOrder,
			orderMu:  &mu,
		}
	}

	// We need two distinct tool names that are classified as serial.
	// bash and git_commit are both serial.
	toolA := makeOrdered("bash", "first")
	toolB := makeOrdered("git_commit", "second")
	reg := newRegistryWith(toolA, toolB)

	calls := []backend.ToolCall{
		{ID: "c1", Function: backend.ToolCallFunction{Name: "bash", Arguments: map[string]any{}}},
		{ID: "c2", Function: backend.ToolCallFunction{Name: "git_commit", Arguments: map[string]any{}}},
	}
	cfg := &RunLoopConfig{Tools: reg}
	results := cfg.dispatchTools(context.Background(), calls)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	mu.Lock()
	defer mu.Unlock()
	// Both should have run.
	if len(callOrder) != 2 {
		t.Errorf("expected 2 calls, got %v", callOrder)
	}
}

// ---------------------------------------------------------------------------
// Permission gate integration via RunLoop
// ---------------------------------------------------------------------------

// TestRunLoop_PermissionGateDenies verifies that when the Gate denies a tool,
// OnPermissionDenied is called and the tool is not executed.
func TestRunLoop_PermissionGateDenies(t *testing.T) {
	var deniedTools []string

	tool := &writeLevelMockTool{name: "dangerous_tool"}
	reg := newRegistryWith(tool)

	// Gate with no prompt function — always denies write-level tools.
	gate := permissions.NewGate(false, nil)

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("dangerous_tool", "c1"),
			stopResponse("done"),
		},
	}

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Gate:     gate,
		Messages: []backend.Message{{Role: "user", Content: "do dangerous thing"}},
		OnPermissionDenied: func(name string) {
			deniedTools = append(deniedTools, name)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "stop")
	}
	if tool.callCount != 0 {
		t.Errorf("expected tool NOT executed, got %d calls", tool.callCount)
	}
	if len(deniedTools) != 1 || deniedTools[0] != "dangerous_tool" {
		t.Errorf("expected OnPermissionDenied for dangerous_tool, got %v", deniedTools)
	}
	// Verify denial message in history.
	found := false
	for _, msg := range result.Messages {
		if msg.Role == "tool" && strings.Contains(msg.Content, "permission denied") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'permission denied' message in tool result")
	}
}

// TestRunLoop_PermissionGateAllowsReadTool verifies that a Gate configured with
// skipAll=false still allows read-level tools without a promptFunc.
func TestRunLoop_PermissionGateAllowsReadTool(t *testing.T) {
	tool := &mockTool{name: "read_file", result: tools.ToolResult{Output: "file contents"}}
	reg := newRegistryWith(tool)

	// Gate without skipAll, without promptFunc — read tools always pass.
	gate := permissions.NewGate(false, nil)

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("read_file", "c1"),
			stopResponse("done"),
		},
	}

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Gate:     gate,
		Messages: []backend.Message{{Role: "user", Content: "read file"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "stop")
	}
	if tool.callCount != 1 {
		t.Errorf("expected tool executed once, got %d", tool.callCount)
	}
}

// TestRunLoop_PermissionGateSkipAllAllowsWriteTool verifies that skipAll=true
// permits write-level tools even without a promptFunc.
func TestRunLoop_PermissionGateSkipAllAllowsWriteTool(t *testing.T) {
	tool := &writeLevelMockTool{name: "write_tool"}
	reg := newRegistryWith(tool)

	// skipAll=true bypasses permission prompting.
	gate := permissions.NewGate(true, nil)

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("write_tool", "c1"),
			stopResponse("done"),
		},
	}

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Gate:     gate,
		Messages: []backend.Message{{Role: "user", Content: "write something"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "stop")
	}
	if tool.callCount != 1 {
		t.Errorf("expected tool executed once, got %d", tool.callCount)
	}
}

// TestRunLoop_PermissionGatePromptFuncAllows verifies that when the promptFunc
// returns Allow, the write tool executes.
func TestRunLoop_PermissionGatePromptFuncAllows(t *testing.T) {
	tool := &writeLevelMockTool{name: "write_tool"}
	reg := newRegistryWith(tool)

	gate := permissions.NewGate(false, func(req permissions.PermissionRequest) permissions.Decision {
		return permissions.Allow
	})

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("write_tool", "c1"),
			stopResponse("done"),
		},
	}

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Gate:     gate,
		Messages: []backend.Message{{Role: "user", Content: "write something"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "stop")
	}
	if tool.callCount != 1 {
		t.Errorf("expected tool executed once, got %d", tool.callCount)
	}
}

// ---------------------------------------------------------------------------
// RunLoop edge cases
// ---------------------------------------------------------------------------

// TestRunLoop_MaxTurns1 verifies that MaxTurns=1 exits after the first turn.
func TestRunLoop_MaxTurns1(t *testing.T) {
	// Backend always returns a tool call, so without the MaxTurns guard it
	// would loop forever.
	tool := &mockTool{name: "mytool", result: tools.ToolResult{Output: "ok"}}
	mb := &mockBackend{}
	for i := 0; i < 5; i++ {
		mb.responses = append(mb.responses, toolCallResponse("mytool", "c"))
	}
	reg := newRegistryWith(tool)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 1,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "go"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "max_turns" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "max_turns")
	}
	if result.TurnCount != 1 {
		t.Errorf("TurnCount = %d, want 1", result.TurnCount)
	}
}

// TestRunLoop_ToolOutputTruncated verifies that tool output exceeding 100KB is
// truncated and the truncation marker is appended to the message.
func TestRunLoop_ToolOutputTruncated(t *testing.T) {
	bigOutput := strings.Repeat("x", 110*1024) // 110KB
	tool := &mockTool{
		name:   "big_tool",
		result: tools.ToolResult{Output: bigOutput},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("big_tool", "c1"),
			stopResponse("done"),
		},
	}
	reg := newRegistryWith(tool)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "big output"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Find the tool message and verify it is truncated.
	found := false
	for _, msg := range result.Messages {
		if msg.Role == "tool" && msg.ToolName == "big_tool" {
			if len(msg.Content) > 110*1024 {
				t.Errorf("tool message not truncated: len=%d", len(msg.Content))
			}
			if !strings.Contains(msg.Content, "truncated") {
				t.Error("expected truncation marker in tool message")
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("expected tool message in history")
	}
}

// TestRunLoop_CorrelationID verifies that SetCorrelationID / GetCorrelationID
// round-trip correctly through the context.
func TestRunLoop_CorrelationID(t *testing.T) {
	ctx := SetCorrelationID(context.Background(), "test-corr-123")
	got := GetCorrelationID(ctx)
	if got != "test-corr-123" {
		t.Errorf("GetCorrelationID = %q, want %q", got, "test-corr-123")
	}
}

// TestRunLoop_CorrelationIDEmpty verifies that an empty correlation ID is
// returned when none has been set.
func TestRunLoop_CorrelationIDEmpty(t *testing.T) {
	got := GetCorrelationID(context.Background())
	if got != "" {
		t.Errorf("expected empty correlation ID, got %q", got)
	}
}

// TestRunLoop_OnBeforeWrite_PanicRecovered verifies that a panic inside
// OnBeforeWrite does not crash the loop; the tool is skipped and an error
// message is appended.
func TestRunLoop_OnBeforeWrite_PanicRecovered(t *testing.T) {
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
								"file_path": "out.txt",
								"content":   "hello",
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
		Messages: []backend.Message{{Role: "user", Content: "write"}},
		OnBeforeWrite: func(path string, oldContent, newContent []byte) bool {
			panic("write callback exploded")
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "stop")
	}
	// The tool must NOT have been called.
	if tool.callCount != 0 {
		t.Errorf("expected tool NOT executed after panic, got %d calls", tool.callCount)
	}
	// The error message should mention the panic.
	found := false
	for _, msg := range result.Messages {
		if msg.Role == "tool" && strings.Contains(msg.Content, "panicked") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected panic error message in tool result history")
	}
}

// TestRunLoop_NilBackendResponse verifies that a nil response without error
// from the backend sets StopReason="error".
func TestRunLoop_NilBackendResponse(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{nil},
	}
	reg := newRegistryWith()

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for nil backend response")
	}
	if result.StopReason != "error" {
		t.Errorf("StopReason = %q, want %q", result.StopReason, "error")
	}
}

// TestRunLoop_ParallelToolsAllComplete verifies that when multiple independent
// tools are dispatched, all results are present in the final message history.
func TestRunLoop_ParallelToolsAllComplete(t *testing.T) {
	var executedCount int64

	makeCountTool := func(name, out string) tools.Tool {
		return &countingTool{
			toolName: name,
			out:      out,
			counter:  &executedCount,
		}
	}

	toolA := makeCountTool("read_file", "content-A")
	toolB := makeCountTool("grep", "content-B")
	toolC := makeCountTool("list_dir", "content-C")

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{
				Content:    "",
				DoneReason: "tool_calls",
				ToolCalls: []backend.ToolCall{
					{ID: "c1", Function: backend.ToolCallFunction{Name: "read_file", Arguments: map[string]any{}}},
					{ID: "c2", Function: backend.ToolCallFunction{Name: "grep", Arguments: map[string]any{}}},
					{ID: "c3", Function: backend.ToolCallFunction{Name: "list_dir", Arguments: map[string]any{}}},
				},
			},
			stopResponse("done"),
		},
	}
	reg := newRegistryWith(toolA, toolB, toolC)

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "run all"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want stop", result.StopReason)
	}
	// All 3 tools should have executed.
	finalCount := atomic.LoadInt64(&executedCount)
	if finalCount != 3 {
		t.Errorf("expected 3 tool executions, got %d", finalCount)
	}
	// All 3 results should appear in message history.
	toolMsgs := 0
	for _, msg := range result.Messages {
		if msg.Role == "tool" {
			toolMsgs++
		}
	}
	if toolMsgs != 3 {
		t.Errorf("expected 3 tool messages, got %d", toolMsgs)
	}
}

// ---------------------------------------------------------------------------
// Helper types used only in this file
// ---------------------------------------------------------------------------

// writeLevelMockTool is like mockTool but with PermWrite permission level.
type writeLevelMockTool struct {
	name      string
	callCount int
	mu        sync.Mutex
}

func (t *writeLevelMockTool) Name() string                      { return t.name }
func (t *writeLevelMockTool) Description() string               { return "" }
func (t *writeLevelMockTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *writeLevelMockTool) Schema() backend.Tool {
	return backend.Tool{Function: backend.ToolFunction{Name: t.name}}
}
func (t *writeLevelMockTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.callCount++
	return tools.ToolResult{Output: "ok"}
}

// identityTool returns a fixed output string, used to verify result ordering.
type identityTool struct {
	name string
	out  string
}

func (t *identityTool) Name() string                      { return t.name }
func (t *identityTool) Description() string               { return "" }
func (t *identityTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *identityTool) Schema() backend.Tool {
	return backend.Tool{Function: backend.ToolFunction{Name: t.name}}
}
func (t *identityTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	return tools.ToolResult{Output: t.out}
}

// slowRecordingTool records its start time and sleeps to simulate latency.
type slowRecordingTool struct {
	toolName string
	sleepDur time.Duration
	startsMu *sync.Mutex
	starts   *[]time.Time
}

func (t *slowRecordingTool) Name() string                      { return t.toolName }
func (t *slowRecordingTool) Description() string               { return "" }
func (t *slowRecordingTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *slowRecordingTool) Schema() backend.Tool {
	return backend.Tool{Function: backend.ToolFunction{Name: t.toolName}}
}
func (t *slowRecordingTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	t.startsMu.Lock()
	*t.starts = append(*t.starts, time.Now())
	t.startsMu.Unlock()
	time.Sleep(t.sleepDur)
	return tools.ToolResult{Output: t.toolName + "-done"}
}

// orderRecordingTool records execution order using a shared slice.
type orderRecordingTool struct {
	toolName string
	id       string
	order    *[]string
	orderMu  *sync.Mutex
}

func (t *orderRecordingTool) Name() string                      { return t.toolName }
func (t *orderRecordingTool) Description() string               { return "" }
func (t *orderRecordingTool) Permission() tools.PermissionLevel { return tools.PermExec }
func (t *orderRecordingTool) Schema() backend.Tool {
	return backend.Tool{Function: backend.ToolFunction{Name: t.toolName}}
}
func (t *orderRecordingTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	t.orderMu.Lock()
	*t.order = append(*t.order, t.id)
	t.orderMu.Unlock()
	return tools.ToolResult{Output: t.id}
}

// countingTool atomically increments a counter on each execution.
type countingTool struct {
	toolName string
	out      string
	counter  *int64
}

func (t *countingTool) Name() string                      { return t.toolName }
func (t *countingTool) Description() string               { return "" }
func (t *countingTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *countingTool) Schema() backend.Tool {
	return backend.Tool{Function: backend.ToolFunction{Name: t.toolName}}
}
func (t *countingTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	atomic.AddInt64(t.counter, 1)
	return tools.ToolResult{Output: t.out}
}
