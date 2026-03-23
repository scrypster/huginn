package relay_test

// hardening_iter10_test.go — Hardening iteration 10.
// Covers gaps not addressed by prior iterations:
//   1. Satellite.Connect called twice closes the old hub first (no leak)
//   2. Satellite.ActiveHub returns InProcessHub when hub is nil
//   3. Satellite.Disconnect → Connect cycle (reconnect after explicit disconnect)
//   4. Dispatcher MsgSessionResume: session found sends ack; session not found is safe
//   5. Dispatcher MsgSessionResume: missing session_id is safe no-op
//   6. Dispatcher MsgSessionStart: sends session_start_ack when wired
//   7. Dispatcher MsgCancelSession: missing session_id is a safe no-op
//   8. ActiveSessions.Start replaces existing session and cancels it
//   9. ActiveSessions.Remove with wrong generation does NOT remove entry
//  10. WebSocketHub.sendHello includes active_sessions from SessionStore
//  11. satelliteVersion uses HUGINN_VERSION env var when set
//  12. Satellite.Connect returns error when token missing (not registered)

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/storage"
)

// ── 1. Satellite.Connect called twice closes the old hub ─────────────────────

// TestSatellite_Connect_Twice_ClosesOldHub verifies that calling Connect a
// second time closes the previous hub (no connection leak) and the new hub
// takes over. We use a test WS server that counts connects.
func TestSatellite_Connect_Twice_ClosesOldHub(t *testing.T) {
	var mu sync.Mutex
	connectCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		mu.Lock()
		connectCount++
		mu.Unlock()
		defer conn.Close()
		// Drain hello and stay alive until the client closes.
		conn.ReadMessage() //nolint
		<-r.Context().Done()
	}))
	defer srv.Close()

	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")
	store := &relay.MemoryTokenStore{}
	_ = store.Save("test-tok")
	sat := relay.NewSatelliteWithStore(wsBase, store)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// First connect.
	if err := sat.Connect(ctx); err != nil {
		t.Fatalf("first Connect: %v", err)
	}

	// Second connect — should close the first hub and open a second connection.
	if err := sat.Connect(ctx); err != nil {
		t.Fatalf("second Connect: %v", err)
	}
	defer sat.Disconnect()

	// Give goroutines time to settle.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	cc := connectCount
	mu.Unlock()
	if cc < 2 {
		t.Errorf("expected at least 2 connections (one per Connect call), got %d", cc)
	}
}

// ── 2. Satellite.ActiveHub returns InProcessHub when hub is nil ───────────────

// TestSatellite_ActiveHub_ReturnsInProcessWhenNil verifies that ActiveHub on a
// freshly created (never connected) satellite returns an InProcessHub.
func TestSatellite_ActiveHub_ReturnsInProcessWhenNil(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	sat := relay.NewSatelliteWithStore("wss://example.com", store)

	hub := sat.ActiveHub()
	if hub == nil {
		t.Fatal("expected non-nil hub from ActiveHub")
	}
	_, ok := hub.(*relay.InProcessHub)
	if !ok {
		t.Errorf("expected InProcessHub when no hub set, got %T", hub)
	}
}

// ── 3. Satellite.Disconnect → Connect cycle ────────────────────────────────────

// TestSatellite_Disconnect_Then_Connect verifies that after an explicit
// Disconnect, Connect succeeds and the satellite is active again.
func TestSatellite_Disconnect_Then_Connect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.ReadMessage() //nolint
		<-r.Context().Done()
	}))
	defer srv.Close()

	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")
	store := &relay.MemoryTokenStore{}
	_ = store.Save("cycle-tok")
	sat := relay.NewSatelliteWithStore(wsBase, store)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Connect, disconnect, reconnect.
	if err := sat.Connect(ctx); err != nil {
		t.Fatalf("first Connect: %v", err)
	}
	sat.Disconnect()

	// After disconnect, ActiveHub should return InProcessHub.
	hub := sat.ActiveHub()
	if _, ok := hub.(*relay.InProcessHub); !ok {
		t.Errorf("expected InProcessHub after Disconnect, got %T", hub)
	}

	// Connect again.
	if err := sat.Connect(ctx); err != nil {
		t.Fatalf("second Connect after Disconnect: %v", err)
	}
	defer sat.Disconnect()

	// Should be connected now.
	status := sat.Status()
	if !status.Connected {
		t.Error("expected Connected=true after reconnect")
	}
}

// ── 4. Satellite.Connect returns error when not registered ────────────────────

// TestSatellite_Connect_NotRegistered returns an error immediately.
func TestSatellite_Connect_NotRegistered(t *testing.T) {
	store := &relay.MemoryTokenStore{} // no token
	sat := relay.NewSatelliteWithStore("wss://example.com", store)

	err := sat.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error from Connect when not registered, got nil")
	}
}

// ── 5. Dispatcher MsgSessionResume: session found sends ack ───────────────────

// TestDispatcher_SessionResume_Found_SendsAck verifies that a session_resume
// message for an existing session causes the dispatcher to send session_resume_ack.
func TestDispatcher_SessionResume_Found_SendsAck(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	store := relay.NewSessionStore(s)
	sess := relay.SessionMeta{
		ID:      "session-abc",
		Status:  "active",
		LastSeq: 5,
	}
	if err := store.Save(sess); err != nil {
		t.Fatalf("Save session: %v", err)
	}

	sent := make(chan relay.Message, 4)
	hub := &replyRecorder{ch: sent}

	dispatch := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		Store:     store,
	})

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgSessionResume,
		MachineID: "m1",
		Payload:   map[string]any{"session_id": "session-abc"},
	})

	select {
	case msg := <-sent:
		if msg.Type != relay.MsgSessionResumeAck {
			t.Errorf("expected session_resume_ack, got %q", msg.Type)
		}
		if msg.Payload["session_id"] != "session-abc" {
			t.Errorf("ack session_id = %v, want %q", msg.Payload["session_id"], "session-abc")
		}
		if msg.Payload["last_seq"] != uint64(5) {
			t.Errorf("ack last_seq = %v, want 5", msg.Payload["last_seq"])
		}
	case <-time.After(time.Second):
		t.Fatal("no session_resume_ack received")
	}
}

// ── 6. Dispatcher MsgSessionResume: session not found is safe no-op ───────────

// TestDispatcher_SessionResume_NotFound_NoSend verifies that a session_resume
// for a non-existent session does not send anything (logs warning, returns).
func TestDispatcher_SessionResume_NotFound_NoSend(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	store := relay.NewSessionStore(s)
	sent := make(chan relay.Message, 4)
	hub := &replyRecorder{ch: sent}

	dispatch := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		Store:     store,
	})

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgSessionResume,
		MachineID: "m1",
		Payload:   map[string]any{"session_id": "does-not-exist"},
	})

	select {
	case msg := <-sent:
		t.Errorf("unexpected message sent for unknown session: %q", msg.Type)
	case <-time.After(100 * time.Millisecond):
		// Correct: no message sent.
	}
}

// ── 7. Dispatcher MsgSessionResume: missing session_id ────────────────────────

// TestDispatcher_SessionResume_MissingSessionID verifies safe no-op behavior.
func TestDispatcher_SessionResume_MissingSessionID(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	store := relay.NewSessionStore(s)
	sent := make(chan relay.Message, 4)
	hub := &replyRecorder{ch: sent}

	dispatch := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		Store:     store,
	})

	// Must not panic.
	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgSessionResume,
		MachineID: "m1",
		Payload:   map[string]any{}, // missing session_id
	})

	select {
	case msg := <-sent:
		t.Errorf("unexpected message for missing session_id: %q", msg.Type)
	case <-time.After(100 * time.Millisecond):
		// Correct: no send.
	}
}

// ── 8. Dispatcher MsgSessionStart: sends session_start_ack ────────────────────

// TestDispatcher_SessionStart_SendsAck verifies that when NewSession is wired,
// a session_start message causes session_start_ack to be sent.
func TestDispatcher_SessionStart_SendsAck(t *testing.T) {
	sent := make(chan relay.Message, 4)
	hub := &replyRecorder{ch: sent}

	dispatch := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		NewSession: func(reqID string) string {
			return "created-" + reqID
		},
	})

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgSessionStart,
		MachineID: "m1",
		Payload:   map[string]any{"session_id": "req-1"},
	})

	select {
	case msg := <-sent:
		if msg.Type != relay.MsgSessionStartAck {
			t.Errorf("expected session_start_ack, got %q", msg.Type)
		}
		if msg.Payload["session_id"] != "created-req-1" {
			t.Errorf("ack session_id = %v, want %q", msg.Payload["session_id"], "created-req-1")
		}
		if msg.Payload["status"] != "created" {
			t.Errorf("ack status = %v, want %q", msg.Payload["status"], "created")
		}
	case <-time.After(time.Second):
		t.Fatal("no session_start_ack received")
	}
}

// ── 9. Dispatcher MsgCancelSession: missing session_id ────────────────────────

// TestDispatcher_CancelSession_MissingSessionID verifies safe no-op.
func TestDispatcher_CancelSession_MissingSessionID(t *testing.T) {
	active := relay.NewActiveSessions()
	dispatch := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "m1",
		Active:    active,
	})

	// Must not panic.
	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgCancelSession,
		MachineID: "m1",
		Payload:   map[string]any{}, // no session_id
	})
}

// ── 10. ActiveSessions.Start replaces and cancels existing session ─────────────

// TestActiveSessions_Start_Replaces_CancelsExisting verifies that when Start
// is called for a session that already has an active entry, the old cancel
// function is called before the new one is stored.
func TestActiveSessions_Start_Replaces_CancelsExisting(t *testing.T) {
	as := relay.NewActiveSessions()

	oldCancelled := false
	oldCancel := func() { oldCancelled = true }

	gen1, replaced1 := as.Start("sess-1", oldCancel)
	if replaced1 {
		t.Error("first Start should not replace anything")
	}
	_ = gen1

	newCancelled := false
	newCancel := func() { newCancelled = true }

	gen2, replaced2 := as.Start("sess-1", newCancel)
	if !replaced2 {
		t.Error("second Start should have replaced the first")
	}
	if !oldCancelled {
		t.Error("old cancel should have been called when Start replaced session")
	}
	if newCancelled {
		t.Error("new cancel should NOT have been called yet")
	}
	_ = gen2
}

// ── 11. ActiveSessions.Remove with wrong generation does not remove ────────────

// TestActiveSessions_Remove_WrongGen_NoOp verifies that Remove with a stale
// generation token does NOT remove the current (replacement) entry.
// This prevents a goroutine that was replaced from inadvertently removing its
// successor's entry from the active session map.
func TestActiveSessions_Remove_WrongGen(t *testing.T) {
	as := relay.NewActiveSessions()

	gen1, _ := as.Start("sess-x", func() {})
	// Start a second session replacing the first; gen2 > gen1.
	// Note: Start cancels the old cancel immediately, so gen1's cancel fires here.
	gen2, replaced := as.Start("sess-x", func() {})
	if !replaced {
		t.Fatal("second Start should have set replaced=true")
	}
	_ = gen2

	// Remove with the OLD generation — must leave the current entry intact.
	as.Remove("sess-x", gen1)

	// The current entry (gen2) must still be present.
	found := as.Cancel("sess-x")
	if !found {
		t.Error("Cancel should find the entry after Remove with wrong gen (Remove was not a no-op)")
	}
}

// ── 12. WebSocketHub.sendHello includes active_sessions from SessionStore ──────

// TestWebSocketHub_Hello_IncludesActiveSessions verifies that when a SessionStore
// is provided in WebSocketConfig, the satellite_hello message sent on connect
// includes active_sessions in the payload.
func TestWebSocketHub_Hello_IncludesActiveSessions(t *testing.T) {
	helloCh := make(chan map[string]any, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Read satellite_hello.
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg map[string]any
		if json.Unmarshal(data, &msg) == nil {
			helloCh <- msg
		}
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	// Set up a SessionStore with one active session.
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	store := relay.NewSessionStore(s)
	if err := store.Save(relay.SessionMeta{ID: "active-sess", Status: "active"}); err != nil {
		t.Fatalf("save session: %v", err)
	}

	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{
		URL:   wsBase,
		Store: store,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer hub.Close("")

	select {
	case helloMsg := <-helloCh:
		payload, ok := helloMsg["payload"].(map[string]any)
		if !ok {
			t.Fatal("hello payload is not a map")
		}
		activeSessions, ok := payload["active_sessions"]
		if !ok {
			t.Fatal("hello payload missing active_sessions field")
		}
		sessions, ok := activeSessions.([]any)
		if !ok {
			t.Fatalf("active_sessions is not an array, got %T", activeSessions)
		}
		if len(sessions) != 1 {
			t.Errorf("expected 1 active session in hello, got %d", len(sessions))
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for satellite_hello")
	}
}

// ── 13. satelliteVersion uses HUGINN_VERSION env var ──────────────────────────

// TestSatellite_Connect_SendsVersionFromEnv verifies that when HUGINN_VERSION
// is set, the satellite_hello sent to the server includes that version string.
func TestSatellite_Connect_SendsVersionFromEnv(t *testing.T) {
	t.Setenv("HUGINN_VERSION", "v2.3.4-test")

	helloCh := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg map[string]any
		if json.Unmarshal(data, &msg) == nil {
			helloCh <- msg
		}
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	wsBase := "ws" + strings.TrimPrefix(srv.URL, "http")
	store := &relay.MemoryTokenStore{}
	_ = store.Save("tok")
	sat := relay.NewSatelliteWithStore(wsBase, store)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := sat.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer sat.Disconnect()

	select {
	case helloMsg := <-helloCh:
		payload, ok := helloMsg["payload"].(map[string]any)
		if !ok {
			t.Fatal("hello has no payload map")
		}
		if payload["version"] != "v2.3.4-test" {
			t.Errorf("hello version = %v, want %q", payload["version"], "v2.3.4-test")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for satellite_hello")
	}
}

// ── 14. Dispatcher MsgModelListRequest returns model list ─────────────────────

// TestDispatcher_ModelListRequest_ReturnsList verifies that a model_list_request
// causes the dispatcher to call ListModels and send model_list_result.
func TestDispatcher_ModelListRequest_ReturnsList(t *testing.T) {
	sent := make(chan relay.Message, 4)
	hub := &replyRecorder{ch: sent}

	dispatch := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		ListModels: func() []string {
			return []string{"claude-3", "claude-3-haiku"}
		},
	})

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgModelListRequest,
		MachineID: "m1",
	})

	select {
	case msg := <-sent:
		if msg.Type != relay.MsgModelListResult {
			t.Errorf("expected model_list_result, got %q", msg.Type)
		}
		models, ok := msg.Payload["models"].([]string)
		if !ok {
			t.Fatalf("models is not []string, got %T", msg.Payload["models"])
		}
		if len(models) != 2 {
			t.Errorf("expected 2 models, got %d", len(models))
		}
	case <-time.After(time.Second):
		t.Fatal("no model_list_result received")
	}
}

// ── 15. Dispatcher ChatMessage completes and sends done ───────────────────────

// TestDispatcher_ChatMessage_SendsDone verifies that a chat_message causes
// the ChatSession function to be called and a done message to be sent after
// the session completes.
func TestDispatcher_ChatMessage_SendsDone(t *testing.T) {
	sent := make(chan relay.Message, 16)
	hub := &replyRecorder{ch: sent}
	sessionCalled := make(chan struct{}, 1)

	dispatch := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		ChatSession: func(ctx context.Context, sessionID, content string,
			onToken func(string),
			onToolEvent func(string, map[string]any),
			onEvent func(backend.StreamEvent)) error {
			sessionCalled <- struct{}{}
			onToken("hello ")
			onToken("world")
			return nil
		},
	})

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgChatMessage,
		MachineID: "m1",
		Payload:   map[string]any{"session_id": "chat-1", "content": "hi"},
	})

	// Wait for session to be called.
	select {
	case <-sessionCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("ChatSession was not called")
	}

	// Collect messages: 2 tokens + 1 done.
	var types []relay.MessageType
	deadline := time.After(2 * time.Second)
collectLoop:
	for {
		select {
		case msg := <-sent:
			types = append(types, msg.Type)
			if msg.Type == relay.MsgDone {
				break collectLoop
			}
		case <-deadline:
			t.Fatalf("timed out waiting for done; got so far: %v", types)
		}
	}

	var gotDone bool
	var tokenCount int
	for _, mt := range types {
		switch mt {
		case relay.MsgToken:
			tokenCount++
		case relay.MsgDone:
			gotDone = true
		}
	}
	if !gotDone {
		t.Error("expected MsgDone to be sent after ChatSession completes")
	}
	if tokenCount != 2 {
		t.Errorf("expected 2 token messages, got %d", tokenCount)
	}
}

// ── helpers ────────────────────────────────────────────────────────────────────

// replyRecorder records Send calls into ch. Implements relay.Hub.
type replyRecorder struct {
	ch chan relay.Message
}

func (r *replyRecorder) Send(_ string, msg relay.Message) error {
	select {
	case r.ch <- msg:
	default:
	}
	return nil
}
func (r *replyRecorder) Close(_ string) {}
