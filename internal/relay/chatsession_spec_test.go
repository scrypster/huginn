package relay_test

// chatsession_spec_test.go — Behavior specs for the dispatcher's chat_message handling.
//
// Run with: go test -race ./internal/relay/...
//
// These tests verify the invariants documented in dispatcher.go:
// - Context cancellation reaches the ChatSession callback (no goroutine leak)
// - Active session is cleaned up after ChatSession returns
// - A blocking ChatSession is unblocked when its context is cancelled

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/relay"
)

// discardHub discards all messages — we only care about goroutine cleanup.
type discardHub struct{}

func (h *discardHub) Send(_ string, _ relay.Message) error { return nil }
func (h *discardHub) Close(_ string)                       {}

// TestDispatcher_ChatSession_ContextCancellationUnblocksCallback verifies that
// when the dispatcher context is cancelled, a blocking ChatSession callback
// receives context cancellation and the goroutine does NOT leak.
//
// This is the goroutine-leak safety invariant: every chat_message goroutine must
// eventually exit when the session context is cancelled.
func TestDispatcher_ChatSession_ContextCancellationUnblocksCallback(t *testing.T) {
	active := relay.NewActiveSessions()
	var callbackStarted, callbackExited sync.WaitGroup
	callbackStarted.Add(1)
	callbackExited.Add(1)

	dispatch := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "test-machine",
		Hub:       &discardHub{},
		Active:    active,
		ChatSession: func(ctx context.Context, sessionID, userMsg string,
			onToken func(string),
			onToolEvent func(string, map[string]any),
			onEvent func(backend.StreamEvent)) error {

			callbackStarted.Done() // signal: callback is running
			// Block until context is cancelled (simulates a slow LLM call).
			<-ctx.Done()
			callbackExited.Done() // signal: callback noticed cancellation
			return ctx.Err()
		},
	})

	ctx, cancel := context.WithCancel(context.Background())

	dispatch(ctx, relay.Message{
		Type: relay.MsgChatMessage,
		Payload: map[string]any{
			"session_id": "sess-goroutine-test",
			"content":    "hello",
		},
	})

	// Wait for the callback to start so we know the goroutine is running.
	waitDone(t, &callbackStarted, 500*time.Millisecond, "ChatSession callback never started")

	// Cancel the context — this should unblock the blocking ChatSession.
	cancel()

	// The callback must exit within a reasonable deadline.
	waitDone(t, &callbackExited, 500*time.Millisecond, "ChatSession goroutine did not exit after context cancellation")
}

// TestDispatcher_ChatSession_ActiveSession_CleanedUpAfterReturn verifies that
// after ChatSession returns (success or error), the session is removed from
// ActiveSessions, so the active-session count returns to zero.
//
// This guarantees that long-running sessions don't accumulate in the tracker.
func TestDispatcher_ChatSession_ActiveSession_CleanedUpAfterReturn(t *testing.T) {
	active := relay.NewActiveSessions()

	dispatch := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "test-machine",
		Hub:       &discardHub{},
		Active:    active,
		ChatSession: func(ctx context.Context, sessionID, userMsg string,
			onToken func(string),
			onToolEvent func(string, map[string]any),
			onEvent func(backend.StreamEvent)) error {
			// Fast return — session is "done" immediately.
			return nil
		},
	})

	dispatch(context.Background(), relay.Message{
		Type: relay.MsgChatMessage,
		Payload: map[string]any{
			"session_id": "sess-cleanup-test",
			"content":    "ping",
		},
	})

	// Poll until the session is gone from active tracker (goroutine has cleaned up).
	// We probe with Cancel(): returns false when the session is no longer registered.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !active.Cancel("sess-cleanup-test") {
			return // Correct: active session was removed
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Error("active session not removed after ChatSession returned (goroutine leak suspected)")
}

// TestDispatcher_ChatSession_Nil_IsNoOp verifies that a chat_message with no
// ChatSession wired is a no-op (no panic, no crash).
func TestDispatcher_ChatSession_Nil_IsNoOp(t *testing.T) {
	dispatch := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID:   "test-machine",
		Hub:         &discardHub{},
		ChatSession: nil, // intentionally not wired
	})

	// Must not panic.
	dispatch(context.Background(), relay.Message{
		Type: relay.MsgChatMessage,
		Payload: map[string]any{
			"session_id": "sess-nil",
			"content":    "test",
		},
	})
}

// TestDispatcher_ChatSession_MissingFields_IsNoOp verifies that a chat_message
// with missing session_id or content fields is ignored gracefully.
func TestDispatcher_ChatSession_MissingFields_IsNoOp(t *testing.T) {
	var called atomic.Bool
	dispatch := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "test-machine",
		Hub:       &discardHub{},
		ChatSession: func(ctx context.Context, sessionID, userMsg string,
			onToken func(string),
			onToolEvent func(string, map[string]any),
			onEvent func(backend.StreamEvent)) error {
			called.Store(true)
			return nil
		},
	})

	// Missing session_id
	dispatch(context.Background(), relay.Message{
		Type:    relay.MsgChatMessage,
		Payload: map[string]any{"content": "hello"},
	})
	// Missing content
	dispatch(context.Background(), relay.Message{
		Type:    relay.MsgChatMessage,
		Payload: map[string]any{"session_id": "sess-1"},
	})

	time.Sleep(20 * time.Millisecond)
	if called.Load() {
		t.Error("ChatSession should NOT be called when session_id or content is missing")
	}
}

// waitDone waits for a WaitGroup to reach zero within deadline, else calls t.Error.
func waitDone(t *testing.T, wg *sync.WaitGroup, d time.Duration, msg string) {
	t.Helper()
	ch := make(chan struct{})
	go func() {
		wg.Wait()
		close(ch)
	}()
	select {
	case <-ch:
	case <-time.After(d):
		t.Error(msg)
	}
}
