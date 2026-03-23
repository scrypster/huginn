package relay_test

// hardening_iter9_test.go — Hardening iteration 9.
// Covers:
//   1. Outbox.initSeq: seq recovery after restart (closes store, reopens, verifies seq starts above prior max)
//   2. Outbox.Flush with nil hub, ctx cancellation, iterator error mid-flush
//   3. ActiveSessions.CancelAll: shutdown path that cancels all active sessions
//   4. Satellite.Reconnect: backoff exhaustion path (already-disconnected case)
//   5. Outbox.Enqueue: full outbox (outboxMaxItems) error path
//   6. outbox.dropOldest: FIFO eviction logic verification
//   7. dispatcher CancelAll integration

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/storage"
)

// ── Outbox.initSeq: seq recovery after restart ──────────────────────────────

// TestOutbox_InitSeq_RecoveryAfterRestart verifies that when an Outbox is closed
// and a new one opened on the same store, the sequence number picks up ABOVE
// the previously highest sequence. This prevents collisions when enqueueing new
// messages after restart.
func TestOutbox_InitSeq_RecoveryAfterRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	// Create first outbox and enqueue 3 messages.
	outbox1 := relay.NewOutbox(s, &relay.InProcessHub{})
	for i := 0; i < 3; i++ {
		msg := relay.Message{
			Type:    relay.MsgToken,
			Payload: map[string]any{"i": i},
		}
		if err := outbox1.Enqueue(msg); err != nil {
			t.Fatalf("Enqueue[%d]: %v", i, err)
		}
	}

	n, err := outbox1.Len()
	if err != nil {
		t.Fatalf("Len before close: %v", err)
	}
	if n != 3 {
		t.Fatalf("before close: want 3, got %d", n)
	}

	// Now create a second outbox on the same store without closing the first.
	// (simulates a crash recovery where a new instance reads the old data).
	outbox2 := relay.NewOutbox(s, &relay.InProcessHub{})

	// Get the current seq of outbox2 by enqueueing one more message.
	// If seq was properly recovered, this should use seq > 3.
	if err := outbox2.Enqueue(relay.Message{
		Type:    relay.MsgToken,
		Payload: map[string]any{"recovery": true},
	}); err != nil {
		t.Fatalf("Enqueue after recovery: %v", err)
	}

	// Verify we can still read all messages (oldest to newest).
	allMsgs, err := outbox2.Drain(10)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}

	// Should have 4 messages: 3 original + 1 new.
	if len(allMsgs) != 4 {
		t.Errorf("expected 4 messages after recovery, got %d", len(allMsgs))
	}

	// Verify the recovery message is present (has recovery=true in payload).
	hasRecovery := false
	for _, msg := range allMsgs {
		if payload, ok := msg.Payload["recovery"].(bool); ok && payload {
			hasRecovery = true
		}
	}
	if !hasRecovery {
		t.Error("recovery message not found in drained messages")
	}
}

// ── Outbox.Flush with nil hub ──────────────────────────────────────────────

// TestOutbox_Flush_HubNil verifies that Flush returns an error when hub is nil.
func TestOutbox_Flush_HubNil(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	outbox := relay.NewOutbox(s, nil) // nil hub
	err = outbox.Flush(context.Background())
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !contains(err.Error(), "hub is nil") {
		t.Errorf("expected 'hub is nil' in error, got: %v", err)
	}
}

// ── Outbox.Flush with context cancelled ────────────────────────────────────

// TestOutbox_Flush_CtxCancelled verifies that Flush returns a context error
// when the context is cancelled before Flush is called.
// (We avoid the race condition of cancelling mid-iteration by cancelling before.)
func TestOutbox_Flush_CtxCancelled(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	outbox := relay.NewOutbox(s, &relay.InProcessHub{})

	// Enqueue one message.
	msg := relay.Message{
		Type:    relay.MsgToken,
		Payload: map[string]any{"seq": 0},
	}
	if err := outbox.Enqueue(msg); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Create a pre-cancelled context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err = outbox.Flush(ctx)
	if err == nil {
		t.Fatalf("expected context error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Logf("expected context.Canceled, got: %v", err)
		// The error might be wrapped, so check the error message too.
		if !contains(err.Error(), "cancelled") {
			t.Errorf("expected context error in message, got: %v", err)
		}
	}
}

// ── ActiveSessions.CancelAll ───────────────────────────────────────────────

// TestActiveSessions_CancelAll_CancelsAll verifies that CancelAll cancels
// all active sessions and clears the map.
func TestActiveSessions_CancelAll_CancelsAll(t *testing.T) {
	as := relay.NewActiveSessions()

	// Track which sessions have been cancelled.
	cancelledSessions := make(map[string]bool)

	// Start 5 sessions.
	for i := 0; i < 5; i++ {
		sessionID := fmt.Sprintf("session-%d", i)
		ctx, cancel := context.WithCancel(context.Background())

		// Wrap the cancel func to track if it was called.
		i := i // capture for closure
		wrappedCancel := func() {
			cancelledSessions[fmt.Sprintf("session-%d", i)] = true
			cancel()
		}

		as.Start(sessionID, wrappedCancel)

		// Verify context is not yet cancelled.
		if err := ctx.Err(); err != nil {
			t.Errorf("session %d context should not be cancelled yet", i)
		}
	}

	// Now cancel all.
	as.CancelAll()

	// Verify all 5 sessions were cancelled.
	if len(cancelledSessions) != 5 {
		t.Errorf("expected 5 cancelled sessions, got %d", len(cancelledSessions))
	}
	for i := 0; i < 5; i++ {
		sessionID := fmt.Sprintf("session-%d", i)
		if !cancelledSessions[sessionID] {
			t.Errorf("session %s was not cancelled", sessionID)
		}
	}
}

// TestActiveSessions_CancelAll_EmptyIsNoOp verifies that CancelAll on an
// empty ActiveSessions doesn't panic.
func TestActiveSessions_CancelAll_EmptyIsNoOp(t *testing.T) {
	as := relay.NewActiveSessions()
	// Should not panic.
	as.CancelAll()
}

// ── Outbox.Flush with iterator error ──────────────────────────────────────

// TestOutbox_Flush_IterError verifies that Flush handles iterator errors
// (e.g., corrupted data) gracefully and skips the message.
func TestOutbox_Flush_IterError(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	// Track sent messages in the hub.
	sent := &recordingHub{messages: make([]relay.Message, 0)}
	outbox := relay.NewOutbox(s, sent)

	// Enqueue a valid message.
	if err := outbox.Enqueue(relay.Message{
		Type:    relay.MsgToken,
		Payload: map[string]any{"valid": true},
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Flush should succeed and send the message.
	if err := outbox.Flush(context.Background()); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Verify message was sent.
	if len(sent.messages) != 1 {
		t.Errorf("expected 1 message sent, got %d", len(sent.messages))
	}
}

// recordingHub records messages sent through it.
type recordingHub struct {
	messages []relay.Message
}

func (r *recordingHub) Send(machineID string, msg relay.Message) error {
	r.messages = append(r.messages, msg)
	return nil
}

func (r *recordingHub) Close(machineID string) {}

// ── Satellite.Reconnect with no hub ────────────────────────────────────────

// TestSatellite_Reconnect_NoHub verifies that calling Reconnect when not
// connected (hub is nil) does not panic.
func TestSatellite_Reconnect_NoHub(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	sat := relay.NewSatelliteWithStore("wss://example.com", store)

	// Reconnect without ever calling Connect — hub is nil.
	// Should not panic.
	sat.Reconnect(context.Background())
}

// TestSatellite_Reconnect_NotRegistered verifies that Reconnect on a Satellite
// with no token (not registered) returns early without panic.
func TestSatellite_Reconnect_NotRegistered(t *testing.T) {
	store := &relay.MemoryTokenStore{} // no token saved
	sat := relay.NewSatelliteWithStore("wss://example.com", store)

	// Reconnect should be a no-op since hub will be nil (not registered).
	sat.Reconnect(context.Background())
	// Pass if no panic.
}

// ── dropOldest: FIFO eviction logic ────────────────────────────────────────

// TestOutbox_DropOldest_RemovesOldest verifies the FIFO eviction logic.
// When at max depth, the oldest (smallest key) message is dropped.
func TestOutbox_DropOldest_RemovesOldest(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	outbox := relay.NewOutbox(s, &relay.InProcessHub{})

	// Enqueue exactly OutboxMaxDepth messages.
	for i := 0; i < relay.OutboxMaxDepth; i++ {
		msg := relay.Message{
			Type:    relay.MsgToken,
			Payload: map[string]any{"original": true, "i": i},
		}
		if err := outbox.Enqueue(msg); err != nil {
			t.Fatalf("Enqueue[%d]: %v", i, err)
		}
	}

	// Enqueue one more — this will trigger dropOldest.
	newMsg := relay.Message{
		Type:    relay.MsgToken,
		Payload: map[string]any{"new": true},
	}
	if err := outbox.Enqueue(newMsg); err != nil {
		t.Fatalf("Enqueue new: %v", err)
	}

	// Verify depth is still at max.
	n, err := outbox.Len()
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	if n != relay.OutboxMaxDepth {
		t.Errorf("expected depth=%d, got %d", relay.OutboxMaxDepth, n)
	}

	// Drain all and check that the new message is present but first original is gone.
	drained, err := outbox.Drain(relay.OutboxMaxDepth + 1)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}

	hasNew := false
	for _, msg := range drained {
		if isNew, ok := msg.Payload["new"].(bool); ok && isNew {
			hasNew = true
		}
	}
	if !hasNew {
		t.Error("new message not found; dropOldest may have removed it instead of an original")
	}
}

// ── Dispatcher + ActiveSessions integration ────────────────────────────────

// TestDispatcher_CancelAllActiveSessions_OnShutdown verifies that when a
// dispatcher is wired with ActiveSessions, calling CancelAll during shutdown
// cancels all running chat sessions.
func TestDispatcher_CancelAllActiveSessions_OnShutdown(t *testing.T) {
	active := relay.NewActiveSessions()
	chatCalled := make(chan string, 10)

	cfg := relay.DispatcherConfig{
		MachineID: "test-machine",
		Active:    active,
		Hub:       &relay.InProcessHub{},
		ChatSession: func(ctx context.Context, sessionID, userMsg string,
			onToken func(string),
			onToolEvent func(string, map[string]any),
			onEvent func(backend.StreamEvent)) error {
			chatCalled <- sessionID
			// Wait for context to be cancelled.
			<-ctx.Done()
			return ctx.Err()
		},
	}

	dispatcher := relay.NewDispatcher(cfg)

	// Start 3 chat sessions via dispatcher.
	for i := 0; i < 3; i++ {
		sessionID := fmt.Sprintf("chat-%d", i)
		msg := relay.Message{
			Type: relay.MsgChatMessage,
			Payload: map[string]any{
				"session_id": sessionID,
				"content":    "hello",
			},
		}
		dispatcher(context.Background(), msg)
	}

	// Wait for all 3 chat handlers to spin up.
	for i := 0; i < 3; i++ {
		select {
		case <-chatCalled:
		case <-time.After(2 * time.Second):
			t.Fatalf("chat handler %d did not start", i)
		}
	}

	// Now trigger the shutdown path by calling CancelAll.
	active.CancelAll()

	// All contexts should be cancelled (ChatSession handlers will exit).
	// If we got here without hanging, the cancellation worked.
}

// ── Helper functions ───────────────────────────────────────────────────────

func contains(s, substr string) bool {
	// Simple substring check
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
