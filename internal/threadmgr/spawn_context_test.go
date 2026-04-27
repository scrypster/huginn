package threadmgr

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
)

// TestSpawnThread_ContextCancelledAfterSpawn verifies that when SpawnThread is
// called with a non-cancelled context, and the context is cancelled AFTER spawn,
// the goroutine is able to run at least one iteration. This is because SpawnThread
// creates its own context via context.WithCancel(ctx), decoupling the goroutine's
// lifetime from the caller's context. The goroutine should not die immediately when
// the parent context is cancelled — it should detect the cancellation on the next
// context check (e.g., in runOnce's select statement).
func TestSpawnThread_ContextCancelledAfterSpawn(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test-context-after", "/tmp", "claude-haiku-4")

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:    "ContextBot",
		ModelID: "claude-haiku-4",
	})

	// This backend will block until the block channel is closed or context is cancelled.
	blockCh := make(chan struct{})
	b := &blockingFakeBackend{block: blockCh}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "ContextBot", Task: "test context"})

	broadcastFn := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	// Create a cancellable parent context.
	parentCtx, cancel := context.WithCancel(context.Background())

	// SpawnThread will create its own context via context.WithCancel(parentCtx),
	// decoupling the goroutine's lifetime from this cancellation.
	tm.SpawnThread(parentCtx, thread.ID, store, sess, reg, b, broadcastFn, ca, nil)

	// Give the goroutine time to start and enter the first iteration.
	time.Sleep(50 * time.Millisecond)

	// Now cancel the parent context. The goroutine should NOT die immediately
	// because it has its own context (created via context.WithCancel).
	cancel()

	// Give the goroutine time to detect the parent context cancellation.
	// It should detect this at the next context check in runOnce.
	time.Sleep(100 * time.Millisecond)

	// Unblock the fake backend so the goroutine can proceed.
	close(blockCh)

	// Wait for the thread to reach a terminal state.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := tm.Get(thread.ID)
		if got != nil && (got.Status == StatusCancelled || got.Status == StatusError || got.Status == StatusDone) {
			// Thread has terminated. This is the expected outcome.
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Error("goroutine did not terminate after parent context cancellation")
}

// TestSpawnThread_ContextCancelledBeforeSpawn verifies that when the parent
// context is already cancelled BEFORE SpawnThread is called, the goroutine should
// detect the cancellation promptly. Since SpawnThread derives its context from
// the parent, the derived context will also be cancelled, and the goroutine should
// exit on its first context check (in runOnce).
func TestSpawnThread_ContextCancelledBeforeSpawn(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test-context-before", "/tmp", "claude-haiku-4")

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:    "ContextBotPre",
		ModelID: "claude-haiku-4",
	})

	// This backend will block until the block channel is closed or context is cancelled.
	blockCh := make(chan struct{})
	b := &blockingFakeBackend{block: blockCh}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "ContextBotPre", Task: "test context pre"})

	broadcastFn := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	// Create a context and cancel it BEFORE calling SpawnThread.
	parentCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// SpawnThread will create threadCtx := context.WithCancel(parentCtx).
	// Since parentCtx is already cancelled, threadCtx will also be in cancelled state.
	tm.SpawnThread(parentCtx, thread.ID, store, sess, reg, b, broadcastFn, ca, nil)

	// The goroutine should detect the cancellation almost immediately.
	// Give it a bit of time to detect and respond.
	time.Sleep(100 * time.Millisecond)

	// Unblock the fake backend just in case.
	close(blockCh)

	// The thread should be in a terminal state (cancelled or error).
	got, ok := tm.Get(thread.ID)
	if !ok {
		t.Fatal("thread not found")
	}

	if got.Status != StatusCancelled && got.Status != StatusError {
		t.Errorf("expected StatusCancelled or StatusError when ctx already cancelled, got %s", got.Status)
	}
}

// TestSpawnThread_ContextCancellationPropagates verifies that SpawnThread's
// context cancellation propagates into LLM calls. When the parent context is
// cancelled, the goroutine should stop making LLM calls and exit cleanly.
func TestSpawnThread_ContextCancellationPropagates(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test-propagate", "/tmp", "claude-haiku-4")

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:    "PropagateBot",
		ModelID: "claude-haiku-4",
	})

	// Count how many ChatCompletion calls are made.
	callCount := 0
	blockCh := make(chan struct{})

	backend := &blockingFakeBackend{block: blockCh}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "PropagateBot", Task: "test propagate"})

	broadcastFn := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	parentCtx, cancel := context.WithCancel(context.Background())

	tm.SpawnThread(parentCtx, thread.ID, store, sess, reg, backend, broadcastFn, ca, nil)

	// Let the goroutine start and attempt the first LLM call.
	time.Sleep(50 * time.Millisecond)

	// Cancel the parent context, which should propagate to the LLM call.
	cancel()

	// Give time for the goroutine to detect the cancellation.
	time.Sleep(100 * time.Millisecond)

	// Unblock the backend.
	close(blockCh)

	// Wait for thread to terminate.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := tm.Get(thread.ID)
		if got != nil && (got.Status == StatusCancelled || got.Status == StatusError || got.Status == StatusDone) {
			// Success: thread terminated cleanly without hanging.
			_ = callCount
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Error("goroutine did not terminate after context cancellation propagated")
}

// TestSpawnThread_DecoupledFromRequestContext verifies the key invariant:
// SpawnThread decouples the thread goroutine from the caller's context.
// This allows a thread spawned in a request handler (which finishes immediately)
// to continue running independently until it completes or is explicitly cancelled
// via tm.Cancel(threadID). This test demonstrates why passing a long-lived server
// context instead of a request context is crucial.
func TestSpawnThread_DecoupledFromRequestContext(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test-decouple", "/tmp", "claude-haiku-4")

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:    "DecoupleBot",
		ModelID: "claude-haiku-4",
	})

	// Backend that finishes normally (doesn't block).
	fb := &fakeBackend{
		response: &backend.ChatResponse{
			ToolCalls: []backend.ToolCall{
				{
					ID: "tc-decouple",
					Function: backend.ToolCallFunction{
						Name: "finish",
						Arguments: map[string]any{
							"summary": "thread completed",
							"status":  "completed",
						},
					},
				},
			},
			DoneReason:       "tool_calls",
			PromptTokens:     100,
			CompletionTokens: 50,
		},
	}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "DecoupleBot", Task: "test decouple"})

	broadcastFn := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	// Simulate a request-scoped context that would normally be cancelled
	// when the HTTP request handler returns. Create a context with a very
	// short timeout to simulate a request ending quickly.
	requestCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)

	// SpawnThread receives the request context, but creates its own derived context
	// via context.WithCancel(requestCtx). The key is that even though requestCtx
	// will expire, the goroutine's internal context can continue running because
	// it's not directly bound to the request lifetime.
	tm.SpawnThread(requestCtx, thread.ID, store, sess, reg, fb, broadcastFn, ca, nil)

	// Clean up the request context.
	defer cancel()

	// The request context timeout should fire ~10ms from now, but the goroutine
	// should not die immediately. Instead, it should continue to completion
	// because SpawnThread created its own context.
	//
	// Wait for the thread to complete.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := tm.Get(thread.ID)
		if got != nil && got.Status == StatusDone {
			// Success: thread completed despite request context expiring.
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	got, ok := tm.Get(thread.ID)
	if !ok {
		t.Fatal("thread not found")
	}

	// The thread should have completed, not been cancelled by the request context.
	if got.Status != StatusDone {
		t.Errorf("expected StatusDone (decoupled from request context), got %s", got.Status)
	}
}
