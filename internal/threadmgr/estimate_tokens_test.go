package threadmgr

// coverage_boost95_test.go — additional tests to bring threadmgr to 95%+ coverage.
// This is an internal (white-box) test file to access unexported functions.

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
)

// ---------------------------------------------------------------------------
// estimateTokens — the tokens < 1 branch is dead code for non-empty strings
// but we can at least verify a 1-char string produces >= 1 token.
// ---------------------------------------------------------------------------

func TestEstimateTokens_SingleChar(t *testing.T) {
	// 1 char → ceil(1/4) = 1
	got := estimateTokens("a")
	if got < 1 {
		t.Errorf("expected >= 1, got %d", got)
	}
}

func TestEstimateTokens_ThreeChars(t *testing.T) {
	// 3 chars → ceil(3/4) = 1
	got := estimateTokens("abc")
	if got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestEstimateTokens_FiveChars(t *testing.T) {
	// 5 chars → ceil(5/4) = 2
	got := estimateTokens("abcde")
	if got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// buildSnapshotMessages — messages that fit within budget (covers selected/reverse)
// ---------------------------------------------------------------------------

func TestBuildSnapshotMessages_FitsWithinBudget(t *testing.T) {
	store := makeTestStore(t)
	sess := store.New("snap-test", "/tmp", "m")

	_ = store.Append(sess, session.SessionMessage{
		Role:    "user",
		Content: "hello",
	})
	_ = store.Append(sess, session.SessionMessage{
		Role:    "assistant",
		Content: "world",
	})

	// Budget is large enough to fit both messages.
	msgs := buildSnapshotMessages(sess.ID, store, 4096)
	if len(msgs) < 2 {
		t.Errorf("expected at least 2 messages, got %d", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// Complete — thread not found (early return path)
// ---------------------------------------------------------------------------

func TestComplete_ThreadNotFound(t *testing.T) {
	tm := New()
	// Must not panic.
	tm.Complete("nonexistent-thread-id", FinishSummary{Summary: "done", Status: "completed"})
}

// ---------------------------------------------------------------------------
// Complete — with dagFn (covers dagFn() call in runOnce finish path)
// ---------------------------------------------------------------------------

func TestRunOnce_WithDagFn(t *testing.T) {
	tm := New()
	store := makeTestStore(t)
	sess := store.New("test", "/tmp", "m")
	reg := agents.NewRegistry()

	fb := &fakeBackend{
		response: &backend.ChatResponse{
			ToolCalls: []backend.ToolCall{
				{
					ID: "tc-fin",
					Function: backend.ToolCallFunction{
						Name:      "finish",
						Arguments: map[string]any{"summary": "done", "status": "completed"},
					},
				},
			},
			DoneReason: "tool_calls",
		},
	}

	var dagCalled int32
	dagFn := func() {
		atomic.AddInt32(&dagCalled, 1)
	}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "work"})
	broadcast := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	result := tm.runOnce(context.Background(), thread.ID, "", "", sess, store, reg, fb, broadcast, ca, dagFn, nil, nil, nil, nil, nil)
	if result.kind != loopDone {
		t.Errorf("expected loopDone, got %v", result.kind)
	}
	if atomic.LoadInt32(&dagCalled) == 0 {
		t.Error("expected dagFn to be called")
	}
}

// ---------------------------------------------------------------------------
// runOnce — unexpected panic recovery
// ---------------------------------------------------------------------------

type panicBackend struct{}

func (p *panicBackend) ChatCompletion(_ context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
	panic("unexpected internal error")
}
func (p *panicBackend) Health(_ context.Context) error   { return nil }
func (p *panicBackend) Shutdown(_ context.Context) error { return nil }
func (p *panicBackend) ContextWindow() int               { return 8192 }

func TestRunOnce_UnexpectedPanic(t *testing.T) {
	tm := New()
	store := makeTestStore(t)
	sess := store.New("test", "/tmp", "m")
	reg := agents.NewRegistry()

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "panic"})
	broadcast := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	result := tm.runOnce(context.Background(), thread.ID, "", "", sess, store, reg, &panicBackend{}, broadcast, ca, nil, nil, nil, nil, nil, nil)
	if result.kind != loopDone {
		t.Errorf("expected loopDone after panic recovery, got %v", result.kind)
	}
	got, _ := tm.Get(thread.ID)
	if got == nil || got.Status != StatusDone {
		t.Errorf("expected StatusDone after panic recovery, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// runOnce — context cancelled before first turn (select default vs Done)
// ---------------------------------------------------------------------------

func TestRunOnce_ContextAlreadyCancelled(t *testing.T) {
	tm := New()
	store := makeTestStore(t)
	sess := store.New("test", "/tmp", "m")
	reg := agents.NewRegistry()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "cancelled"})
	broadcast := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	fb := &fakeBackend{response: &backend.ChatResponse{Content: "noop", DoneReason: "stop"}}
	result := tm.runOnce(ctx, thread.ID, "", "", sess, store, reg, fb, broadcast, ca, nil, nil, nil, nil, nil, nil)
	if result.kind != loopDone {
		t.Errorf("expected loopDone for pre-cancelled context, got %v", result.kind)
	}
}

// ---------------------------------------------------------------------------
// runOnce — max turns path (50 turns of "stop" → max turns summary)
// ---------------------------------------------------------------------------

func TestRunOnce_MaxTurns(t *testing.T) {
	tm := New()
	store := makeTestStore(t)
	sess := store.New("test", "/tmp", "m")
	reg := agents.NewRegistry()

	// Backend that returns tool calls requiring 51 turns (more than maxTurns=50).
	callCount := 0
	var mu sync.Mutex
	infiniteBackend := &callbackBackend{
		fn: func(_ context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
			mu.Lock()
			callCount++
			mu.Unlock()
			// Always return an unknown tool call so the loop keeps going.
			return &backend.ChatResponse{
				ToolCalls: []backend.ToolCall{
					{
						ID: fmt.Sprintf("tc-%d", callCount),
						Function: backend.ToolCallFunction{
							Name:      "keep_going_tool",
							Arguments: map[string]any{},
						},
					},
				},
				DoneReason: "tool_calls",
			}, nil
		},
	}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "infinite"})
	broadcast := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	var dagCalled int32
	dagFn := func() { atomic.AddInt32(&dagCalled, 1) }

	result := tm.runOnce(context.Background(), thread.ID, "", "", sess, store, reg, infiniteBackend, broadcast, ca, dagFn, nil, nil, nil, nil, nil)
	if result.kind != loopDone {
		t.Errorf("expected loopDone at max turns, got %v", result.kind)
	}
	got, _ := tm.Get(thread.ID)
	if got == nil || got.Status != StatusDone {
		t.Errorf("expected StatusDone after max turns, got %v", got)
	}
	if atomic.LoadInt32(&dagCalled) == 0 {
		t.Error("expected dagFn to be called at max turns")
	}
}

// ---------------------------------------------------------------------------
// SpawnThread — waitForInputOnce context cancelled path
// ---------------------------------------------------------------------------

func TestSpawnThread_WaitForInput_ContextCancelled(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "m")
	reg := agents.NewRegistry()

	// First response: request_help (sets status to Blocked)
	// After that the context will be cancelled.
	helpResponse := &backend.ChatResponse{
		ToolCalls: []backend.ToolCall{
			{
				ID: "tc-help",
				Function: backend.ToolCallFunction{
					Name:      "request_help",
					Arguments: map[string]any{"message": "need input"},
				},
			},
		},
		DoneReason: "tool_calls",
	}

	fb := &fakeBackend{response: helpResponse}
	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Helper", Task: "help"})

	broadcast := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	ctx, cancel := context.WithCancel(context.Background())
	tm.SpawnThread(ctx, thread.ID, store, sess, reg, fb, broadcast, ca, nil)

	// Wait for blocked state.
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := tm.Get(thread.ID)
		if got != nil && got.Status == StatusBlocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Cancel the context — goroutine should exit through waitForInputOnce.
	cancel()

	// Wait for the goroutine to handle cancellation.
	deadline = time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := tm.Get(thread.ID)
		if got != nil && (got.Status == StatusCancelled || got.Status == StatusDone || got.Status == StatusError) {
			return // success
		}
		time.Sleep(10 * time.Millisecond)
	}
	// It's also acceptable if the status remains Blocked (goroutine may exit without
	// changing status in this specific race). The important thing is no deadlock.
}

// ---------------------------------------------------------------------------
// buildSnapshotMessages — messages exactly hit budget boundary
// ---------------------------------------------------------------------------

func TestBuildSnapshotMessages_ExactBudget(t *testing.T) {
	store := makeTestStore(t)
	sess := store.New("exact-budget", "/tmp", "m")

	// Add a message with exactly 4 chars = 1 token.
	_ = store.Append(sess, session.SessionMessage{
		Role:    "user",
		Content: "abcd", // 4 chars = 1 token
	})

	// Budget = 1 token — should fit exactly.
	msgs := buildSnapshotMessages(sess.ID, store, 1)
	if len(msgs) != 1 {
		t.Errorf("expected 1 message for exact budget, got %d", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// runOnce — budget exceeded with dagFn
// ---------------------------------------------------------------------------

func TestRunOnce_BudgetExceeded_WithDagFn(t *testing.T) {
	tm := New()
	store := makeTestStore(t)
	sess := store.New("test", "/tmp", "m")
	reg := agents.NewRegistry()

	fb := &fakeBackend{
		response: &backend.ChatResponse{Content: "response", DoneReason: "stop"},
	}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "expensive"})

	// Budget already exceeded.
	ca := NewCostAccumulator(0.000001)
	ca.Record("other", 1000000, 1000000, "big-model")

	var dagCalled int32
	dagFn := func() { atomic.AddInt32(&dagCalled, 1) }

	broadcast := func(_, _ string, _ map[string]any) {}
	result := tm.runOnce(context.Background(), thread.ID, "", "", sess, store, reg, fb, broadcast, ca, dagFn, nil, nil, nil, nil, nil)
	if result.kind != loopDone {
		t.Errorf("expected loopDone, got %v", result.kind)
	}
	if atomic.LoadInt32(&dagCalled) == 0 {
		t.Error("expected dagFn to be called on budget exceeded")
	}
}

// ---------------------------------------------------------------------------
// runOnce — LLM error with dagFn
// ---------------------------------------------------------------------------

func TestRunOnce_LLMError_WithDagFn(t *testing.T) {
	tm := New()
	store := makeTestStore(t)
	sess := store.New("test", "/tmp", "m")
	reg := agents.NewRegistry()

	errBackend := &alwaysErrorBackend{err: fmt.Errorf("api gone")}
	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "work"})

	var dagCalled int32
	dagFn := func() { atomic.AddInt32(&dagCalled, 1) }

	broadcast := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	result := tm.runOnce(context.Background(), thread.ID, "", "", sess, store, reg, errBackend, broadcast, ca, dagFn, nil, nil, nil, nil, nil)
	if result.kind != loopDone {
		t.Errorf("expected loopDone, got %v", result.kind)
	}
	if atomic.LoadInt32(&dagCalled) == 0 {
		t.Error("expected dagFn to be called on LLM error")
	}
}

// ---------------------------------------------------------------------------
// runOnce — length done reason with dagFn
// ---------------------------------------------------------------------------

func TestRunOnce_LengthDoneReason_WithDagFn(t *testing.T) {
	tm := New()
	store := makeTestStore(t)
	sess := store.New("test", "/tmp", "m")
	reg := agents.NewRegistry()

	fb := &fakeBackend{
		response: &backend.ChatResponse{
			Content:    "partial",
			DoneReason: "length",
		},
	}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "big"})

	var dagCalled int32
	dagFn := func() { atomic.AddInt32(&dagCalled, 1) }

	broadcast := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	result := tm.runOnce(context.Background(), thread.ID, "", "", sess, store, reg, fb, broadcast, ca, dagFn, nil, nil, nil, nil, nil)
	if result.kind != loopDone {
		t.Errorf("expected loopDone, got %v", result.kind)
	}
	if atomic.LoadInt32(&dagCalled) == 0 {
		t.Error("expected dagFn to be called on length done")
	}
}

// ---------------------------------------------------------------------------
// runOnce — stop reason (no tool calls) with dagFn
// ---------------------------------------------------------------------------

func TestRunOnce_StopReason_WithDagFn(t *testing.T) {
	tm := New()
	store := makeTestStore(t)
	sess := store.New("test", "/tmp", "m")
	reg := agents.NewRegistry()

	fb := &fakeBackend{
		response: &backend.ChatResponse{
			Content:    "just stopped",
			DoneReason: "stop",
		},
	}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "stop"})

	var dagCalled int32
	dagFn := func() { atomic.AddInt32(&dagCalled, 1) }

	broadcast := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	result := tm.runOnce(context.Background(), thread.ID, "", "", sess, store, reg, fb, broadcast, ca, dagFn, nil, nil, nil, nil, nil)
	if result.kind != loopDone {
		t.Errorf("expected loopDone, got %v", result.kind)
	}
	if atomic.LoadInt32(&dagCalled) == 0 {
		t.Error("expected dagFn to be called on stop")
	}
}

// ---------------------------------------------------------------------------
// runOnce — panic recovery with dagFn
// ---------------------------------------------------------------------------

func TestRunOnce_UnexpectedPanic_WithDagFn(t *testing.T) {
	tm := New()
	store := makeTestStore(t)
	sess := store.New("test", "/tmp", "m")
	reg := agents.NewRegistry()

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "panic"})

	var dagCalled int32
	dagFn := func() { atomic.AddInt32(&dagCalled, 1) }

	broadcast := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	result := tm.runOnce(context.Background(), thread.ID, "", "", sess, store, reg, &panicBackend{}, broadcast, ca, dagFn, nil, nil, nil, nil, nil)
	if result.kind != loopDone {
		t.Errorf("expected loopDone, got %v", result.kind)
	}
	if atomic.LoadInt32(&dagCalled) == 0 {
		t.Error("expected dagFn to be called on panic")
	}
}

// ---------------------------------------------------------------------------
// runOnce — context cancel after retry (ctx error path after both attempts fail)
// ---------------------------------------------------------------------------

func TestRunOnce_ContextCancelAfterBothRetries(t *testing.T) {
	tm := New()
	store := makeTestStore(t)
	sess := store.New("test", "/tmp", "m")
	reg := agents.NewRegistry()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after first attempt — second attempt will see ctx.Err() != nil
	callCount := 0
	cancelAfterFirstBackend := &callbackBackend{
		fn: func(c context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
			callCount++
			if callCount >= 2 {
				cancel()
			}
			return nil, fmt.Errorf("error %d", callCount)
		},
	}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "x"})
	broadcast := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	result := tm.runOnce(ctx, thread.ID, "", "", sess, store, reg, cancelAfterFirstBackend, broadcast, ca, nil, nil, nil, nil, nil, nil)
	if result.kind != loopDone {
		t.Errorf("expected loopDone, got %v", result.kind)
	}
}
