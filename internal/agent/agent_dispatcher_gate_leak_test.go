package agent

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/tools"
)

// TestChatWithAgent_DoesNotLeakForkedGateSweeper is a regression test for a
// goroutine leak: applyToolbelt (agent_dispatcher.go) forks the shared
// permission gate once per agent-dispatch turn via gate.Fork(...), and every
// fork starts its own background sweep goroutine
// (permissions.Gate.startSweep). Nothing closed those forked gates, so every
// dispatch permanently leaked one sweeper — the CI race-timeout goroutine
// dump was full of Fork.(*Gate).startSweep frames.
//
// This drives the real leak path end-to-end through ChatWithAgent (which
// calls runAgentTurn, which forks the gate via applyToolbelt) rather than
// re-implementing the fork/close pairing in the test, so it actually
// exercises whatever runAgentTurn does — or fails to do — with the fork.
func TestChatWithAgent_DoesNotLeakForkedGateSweeper(t *testing.T) {
	const iterations = 40

	mb := &mockBackend{responses: nil} // filled per-iteration below
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)

	// A non-nil, non-skipAll gate so applyToolbelt actually forks (see
	// agent_dispatcher.go: `if gate != nil { agentGate = gate.Fork(...) }`).
	// promptFunc auto-allows so the turn completes without blocking on a
	// permission prompt.
	gate := permissions.NewGate(false, func(req permissions.PermissionRequest) permissions.Decision {
		return permissions.AllowAll
	})
	defer gate.Close()
	o.SetTools(tools.NewRegistry(), gate)

	ag := &agents.Agent{
		Name:         "Steve",
		ModelID:      "test-model",
		SystemPrompt: "You are Steve.",
	}

	// Let the top-level gate's own sweep goroutine start and settle before
	// measuring.
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	before := runtime.NumGoroutine()

	for i := 0; i < iterations; i++ {
		mb.mu.Lock()
		mb.responses = []*backend.ChatResponse{stopResponse("done")}
		mb.mu.Unlock()
		if err := o.ChatWithAgent(context.Background(), ag, "hello", "", nil, nil, nil); err != nil {
			t.Fatalf("ChatWithAgent iteration %d: %v", i, err)
		}
	}

	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	after := runtime.NumGoroutine()

	// Small slack for unrelated runtime/test-harness goroutines. Before the
	// fix (no Close() on the per-turn forked gate), this delta grew by ~1
	// goroutine per ChatWithAgent call (40) — far beyond this slack.
	const slack = 8
	if after > before+slack {
		t.Errorf("possible forked-gate sweep goroutine leak across %d ChatWithAgent turns: before=%d after=%d (delta=%d > slack=%d)",
			iterations, before, after, after-before, slack)
	}
}
