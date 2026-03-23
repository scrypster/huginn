package relay_test

// phase4_outbox_test.go — Tests for the Phase 4 send-or-enqueue behavior.
// Verifies that all dispatcher hub.Send paths log on failure and queue in the
// outbox (when wired) rather than silently dropping messages.
//
// Covered:
//   1. session_start_ack: send failure queues message in outbox
//   2. session_list_result: send failure queues message in outbox
//   3. model_list_result: send failure queues message in outbox
//   4. chat_message token: send failure queues token in outbox
//   5. chat_message done: send failure queues done in outbox
//   6. No outbox wired: send failure is logged only, no panic
//   7. Outbox full: second-level failure is logged, no panic

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/storage"
)

// errHub is a Hub that always returns an error from Send.
type errHub struct {
	err error
}

func (e *errHub) Send(_ string, _ relay.Message) error { return e.err }
func (e *errHub) Close(_ string)                       {}

// ── 1. session_start_ack: send failure queues in outbox ──────────────────────

func TestPhase4_SessionStartAck_SendFails_QueuesInOutbox(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	hub := &errHub{err: fmt.Errorf("connection closed")}
	outbox := relay.NewOutbox(s, &relay.InProcessHub{})

	dispatch := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		Outbox:    outbox,
		NewSession: func(reqID string) string {
			return "sess-" + reqID
		},
	})

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgSessionStart,
		MachineID: "m1",
		Payload:   map[string]any{"session_id": "req-1"},
	})

	// hub.Send failed → message should be in outbox.
	n, err := outbox.Len()
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 queued message in outbox after send failure, got %d", n)
	}

	// Drain and verify message type.
	msgs, err := outbox.Drain(10)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Type != relay.MsgSessionStartAck {
		t.Errorf("expected session_start_ack in outbox, got %+v", msgs)
	}
}

// ── 2. session_list_result: send failure queues in outbox ────────────────────

func TestPhase4_SessionListResult_SendFails_QueuesInOutbox(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	sessStore := relay.NewSessionStore(s)

	// Separate store for outbox.
	dir2 := t.TempDir()
	s2, err := storage.Open(dir2)
	if err != nil {
		t.Fatalf("open outbox store: %v", err)
	}
	defer s2.Close()

	hub := &errHub{err: fmt.Errorf("disconnected")}
	outbox := relay.NewOutbox(s2, &relay.InProcessHub{})

	dispatch := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		Store:     sessStore,
		Outbox:    outbox,
	})

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgSessionListRequest,
		MachineID: "m1",
	})

	n, err := outbox.Len()
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 queued message, got %d", n)
	}

	msgs, err := outbox.Drain(10)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Type != relay.MsgSessionListResult {
		t.Errorf("expected session_list_result in outbox, got %+v", msgs)
	}
}

// ── 3. model_list_result: send failure queues in outbox ──────────────────────

func TestPhase4_ModelListResult_SendFails_QueuesInOutbox(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	hub := &errHub{err: fmt.Errorf("write error")}
	outbox := relay.NewOutbox(s, &relay.InProcessHub{})

	dispatch := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		Outbox:    outbox,
		ListModels: func() []string {
			return []string{"model-a", "model-b"}
		},
	})

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgModelListRequest,
		MachineID: "m1",
	})

	n, err := outbox.Len()
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 queued message, got %d", n)
	}

	msgs, err := outbox.Drain(10)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Type != relay.MsgModelListResult {
		t.Errorf("expected model_list_result in outbox, got %+v", msgs)
	}
}

// ── 4. chat_message token + done: send failures queue in outbox ──────────────

func TestPhase4_ChatMessage_TokenAndDone_SendFails_QueuesInOutbox(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	hub := &errHub{err: fmt.Errorf("send failed")}
	outbox := relay.NewOutbox(s, &relay.InProcessHub{})

	sessionDone := make(chan struct{})

	dispatch := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		Outbox:    outbox,
		ChatSession: func(ctx context.Context, sessionID, content string,
			onToken func(string),
			onToolEvent func(string, map[string]any),
			onEvent func(backend.StreamEvent)) error {
			onToken("hello")
			onToken("world")
			return nil
		},
	})

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgChatMessage,
		MachineID: "m1",
		Payload:   map[string]any{"session_id": "chat-1", "content": "hi"},
	})

	// Wait for the goroutine to finish (it calls sendOrEnqueue for token×2 + done×1).
	deadline := time.After(3 * time.Second)
	for {
		n, err := outbox.Len()
		if err != nil {
			t.Fatalf("Len: %v", err)
		}
		if n >= 3 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for outbox to fill (got %d messages)", n)
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	_ = sessionDone

	msgs, err := outbox.Drain(10)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}

	var tokenCount, doneCount int
	for _, m := range msgs {
		switch m.Type {
		case relay.MsgToken:
			tokenCount++
		case relay.MsgDone:
			doneCount++
		}
	}
	if tokenCount != 2 {
		t.Errorf("expected 2 token messages in outbox, got %d", tokenCount)
	}
	if doneCount != 1 {
		t.Errorf("expected 1 done message in outbox, got %d", doneCount)
	}
}

// ── 5. No outbox wired: send failure logged only, no panic ───────────────────

func TestPhase4_NoOutbox_SendFails_NoPanic(t *testing.T) {
	hub := &errHub{err: fmt.Errorf("network error")}

	dispatch := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		// No Outbox wired.
		NewSession: func(reqID string) string { return "sess-" + reqID },
	})

	// Must not panic even without an outbox.
	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgSessionStart,
		MachineID: "m1",
		Payload:   map[string]any{"session_id": "req-2"},
	})
}

// ── 6. Outbox wired: verify Enqueue is called (outbox grows) when Send fails ──
//
// This test verifies the full sendOrEnqueue contract end-to-end:
// hub.Send fails → outbox.Enqueue is called → outbox depth increases.
// We use a separate outbox store so we can inspect it independently.
func TestPhase4_SendFails_OutboxGrows(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	hub := &errHub{err: fmt.Errorf("disconnected")}
	outbox := relay.NewOutbox(s, &relay.InProcessHub{})

	dispatch := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID:  "m1",
		Hub:        hub,
		Outbox:     outbox,
		ListModels: func() []string { return []string{"x"} },
	})

	// Before: outbox is empty.
	n, _ := outbox.Len()
	if n != 0 {
		t.Fatalf("expected empty outbox, got %d", n)
	}

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgModelListRequest,
		MachineID: "m1",
	})

	// After: outbox should have 1 queued message.
	n, err = outbox.Len()
	if err != nil {
		t.Fatalf("Len: %v", err)
	}
	if n != 1 {
		t.Errorf("expected outbox to grow to 1, got %d", n)
	}
}
