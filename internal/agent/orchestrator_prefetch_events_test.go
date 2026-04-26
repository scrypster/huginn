package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/stats"
	"github.com/scrypster/huginn/internal/tools"
)

// muninnFixtureTool implements tools.Tool for prefetch-event tests.
type muninnFixtureTool struct {
	name   string
	output string
}

func (t *muninnFixtureTool) Name() string                      { return t.name }
func (t *muninnFixtureTool) Description() string               { return "fixture muninn tool" }
func (t *muninnFixtureTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *muninnFixtureTool) Schema() backend.Tool {
	return backend.Tool{Function: backend.ToolFunction{Name: t.name}}
}
func (t *muninnFixtureTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	return tools.ToolResult{Output: t.output}
}

// orchForPrefetch creates a minimal orchestrator suitable for prefetch tests.
func orchForPrefetch(t *testing.T) *Orchestrator {
	t.Helper()
	o, err := NewOrchestrator(obsBackendNoTools{}, modelconfig.DefaultModels(), nil, nil, stats.NoopCollector{}, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	return o
}

// TestPrefetchMemoryContextWithEvents_FiresCallbackOnFreshFetch verifies that
// when the where_left_off and recall caches are cold, the callback fires once
// for each tool with cached=false. This is the path that surfaces synthetic
// tool_call/tool_result events to the UI on a fresh chat turn.
func TestPrefetchMemoryContextWithEvents_FiresCallbackOnFreshFetch(t *testing.T) {
	t.Parallel()

	o := orchForPrefetch(t)
	reg := tools.NewRegistry()
	reg.Register(&muninnFixtureTool{name: "muninn_where_left_off", output: "you were investigating X"})
	reg.Register(&muninnFixtureTool{name: "muninn_recall", output: "memory: foo bar baz"})

	type call struct {
		tool   string
		cached bool
		output string
	}
	var (
		mu    sync.Mutex
		calls []call
	)
	cb := func(toolName string, _ map[string]any, output string, cached bool) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, call{tool: toolName, cached: cached, output: output})
	}

	got := o.prefetchMemoryContextWithEvents(context.Background(), reg, "agentA", "vaultA", "tell me about X", cb)
	if got == "" {
		t.Fatal("expected non-empty memory context block")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("expected 2 callback invocations (where_left_off + recall), got %d: %+v", len(calls), calls)
	}
	if calls[0].tool != "muninn_where_left_off" {
		t.Errorf("expected first callback for muninn_where_left_off, got %q", calls[0].tool)
	}
	if calls[0].cached {
		t.Error("expected where_left_off callback cached=false on first call")
	}
	if calls[1].tool != "muninn_recall" {
		t.Errorf("expected second callback for muninn_recall, got %q", calls[1].tool)
	}
	if calls[1].cached {
		t.Error("expected recall callback cached=false on first call")
	}
}

// TestPrefetchMemoryContextWithEvents_CachedSecondCall verifies that a second
// invocation with the same agent+vault+message marks the callback as cached.
// The UI uses cached=true to suppress duplicate tool events on every turn.
func TestPrefetchMemoryContextWithEvents_CachedSecondCall(t *testing.T) {
	t.Parallel()

	o := orchForPrefetch(t)
	reg := tools.NewRegistry()
	reg.Register(&muninnFixtureTool{name: "muninn_where_left_off", output: "context block"})
	reg.Register(&muninnFixtureTool{name: "muninn_recall", output: "memory hit"})

	var firstCalls []bool
	cb1 := func(_ string, _ map[string]any, _ string, cached bool) {
		firstCalls = append(firstCalls, cached)
	}
	_ = o.prefetchMemoryContextWithEvents(context.Background(), reg, "agentB", "vaultB", "msg-1", cb1)
	if len(firstCalls) != 2 {
		t.Fatalf("first call: expected 2 callbacks, got %d", len(firstCalls))
	}
	for i, c := range firstCalls {
		if c {
			t.Errorf("first call: callback %d had cached=true, expected false", i)
		}
	}

	var secondCalls []bool
	cb2 := func(_ string, _ map[string]any, _ string, cached bool) {
		secondCalls = append(secondCalls, cached)
	}
	_ = o.prefetchMemoryContextWithEvents(context.Background(), reg, "agentB", "vaultB", "msg-1", cb2)
	if len(secondCalls) != 2 {
		t.Fatalf("second call: expected 2 callbacks, got %d", len(secondCalls))
	}
	for i, c := range secondCalls {
		if !c {
			t.Errorf("second call: callback %d had cached=false, expected true (cache hit)", i)
		}
	}
}

// TestPrefetchMemoryContextWithEvents_NilCallback verifies that passing a nil
// callback is harmless — the function still returns the memory block and the
// caches still warm normally. Documents the supported nil-tolerance contract.
func TestPrefetchMemoryContextWithEvents_NilCallback(t *testing.T) {
	t.Parallel()

	o := orchForPrefetch(t)
	reg := tools.NewRegistry()
	reg.Register(&muninnFixtureTool{name: "muninn_where_left_off", output: "block"})
	reg.Register(&muninnFixtureTool{name: "muninn_recall", output: "recall"})

	got := o.prefetchMemoryContextWithEvents(context.Background(), reg, "agentC", "vaultC", "hello", nil)
	if got == "" {
		t.Fatal("expected non-empty memory context with nil callback")
	}
}

// TestPrefetchMemoryContextWithEvents_NoMuninnTool verifies that when the
// where_left_off tool is unavailable in the registry (vault never connected),
// the function returns "" and the callback is never invoked.
func TestPrefetchMemoryContextWithEvents_NoMuninnTool(t *testing.T) {
	t.Parallel()

	o := orchForPrefetch(t)
	reg := tools.NewRegistry() // empty — no muninn_* tools

	var fired bool
	cb := func(string, map[string]any, string, bool) { fired = true }

	got := o.prefetchMemoryContextWithEvents(context.Background(), reg, "agentD", "vaultD", "anything", cb)
	if got != "" {
		t.Errorf("expected empty result when muninn_where_left_off missing, got %q", got)
	}
	if fired {
		t.Error("expected callback NOT to fire when no muninn tools are registered")
	}
}
