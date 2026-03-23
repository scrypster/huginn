package relay_test

// dispatcher_test.go — Phase 3 dispatcher handler tests.
// Tests for: ChatMessage streaming, duplicate session cancellation,
// CancelSession, SessionListRequest error path, and ActiveSessions generation counter.

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/relay"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// collectingHub captures every message sent through it (thread-safe).
type collectingHub struct {
	mu   sync.Mutex
	msgs []relay.Message
	err  error
}

func (h *collectingHub) Send(_ string, msg relay.Message) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.msgs = append(h.msgs, msg)
	return h.err
}

func (h *collectingHub) Close(_ string) {}

func (h *collectingHub) Collect() []relay.Message {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]relay.Message, len(h.msgs))
	copy(out, h.msgs)
	return out
}

func (h *collectingHub) WaitForType(t *testing.T, mt relay.MessageType, timeout time.Duration) relay.Message {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, m := range h.Collect() {
			if m.Type == mt {
				return m
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for message type %q", mt)
	return relay.Message{}
}

// minimalChatSession is a ChatSession func that streams two tokens then returns.
func minimalChatSession(tokens []string, errToReturn error) func(
	ctx context.Context, sessionID, userMsg string,
	onToken func(string),
	onToolEvent func(string, map[string]any),
	onEvent func(backend.StreamEvent)) error {
	return func(ctx context.Context, sessionID, userMsg string, onToken func(string), onToolEvent func(string, map[string]any), onEvent func(backend.StreamEvent)) error {
		for _, tok := range tokens {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			onToken(tok)
		}
		return errToReturn
	}
}

// blockingChatSession blocks until ctx is cancelled, then returns ctx.Err().
func blockingChatSession() func(
	ctx context.Context, sessionID, userMsg string,
	onToken func(string),
	onToolEvent func(string, map[string]any),
	onEvent func(backend.StreamEvent)) error {
	return func(ctx context.Context, _ string, _ string, _ func(string), _ func(string, map[string]any), _ func(backend.StreamEvent)) error {
		<-ctx.Done()
		return ctx.Err()
	}
}

// NewTestDispatcher creates a Dispatcher suitable for testing.
// It wraps NewDispatcher with reasonable defaults for fuzz/unit tests.
// Only for use in tests.
func NewTestDispatcher(t *testing.T) *relay.Dispatcher {
	t.Helper()
	hub := &collectingHub{}
	active := relay.NewActiveSessions()
	cfg := relay.DispatcherConfig{
		MachineID:   "test-machine",
		DeliverPerm: func(string, bool) bool { return false },
		Hub:         hub,
		Active:      active,
	}
	dispatch := relay.NewDispatcher(cfg)
	return &relay.Dispatcher{Fn: dispatch}
}

// ─── TestDispatcher_ChatMessage_StreamsTokens ─────────────────────────────────

// TestDispatcher_ChatMessage_StreamsTokens verifies that a chat_message with
// a wired ChatSession sends MsgToken frames followed by MsgDone.
func TestDispatcher_ChatMessage_StreamsTokens(t *testing.T) {
	hub := &collectingHub{}
	active := relay.NewActiveSessions()
	dispatched := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID:   "m1",
		DeliverPerm: func(string, bool) bool { return false },
		Hub:         hub,
		ChatSession: minimalChatSession([]string{"hello", " world"}, nil),
		Active:      active,
	})

	dispatched(context.Background(), relay.Message{
		Type: relay.MsgChatMessage,
		Payload: map[string]any{
			"session_id": "sess-stream",
			"content":    "hi",
		},
	})

	// Wait for MsgDone — the goroutine runs async.
	hub.WaitForType(t, relay.MsgDone, 3*time.Second)

	msgs := hub.Collect()
	var tokens []relay.Message
	var dones []relay.Message
	for _, m := range msgs {
		switch m.Type {
		case relay.MsgToken:
			tokens = append(tokens, m)
		case relay.MsgDone:
			dones = append(dones, m)
		}
	}

	if len(tokens) != 2 {
		t.Errorf("expected 2 token messages, got %d", len(tokens))
	}
	if len(dones) != 1 {
		t.Errorf("expected 1 done message, got %d", len(dones))
	}
	if dones[0].Payload["session_id"] != "sess-stream" {
		t.Errorf("done session_id = %v, want sess-stream", dones[0].Payload["session_id"])
	}
}

// ─── TestDispatcher_ChatMessage_DuplicateSession_CancelsFirst ────────────────

// TestDispatcher_ChatMessage_DuplicateSession_CancelsFirst verifies that sending
// two chat_message messages for the same session_id cancels the first before
// starting the second.
func TestDispatcher_ChatMessage_DuplicateSession_CancelsFirst(t *testing.T) {
	hub := &collectingHub{}
	active := relay.NewActiveSessions()

	var firstCtxCancelled int32 // 1 when first session ctx.Done() fires

	first := func(ctx context.Context, _ string, _ string, _ func(string), _ func(string, map[string]any), _ func(backend.StreamEvent)) error {
		<-ctx.Done()
		atomic.StoreInt32(&firstCtxCancelled, 1)
		return ctx.Err()
	}
	second := minimalChatSession([]string{"tok"}, nil)

	var callCount int32
	chatFn := func(ctx context.Context, sessionID, userMsg string, onToken func(string), onToolEvent func(string, map[string]any), onEvent func(backend.StreamEvent)) error {
		n := atomic.AddInt32(&callCount, 1)
		if n == 1 {
			return first(ctx, sessionID, userMsg, onToken, onToolEvent, onEvent)
		}
		return second(ctx, sessionID, userMsg, onToken, onToolEvent, onEvent)
	}

	cfg := relay.DispatcherConfig{
		MachineID:   "m1",
		DeliverPerm: func(string, bool) bool { return false },
		Hub:         hub,
		ChatSession: chatFn,
		Active:      active,
	}
	dispatched := relay.NewDispatcher(cfg)

	// Send first chat_message — first session starts and blocks.
	dispatched(context.Background(), relay.Message{
		Type:    relay.MsgChatMessage,
		Payload: map[string]any{"session_id": "sess-dup", "content": "msg1"},
	})

	// Give first session time to enter its blocking receive.
	time.Sleep(20 * time.Millisecond)

	// Send second chat_message for same session — first should be cancelled.
	dispatched(context.Background(), relay.Message{
		Type:    relay.MsgChatMessage,
		Payload: map[string]any{"session_id": "sess-dup", "content": "msg2"},
	})

	// Wait for two MsgDone (one for each session).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var doneCnt int
		for _, m := range hub.Collect() {
			if m.Type == relay.MsgDone {
				doneCnt++
			}
		}
		if doneCnt >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if atomic.LoadInt32(&firstCtxCancelled) != 1 {
		t.Error("first session context was not cancelled when second message arrived")
	}
}

// ─── TestDispatcher_CancelSession_StopsChat ───────────────────────────────────

// TestDispatcher_CancelSession_StopsChat verifies that a cancel_session message
// cancels the in-flight chat session goroutine.
func TestDispatcher_CancelSession_StopsChat(t *testing.T) {
	hub := &collectingHub{}
	active := relay.NewActiveSessions()

	cfg := relay.DispatcherConfig{
		MachineID:   "m1",
		DeliverPerm: func(string, bool) bool { return false },
		Hub:         hub,
		ChatSession: blockingChatSession(),
		Active:      active,
	}
	dispatched := relay.NewDispatcher(cfg)

	// Start a blocking chat session.
	dispatched(context.Background(), relay.Message{
		Type:    relay.MsgChatMessage,
		Payload: map[string]any{"session_id": "sess-cancel", "content": "go"},
	})
	// Give the goroutine time to start and block.
	time.Sleep(20 * time.Millisecond)

	// Send cancel_session — should unblock the goroutine.
	dispatched(context.Background(), relay.Message{
		Type:    relay.MsgCancelSession,
		Payload: map[string]any{"session_id": "sess-cancel"},
	})

	// Wait for MsgDone — goroutine should finish after cancel.
	hub.WaitForType(t, relay.MsgDone, 3*time.Second)
}

// ─── TestDispatcher_SessionListRequest_NotWired_NoResponse ───────────────────

// TestDispatcher_SessionListRequest_NotWired_NoResponse verifies that when
// Store is nil the dispatcher logs a warning and sends no MsgSessionListResult.
func TestDispatcher_SessionListRequest_NotWired_NoResponse(t *testing.T) {
	hub := &collectingHub{}
	cfg := relay.DispatcherConfig{
		MachineID:   "m1",
		DeliverPerm: func(string, bool) bool { return false },
		Hub:         hub,
		Store:       nil, // not wired
	}
	dispatched := relay.NewDispatcher(cfg)

	dispatched(context.Background(), relay.Message{
		Type:    relay.MsgSessionListRequest,
		Payload: map[string]any{},
	})

	// Should NOT receive any MsgSessionListResult — not wired.
	time.Sleep(50 * time.Millisecond)
	for _, m := range hub.Collect() {
		if m.Type == relay.MsgSessionListResult {
			t.Errorf("unexpected MsgSessionListResult when store is nil: %v", m.Payload)
		}
	}
}

// TestDispatcher_SessionListRequest_StoreSucceeds_SendsResult verifies that
// when Store.List() succeeds, the dispatcher sends MsgSessionListResult with
// a "sessions" key in the payload.
func TestDispatcher_SessionListRequest_StoreSucceeds_SendsResult(t *testing.T) {
	db := openTestDB(t)
	store := relay.NewSessionStore(db)
	hub := &collectingHub{}

	// Pre-save a session so List returns it.
	if err := store.Save(relay.SessionMeta{ID: "list-sess-1", Status: "active"}); err != nil {
		t.Fatal(err)
	}

	cfg := relay.DispatcherConfig{
		MachineID:   "m1",
		DeliverPerm: func(string, bool) bool { return false },
		Hub:         hub,
		Store:       store,
	}
	dispatched := relay.NewDispatcher(cfg)

	dispatched(context.Background(), relay.Message{
		Type:    relay.MsgSessionListRequest,
		Payload: map[string]any{},
	})

	got := hub.WaitForType(t, relay.MsgSessionListResult, 2*time.Second)
	if _, hasSessions := got.Payload["sessions"]; !hasSessions {
		t.Errorf("expected sessions field in MsgSessionListResult payload, got %v", got.Payload)
	}
	if _, hasErr := got.Payload["error"]; hasErr {
		t.Errorf("unexpected error field in MsgSessionListResult: %v", got.Payload["error"])
	}
}

// ─── TestActiveSessions_GenerationCounter ─────────────────────────────────────

// TestActiveSessions_GenerationCounter verifies that Remove with the wrong
// generation does NOT delete the current entry, preventing replaced sessions
// from cleaning up their successors.
func TestActiveSessions_GenerationCounter(t *testing.T) {
	as := relay.NewActiveSessions()

	// Register first session — gen = 1.
	cancel1Called := false
	cancel1 := func() { cancel1Called = true }
	gen1, _ := as.Start("sess", cancel1)

	// Register second session for same ID — gen = 2, first should be cancelled.
	cancel2Called := false
	cancel2 := func() { cancel2Called = true }
	gen2, replaced := as.Start("sess", cancel2)

	if !replaced {
		t.Error("Start should report replaced=true for second registration")
	}
	if !cancel1Called {
		t.Error("first cancel should have been called when second started")
	}
	if gen2 <= gen1 {
		t.Errorf("gen2 (%d) should be > gen1 (%d)", gen2, gen1)
	}

	// Remove with gen1 (stale) — should NOT delete the entry (which now has gen2).
	as.Remove("sess", gen1)

	// Cancel with the correct ID should still work (entry is still present).
	found := as.Cancel("sess")
	if !found {
		t.Error("Cancel should find session after stale Remove; entry was wrongly deleted")
	}
	if !cancel2Called {
		t.Error("cancel2 should have been called by Cancel")
	}

	// Remove gen1 already ran (stale), so cancel2 was NOT called by Remove.
	// Verify no double-free by cancelling again — should return false.
	found = as.Cancel("sess")
	if found {
		t.Error("Cancel should return false when session already removed")
	}
}

// ─── Existing dispatcher compat (DispatcherConfig with nil ChatSession) ───────

// TestDispatcher_Config_PermissionResp_StillWorks verifies the new DispatcherConfig
// approach still handles permission_response messages correctly.
func TestDispatcher_Config_PermissionResp_StillWorks(t *testing.T) {
	type rec struct {
		id       string
		approved bool
	}
	records := make(chan rec, 1)
	dispatched := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "m1",
		DeliverPerm: func(id string, approved bool) bool {
			records <- rec{id, approved}
			return true
		},
	})

	dispatched(context.Background(), relay.Message{
		Type:      relay.MsgPermissionResp,
		MachineID: "m1",
		Payload:   map[string]any{"request_id": "req-ok", "approved": true},
	})

	select {
	case r := <-records:
		if r.id != "req-ok" {
			t.Errorf("id = %q, want req-ok", r.id)
		}
		if !r.approved {
			t.Error("approved should be true")
		}
	case <-time.After(time.Second):
		t.Fatal("deliverPerm was not called")
	}
}

// TestDispatcher_Config_SessionResume_StillWorks verifies session_resume still
// works with the new DispatcherConfig approach.
func TestDispatcher_Config_SessionResume_StillWorks(t *testing.T) {
	db := openTestDB(t)
	store := relay.NewSessionStore(db)
	hub := &collectingHub{}

	sess := relay.SessionMeta{ID: "sess-r2", Status: "active", LastSeq: 7}
	if err := store.Save(sess); err != nil {
		t.Fatal(err)
	}

	cfg := relay.DispatcherConfig{
		MachineID:   "m1",
		DeliverPerm: func(string, bool) bool { return false },
		Hub:         hub,
		Store:       store,
	}
	dispatched := relay.NewDispatcher(cfg)

	dispatched(context.Background(), relay.Message{
		Type:    relay.MsgSessionResume,
		Payload: map[string]any{"session_id": "sess-r2"},
	})

	got := hub.WaitForType(t, relay.MsgSessionResumeAck, 2*time.Second)
	if got.Payload["session_id"] != "sess-r2" {
		t.Errorf("session_id = %v, want sess-r2", got.Payload["session_id"])
	}
	if got.Payload["status"] != "active" {
		t.Errorf("status = %v, want active", got.Payload["status"])
	}
}

// TestDispatcher_ChatMessage_StoreUpdatedOnCompletion verifies that when a
// ChatSession completes, the store is updated with status "completed".
func TestDispatcher_ChatMessage_StoreUpdatedOnCompletion(t *testing.T) {
	db := openTestDB(t)
	store := relay.NewSessionStore(db)
	hub := &collectingHub{}
	active := relay.NewActiveSessions()

	cfg := relay.DispatcherConfig{
		MachineID:   "m1",
		DeliverPerm: func(string, bool) bool { return false },
		Hub:         hub,
		Store:       store,
		ChatSession: minimalChatSession(nil, nil),
		Active:      active,
	}
	dispatched := relay.NewDispatcher(cfg)

	dispatched(context.Background(), relay.Message{
		Type:    relay.MsgChatMessage,
		Payload: map[string]any{"session_id": "sess-store", "content": "hi"},
	})

	hub.WaitForType(t, relay.MsgDone, 3*time.Second)

	// Give the goroutine a moment to write the store update.
	time.Sleep(20 * time.Millisecond)

	sess, err := store.Get("sess-store")
	if err != nil {
		t.Fatalf("session not saved: %v", err)
	}
	if sess.Status != "completed" {
		t.Errorf("status = %q, want completed", sess.Status)
	}
}

// TestDispatcher_ChatMessage_StoreUpdatedOnError verifies that when a ChatSession
// returns a non-cancellation error, the store status is "failed".
func TestDispatcher_ChatMessage_StoreUpdatedOnError(t *testing.T) {
	db := openTestDB(t)
	store := relay.NewSessionStore(db)
	hub := &collectingHub{}
	active := relay.NewActiveSessions()

	chatErr := errors.New("something went wrong")
	cfg := relay.DispatcherConfig{
		MachineID:   "m1",
		DeliverPerm: func(string, bool) bool { return false },
		Hub:         hub,
		Store:       store,
		ChatSession: minimalChatSession(nil, chatErr),
		Active:      active,
	}
	dispatched := relay.NewDispatcher(cfg)

	dispatched(context.Background(), relay.Message{
		Type:    relay.MsgChatMessage,
		Payload: map[string]any{"session_id": "sess-fail", "content": "hi"},
	})

	hub.WaitForType(t, relay.MsgDone, 3*time.Second)
	time.Sleep(20 * time.Millisecond)

	sess, err := store.Get("sess-fail")
	if err != nil {
		t.Fatalf("session not saved: %v", err)
	}
	if sess.Status != "failed" {
		t.Errorf("status = %q, want failed", sess.Status)
	}
}

// ─── TestDispatcher_SessionResume_Duplicate ───────────────────────────────────

// TestDispatcher_SessionResume_Duplicate verifies that the dispatcher handles
// duplicate session_resume messages idempotently. If the cloud's ack is lost
// and the same session_resume is replayed, the dispatcher must:
// 1. Send an ack again (not fail)
// 2. Not create a duplicate session entry (or if it does, it must be an upsert
//    that leaves exactly one session with that ID in the store)
//
// The idempotency guarantee comes from the SessionStore's use of Pebble:
// key-based upsert semantics mean a second Save with the same ID naturally
// overwrites the first. The dispatcher itself does not call Save on
// session_resume (it only reads), so duplicates are transparent to the handler.
func TestDispatcher_SessionResume_Duplicate(t *testing.T) {
	db := openTestDB(t)
	store := relay.NewSessionStore(db)
	hub := &collectingHub{}

	// Pre-save a session so it exists for the first resume.
	sess := relay.SessionMeta{ID: "sess-abc", Status: "active", LastSeq: 5}
	if err := store.Save(sess); err != nil {
		t.Fatal(err)
	}

	cfg := relay.DispatcherConfig{
		MachineID:   "m1",
		DeliverPerm: func(string, bool) bool { return false },
		Hub:         hub,
		Store:       store,
	}
	dispatched := relay.NewDispatcher(cfg)

	msg := relay.Message{
		Type:    relay.MsgSessionResume,
		Payload: map[string]any{"session_id": "sess-abc"},
	}

	// First resume
	dispatched(context.Background(), msg)
	ack1 := hub.WaitForType(t, relay.MsgSessionResumeAck, 2*time.Second)

	// Second resume (duplicate — ack must have been lost)
	dispatched(context.Background(), msg)
	ack2 := hub.WaitForType(t, relay.MsgSessionResumeAck, 2*time.Second)

	// Both acks should have correct session metadata.
	if ack1.Payload["session_id"] != "sess-abc" {
		t.Errorf("first ack session_id = %v, want sess-abc", ack1.Payload["session_id"])
	}
	if ack2.Payload["session_id"] != "sess-abc" {
		t.Errorf("second ack session_id = %v, want sess-abc", ack2.Payload["session_id"])
	}

	// Verify only one session exists with this ID in the store.
	sessions, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	count := 0
	for _, s := range sessions {
		if s.ID == "sess-abc" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 session with ID 'sess-abc', got %d", count)
	}
}

// ─── HTTPProxy dispatch regression ────────────────────────────────────────────

// TestDispatcher_HTTPRequest_MatchingMachineID verifies that an http_request
// addressed to the configured machine ID is proxied (HTTPProxy called) and an
// http_response is sent back via the hub.
//
// The dispatcher cfg.MachineID must match the machine ID registered in
// huginncloud-api (DynamoDB). In huginn serve this is cfg.MachineID (the full
// "hostname-hex" form from config.json), NOT relay.GetMachineID() (8-char hex).
func TestDispatcher_HTTPRequest_MatchingMachineID(t *testing.T) {
	hub := &collectingHub{}
	proxyCalled := make(chan struct{}, 1)

	dispatch := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "test-machine-abc",
		Hub:       hub,
		HTTPProxy: func(method, path string, body []byte) (int, []byte, error) {
			proxyCalled <- struct{}{}
			return 200, []byte(`[{"name":"Alex"}]`), nil
		},
	})

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgHTTPRequest,
		MachineID: "test-machine-abc", // matches cfg.MachineID
		Payload: map[string]any{
			"request_id": "req-001",
			"method":     "GET",
			"path":       "/api/v1/agents",
			"body":       "",
		},
	})

	select {
	case <-proxyCalled:
		// correct
	case <-time.After(time.Second):
		t.Fatal("HTTPProxy was not called — http_request was silently dropped")
	}

	// Verify http_response was sent back via the hub.
	time.Sleep(50 * time.Millisecond) // give goroutine time to send
	msgs := hub.Collect()
	if len(msgs) == 0 {
		t.Fatal("no http_response sent to hub")
	}
	if msgs[0].Type != relay.MsgHTTPResponse {
		t.Errorf("expected MsgHTTPResponse, got %q", msgs[0].Type)
	}
	if rid, _ := msgs[0].Payload["request_id"].(string); rid != "req-001" {
		t.Errorf("expected request_id=req-001, got %q", rid)
	}
}

// TestDispatcher_HTTPRequest_WrongMachineID verifies that http_request
// addressed to a different machine is silently dropped.
func TestDispatcher_HTTPRequest_WrongMachineID(t *testing.T) {
	hub := &collectingHub{}
	proxyCalled := make(chan struct{}, 1)

	dispatch := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "test-machine-abc",
		Hub:       hub,
		HTTPProxy: func(method, path string, body []byte) (int, []byte, error) {
			proxyCalled <- struct{}{}
			return 200, []byte(`[]`), nil
		},
	})

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgHTTPRequest,
		MachineID: "test-machine-DIFFERENT", // does not match
		Payload: map[string]any{
			"request_id": "req-bad",
			"method":     "GET",
			"path":       "/api/v1/agents",
			"body":       "",
		},
	})

	select {
	case <-proxyCalled:
		t.Fatal("HTTPProxy should not be called for wrong machine ID")
	case <-time.After(100 * time.Millisecond):
		// correct: message dropped
	}

	if msgs := hub.Collect(); len(msgs) != 0 {
		t.Errorf("expected no hub messages, got %d", len(msgs))
	}
}

// ─── TestDispatcher_StreamWarning_ForwardedToHub ──────────────────────────────

// TestDispatcher_StreamWarning_ForwardedToHub verifies that when the ChatSession
// emits a backend.StreamWarning event via the onEvent callback, the dispatcher
// forwards it to the hub as a MsgWarning message with the correct session_id and
// text fields.
func TestDispatcher_StreamWarning_ForwardedToHub(t *testing.T) {
	hub := &collectingHub{}
	active := relay.NewActiveSessions()

	const warnText = "vault unavailable: connection refused"

	dispatched := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID:   "m1",
		DeliverPerm: func(string, bool) bool { return false },
		Hub:         hub,
		Active:      active,
		ChatSession: func(ctx context.Context, sessionID, userMsg string,
			onToken func(string),
			onToolEvent func(string, map[string]any),
			onEvent func(backend.StreamEvent)) error {
			// Emit one warning then one token so MsgDone is sent.
			onEvent(backend.StreamEvent{Type: backend.StreamWarning, Content: warnText})
			onToken("ok")
			return nil
		},
	})

	dispatched(context.Background(), relay.Message{
		Type: relay.MsgChatMessage,
		Payload: map[string]any{
			"session_id": "sess-warn",
			"content":    "hi",
		},
	})

	hub.WaitForType(t, relay.MsgDone, 3*time.Second)
	msgs := hub.Collect()

	var warnings []relay.Message
	for _, m := range msgs {
		if m.Type == relay.MsgWarning {
			warnings = append(warnings, m)
		}
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning message, got %d", len(warnings))
	}
	if warnings[0].Payload["session_id"] != "sess-warn" {
		t.Errorf("warning session_id = %v, want sess-warn", warnings[0].Payload["session_id"])
	}
	if warnings[0].Payload["text"] != warnText {
		t.Errorf("warning text = %v, want %q", warnings[0].Payload["text"], warnText)
	}
}

// ─── TestDispatcher_ChatMessage_TokenPayload_FieldName ────────────────────────

// TestDispatcher_ChatMessage_TokenPayload_FieldName verifies that MsgToken
// payloads use "text" as the key for the token content (not "token").
// This aligns with the TypeScript TokenPayload interface: { text: string }.
// Using "token" would silently break streaming in the cloud browser client
// because envelope normalization in huginncloud-api does not rename payload keys.
func TestDispatcher_ChatMessage_TokenPayload_FieldName(t *testing.T) {
	hub := &collectingHub{}
	active := relay.NewActiveSessions()

	dispatched := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID:   "m1",
		DeliverPerm: func(string, bool) bool { return false },
		Hub:         hub,
		Active:      active,
		ChatSession: minimalChatSession([]string{"hello"}, nil),
	})

	dispatched(context.Background(), relay.Message{
		Type: relay.MsgChatMessage,
		Payload: map[string]any{
			"session_id": "sess-field-name",
			"content":    "hi",
		},
	})

	hub.WaitForType(t, relay.MsgDone, 3*time.Second)
	msgs := hub.Collect()

	var tokenMsgs []relay.Message
	for _, m := range msgs {
		if m.Type == relay.MsgToken {
			tokenMsgs = append(tokenMsgs, m)
		}
	}
	if len(tokenMsgs) == 0 {
		t.Fatal("expected at least one MsgToken message")
	}
	tok := tokenMsgs[0]
	if _, hasText := tok.Payload["text"]; !hasText {
		t.Errorf("MsgToken payload missing 'text' key — frontend reads payload.text, got keys: %v", tokKeys(tok.Payload))
	}
	if _, hasToken := tok.Payload["token"]; hasToken {
		t.Errorf("MsgToken payload must NOT have 'token' key — use 'text' to match TypeScript TokenPayload interface")
	}
}

func tokKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
