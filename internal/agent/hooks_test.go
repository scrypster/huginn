package agent

// hooks_test.go — TDD coverage for the G10 PreToolUse/PostToolUse hook
// chain: a pre-hook can veto a tool call (result reflects denial, the tool
// itself never runs), a post-hook observes every completed call, and
// concurrent dispatch of many independent tools fires hooks race-safe.

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// TestPreToolUseHook_VetoesTool verifies a denying PreToolUse hook stops the
// tool from ever executing and produces an honest "<tool> blocked: <reason>"
// result instead of a raw error or panic.
func TestPreToolUseHook_VetoesTool(t *testing.T) {
	tool := &slowMockTool{name: "read_file"}
	reg := newRegistryWith(tool)

	hooks := NewHookRegistry()
	hooks.RegisterPreToolUse(func(_ context.Context, toolName string, _ map[string]any) (bool, string) {
		if toolName == "read_file" {
			return false, "test veto"
		}
		return true, ""
	})

	cfg := &RunLoopConfig{Tools: reg, Hooks: hooks}
	calls := []backend.ToolCall{
		{ID: "c1", Function: backend.ToolCallFunction{Name: "read_file", Arguments: map[string]any{}}},
	}

	results := cfg.dispatchTools(context.Background(), calls)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if got := results[0].content; got != "read_file blocked: test veto" {
		t.Errorf("content = %q, want %q", got, "read_file blocked: test veto")
	}
	if n := atomic.LoadInt32(&tool.callCount); n != 0 {
		t.Errorf("tool.Execute called %d times, want 0 (vetoed)", n)
	}
}

// TestPreToolUseHook_AllowLetsToolRun verifies an allowing hook is
// transparent — the tool runs normally.
func TestPreToolUseHook_AllowLetsToolRun(t *testing.T) {
	tool := &slowMockTool{name: "read_file"}
	reg := newRegistryWith(tool)

	hooks := NewHookRegistry()
	hooks.RegisterPreToolUse(func(_ context.Context, _ string, _ map[string]any) (bool, string) {
		return true, ""
	})

	cfg := &RunLoopConfig{Tools: reg, Hooks: hooks}
	calls := []backend.ToolCall{
		{ID: "c1", Function: backend.ToolCallFunction{Name: "read_file", Arguments: map[string]any{}}},
	}
	results := cfg.dispatchTools(context.Background(), calls)
	if results[0].content != "ok" {
		t.Errorf("content = %q, want %q", results[0].content, "ok")
	}
	if n := atomic.LoadInt32(&tool.callCount); n != 1 {
		t.Errorf("tool.Execute called %d times, want 1", n)
	}
}

// TestPostToolUseHook_ObservesEveryCompletedTool verifies the post hook
// fires for a successful call with the actual result content.
func TestPostToolUseHook_ObservesEveryCompletedTool(t *testing.T) {
	tool := &slowMockTool{name: "read_file"}
	reg := newRegistryWith(tool)

	var seenName string
	var seenOutput string
	var mu sync.Mutex
	hooks := NewHookRegistry()
	hooks.RegisterPostToolUse(func(_ context.Context, toolName string, _ map[string]any, result *tools.ToolResult) {
		mu.Lock()
		seenName = toolName
		seenOutput = result.Output
		mu.Unlock()
	})

	cfg := &RunLoopConfig{Tools: reg, Hooks: hooks}
	calls := []backend.ToolCall{
		{ID: "c1", Function: backend.ToolCallFunction{Name: "read_file", Arguments: map[string]any{}}},
	}
	cfg.dispatchTools(context.Background(), calls)

	mu.Lock()
	defer mu.Unlock()
	if seenName != "read_file" {
		t.Errorf("seenName = %q, want read_file", seenName)
	}
	if seenOutput != "ok" {
		t.Errorf("seenOutput = %q, want ok", seenOutput)
	}
}

// TestPostToolUseHook_CanAnnotateResult verifies the post hook may mutate
// the result it's handed (used by G1's warn mode) and that the mutation is
// visible in what reaches the model.
func TestPostToolUseHook_CanAnnotateResult(t *testing.T) {
	tool := &slowMockTool{name: "read_file"}
	reg := newRegistryWith(tool)

	hooks := NewHookRegistry()
	hooks.RegisterPostToolUse(func(_ context.Context, _ string, _ map[string]any, result *tools.ToolResult) {
		result.Output += "\n[annotation]"
	})

	cfg := &RunLoopConfig{Tools: reg, Hooks: hooks}
	calls := []backend.ToolCall{
		{ID: "c1", Function: backend.ToolCallFunction{Name: "read_file", Arguments: map[string]any{}}},
	}
	results := cfg.dispatchTools(context.Background(), calls)
	want := "ok\n[annotation]"
	if results[0].content != want {
		t.Errorf("content = %q, want %q", results[0].content, want)
	}
}

// TestHooks_NilRegistryIsNoop verifies a nil Hooks field on RunLoopConfig
// behaves exactly as before hooks existed.
func TestHooks_NilRegistryIsNoop(t *testing.T) {
	tool := &slowMockTool{name: "read_file"}
	reg := newRegistryWith(tool)
	cfg := &RunLoopConfig{Tools: reg} // Hooks left nil
	calls := []backend.ToolCall{
		{ID: "c1", Function: backend.ToolCallFunction{Name: "read_file", Arguments: map[string]any{}}},
	}
	results := cfg.dispatchTools(context.Background(), calls)
	if results[0].content != "ok" {
		t.Errorf("content = %q, want ok", results[0].content)
	}
}

// hookCountingTool is a minimal tools.Tool that records how many times Execute
// ran and always succeeds, used for the concurrency race test.
type hookCountingTool struct {
	name string
	n    int32
}

func (t *hookCountingTool) Name() string                      { return t.name }
func (t *hookCountingTool) Description() string               { return "" }
func (t *hookCountingTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *hookCountingTool) Schema() backend.Tool {
	return backend.Tool{Type: "function", Function: backend.ToolFunction{Name: t.name}}
}
func (t *hookCountingTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	atomic.AddInt32(&t.n, 1)
	return tools.ToolResult{Output: "ok:" + t.name}
}

// TestDispatchTools_HooksRaceSafeWithEightConcurrentTools fires 8
// independent tool calls through dispatchTools with both a pre and a post
// hook registered, and both hooks maintaining their own internal counters.
// Run with -race: any unguarded shared state in the hook chain or in the
// hooks themselves would be flagged.
func TestDispatchTools_HooksRaceSafeWithEightConcurrentTools(t *testing.T) {
	// isIndependentTool only classifies these specific read-only tool names
	// as independent (safe to run concurrently); use 8 distinct ones so
	// dispatchTools actually fans them out across goroutines instead of
	// running them serially.
	names := []string{
		"read_file", "grep", "list_dir", "search_files",
		"web_search", "fetch_url", "git_status", "git_log",
	}
	n := len(names)
	regTools := make([]tools.Tool, 0, n)
	toolByName := map[string]*hookCountingTool{}
	for _, name := range names {
		ct := &hookCountingTool{name: name}
		toolByName[name] = ct
		regTools = append(regTools, ct)
	}
	reg := newRegistryWith(regTools...)

	hooks := NewHookRegistry()
	var preCount int32
	var postCount int32
	var preMu sync.Mutex
	preSeen := map[string]bool{}
	hooks.RegisterPreToolUse(func(_ context.Context, toolName string, args map[string]any) (bool, string) {
		atomic.AddInt32(&preCount, 1)
		// Mutate our own copy to prove it doesn't leak back to the caller.
		args["mutated"] = true
		preMu.Lock()
		preSeen[toolName] = true
		preMu.Unlock()
		return true, ""
	})
	hooks.RegisterPostToolUse(func(_ context.Context, _ string, _ map[string]any, result *tools.ToolResult) {
		atomic.AddInt32(&postCount, 1)
		result.Output += ":post"
	})

	calls := make([]backend.ToolCall, 0, n)
	for i, name := range names {
		calls = append(calls, backend.ToolCall{
			ID:       fmt.Sprintf("c%d", i),
			Function: backend.ToolCallFunction{Name: name, Arguments: map[string]any{"i": i}},
		})
	}

	cfg := &RunLoopConfig{Tools: reg, Hooks: hooks}
	results := cfg.dispatchTools(context.Background(), calls)

	if len(results) != n {
		t.Fatalf("expected %d results, got %d", n, len(results))
	}
	if got := atomic.LoadInt32(&preCount); got != int32(n) {
		t.Errorf("preCount = %d, want %d", got, n)
	}
	if got := atomic.LoadInt32(&postCount); got != int32(n) {
		t.Errorf("postCount = %d, want %d", got, n)
	}
	for i, r := range results {
		want := fmt.Sprintf("ok:%s:post", names[i])
		if r.content != want {
			t.Errorf("results[%d].content = %q, want %q", i, r.content, want)
		}
	}
	for name, ct := range toolByName {
		if atomic.LoadInt32(&ct.n) != 1 {
			t.Errorf("tool %s executed %d times, want 1", name, ct.n)
		}
	}
	// The original call args must never be mutated by a hook's copy.
	for _, c := range calls {
		if _, ok := c.Function.Arguments["mutated"]; ok {
			t.Errorf("hook mutation leaked into original args for %s", c.Function.Name)
		}
	}
}
