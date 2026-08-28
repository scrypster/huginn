package agent

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// slowPrefetchTool simulates a real muninn_where_left_off MCP call that takes
// a while to answer, so tests can measure whether other work genuinely
// overlaps with it rather than merely not-crashing.
type slowPrefetchTool struct {
	name  string
	delay time.Duration
}

func (t *slowPrefetchTool) Name() string                      { return t.name }
func (t *slowPrefetchTool) Description() string               { return "" }
func (t *slowPrefetchTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *slowPrefetchTool) Schema() backend.Tool {
	return backend.Tool{Function: backend.ToolFunction{Name: t.name}}
}
func (t *slowPrefetchTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	time.Sleep(t.delay)
	return tools.ToolResult{Output: "recalled context"}
}

// TestStartVaultPrefetch_OverlapsWithCallerWork proves DEFECT B phase 2: the
// vault-connect + memory-prefetch phase (here stood in for by a deliberately
// slow muninn_where_left_off tool call) runs concurrently with whatever the
// caller does next (stood in for by contextBuilder.Build / loadAgentSummaries
// in production), instead of the two running back-to-back.
func TestStartVaultPrefetch_OverlapsWithCallerWork(t *testing.T) {
	const toolDelay = 150 * time.Millisecond
	const callerWork = 60 * time.Millisecond

	newReg := func() *tools.Registry {
		reg := tools.NewRegistry()
		reg.Register(&slowPrefetchTool{name: "muninn_where_left_off", delay: toolDelay})
		return reg
	}
	// MemoryEnabled: false makes connectAgentVault return immediately with a
	// fork of reg (skipping the real network connect, which is out of scope
	// here) — sessionReg still carries the registered muninn_where_left_off
	// tool, so prefetchMemoryContextWithEvents genuinely calls (and waits on)
	// our slow stub.
	ag := &agents.Agent{Name: "overlap-test-agent", MemoryEnabled: false}

	// Concurrent path: start the prefetch, do "caller work" while it runs in
	// the background, then join.
	oConcurrent := newTestOrchestrator()
	reg := newReg()
	start := time.Now()
	getResult := oConcurrent.startVaultPrefetch(context.Background(), ag, reg, "", "hi", nil)
	time.Sleep(callerWork) // stands in for contextBuilder.Build / loadAgentSummaries
	_ = getResult()
	concurrentElapsed := time.Since(start)

	// Sequential baseline: the old behavior — connect+prefetch, THEN caller work.
	oSequential := newTestOrchestrator()
	reg2 := newReg()
	start2 := time.Now()
	vr := oSequential.connectAgentVault(context.Background(), ag, reg2)
	oSequential.prefetchMemoryContextWithEvents(context.Background(), vr.sessionReg, ag.Name, ag.VaultName, "hi", nil)
	time.Sleep(callerWork)
	sequentialElapsed := time.Since(start2)

	t.Logf("concurrent=%v sequential=%v (toolDelay=%v callerWork=%v)", concurrentElapsed, sequentialElapsed, toolDelay, callerWork)

	// Sequential should take roughly toolDelay+callerWork; concurrent should
	// take roughly max(toolDelay, callerWork) = toolDelay. Assert concurrent
	// is meaningfully faster than sequential — proof the two phases overlap.
	if concurrentElapsed >= sequentialElapsed {
		t.Fatalf("expected concurrent prefetch (%v) to be faster than sequential (%v) — phases did not overlap", concurrentElapsed, sequentialElapsed)
	}
	// Concurrent elapsed should be well under the naive sum (toolDelay+callerWork).
	if concurrentElapsed >= toolDelay+callerWork {
		t.Fatalf("concurrent elapsed %v was not less than the naive serial sum %v", concurrentElapsed, toolDelay+callerWork)
	}
}

// TestStartVaultPrefetch_DoesNotBlockCaller verifies startVaultPrefetch
// itself returns immediately (the connect+prefetch work happens in a
// goroutine), independent of how slow that work turns out to be.
func TestStartVaultPrefetch_DoesNotBlockCaller(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&slowPrefetchTool{name: "muninn_where_left_off", delay: 300 * time.Millisecond})
	ag := &agents.Agent{Name: "nonblocking-test-agent", MemoryEnabled: false}
	o := newTestOrchestrator()

	start := time.Now()
	getResult := o.startVaultPrefetch(context.Background(), ag, reg, "", "hi", nil)
	elapsed := time.Since(start)
	if elapsed > 100*time.Millisecond {
		t.Fatalf("startVaultPrefetch blocked its caller for %v; expected near-instant return", elapsed)
	}

	outcome := getResult()
	if outcome.memAddendum == "" {
		t.Fatal("expected a non-empty memory addendum once the goroutine completes")
	}
}
