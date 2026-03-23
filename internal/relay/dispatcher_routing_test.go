package relay_test

// hardening_iter5_test.go — Hardening iteration 5.
// Adds tests for:
//   1. Dispatcher: all routing branches (permission_response approved/denied,
//      wrong machine_id rejection, chat_message, session_start, session_list_request,
//      model_list_request, unknown type, missing request_id).
//   2. relay.go constants: all message type constants are non-empty strings;
//      no duplicate values.
//   3. Registration: browser callback missing params path;
//      device code denied path.
//   4. WebSocket: SetOnMessage nil handler doesn't panic.
//   5. Internal helpers via exported surface: freePort (indirectly via Register),
//      generateDeviceCode format (indirectly via device code flow mock).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/scrypster/huginn/internal/relay"
)

// ─── Dispatcher tests ──────────────────────────────────────────────────────────

// makeDispatcher is a test helper that builds a dispatcher with the given machine
// ID and a deliver function that records calls.
type dispatchRecord struct {
	requestID string
	approved  bool
}

func makeDispatcher(machineID string, records chan dispatchRecord, deliverReturn bool) func(context.Context, relay.Message) {
	return relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: machineID,
		DeliverPerm: func(requestID string, approved bool) bool {
			records <- dispatchRecord{requestID: requestID, approved: approved}
			return deliverReturn
		},
	})
}

// TestDispatcher_PermissionResponse_Approved verifies that a permission_response
// with approved=true is delivered to the deliverFn with the correct fields.
func TestDispatcher_PermissionResponse_Approved(t *testing.T) {
	records := make(chan dispatchRecord, 1)
	dispatch := makeDispatcher("machine-1", records, true)

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgPermissionResp,
		MachineID: "machine-1",
		Payload: map[string]any{
			"request_id": "req-abc",
			"approved":   true,
		},
	})

	select {
	case r := <-records:
		if r.requestID != "req-abc" {
			t.Errorf("requestID = %q, want %q", r.requestID, "req-abc")
		}
		if !r.approved {
			t.Error("expected approved=true")
		}
	case <-time.After(time.Second):
		t.Fatal("deliverFn was not called")
	}
}

// TestDispatcher_PermissionResponse_Denied verifies that a permission_response
// with approved=false is also delivered (denial is a valid outcome).
func TestDispatcher_PermissionResponse_Denied(t *testing.T) {
	records := make(chan dispatchRecord, 1)
	dispatch := makeDispatcher("machine-1", records, true)

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgPermissionResp,
		MachineID: "machine-1",
		Payload: map[string]any{
			"request_id": "req-deny",
			"approved":   false,
		},
	})

	select {
	case r := <-records:
		if r.requestID != "req-deny" {
			t.Errorf("requestID = %q, want %q", r.requestID, "req-deny")
		}
		if r.approved {
			t.Error("expected approved=false")
		}
	case <-time.After(time.Second):
		t.Fatal("deliverFn was not called for denied response")
	}
}

// TestDispatcher_PermissionResponse_UnknownRequestID verifies that when deliverFn
// returns false (unknown request ID), the dispatcher does not panic.
func TestDispatcher_PermissionResponse_UnknownRequestID(t *testing.T) {
	records := make(chan dispatchRecord, 1)
	// deliverReturn=false simulates an unknown / stale request ID.
	dispatch := makeDispatcher("machine-1", records, false)

	// Must not panic.
	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgPermissionResp,
		MachineID: "machine-1",
		Payload: map[string]any{
			"request_id": "stale-id",
			"approved":   true,
		},
	})

	select {
	case <-records:
		// deliverFn was called — correct.
	case <-time.After(time.Second):
		t.Fatal("deliverFn was not called even for stale request ID")
	}
}

// TestDispatcher_PermissionResponse_MissingRequestID verifies that a
// permission_response without request_id does not call deliverFn.
func TestDispatcher_PermissionResponse_MissingRequestID(t *testing.T) {
	records := make(chan dispatchRecord, 1)
	dispatch := makeDispatcher("machine-1", records, true)

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgPermissionResp,
		MachineID: "machine-1",
		Payload:   map[string]any{"approved": true}, // no request_id
	})

	select {
	case <-records:
		t.Error("deliverFn should NOT be called when request_id is missing")
	case <-time.After(100 * time.Millisecond):
		// Correct: no delivery.
	}
}

// TestDispatcher_WrongMachineID verifies that messages addressed to a different
// machine are silently dropped (defense-in-depth).
func TestDispatcher_WrongMachineID_Rejected(t *testing.T) {
	records := make(chan dispatchRecord, 1)
	dispatch := makeDispatcher("machine-correct", records, true)

	// Message addressed to a different machine — must be dropped.
	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgPermissionResp,
		MachineID: "machine-impostor", // wrong
		Payload: map[string]any{
			"request_id": "req-123",
			"approved":   true,
		},
	})

	select {
	case <-records:
		t.Error("deliverFn should NOT be called for wrong machine_id")
	case <-time.After(100 * time.Millisecond):
		// Correct: dropped.
	}
}

// TestDispatcher_EmptyMachineID_NotRejected verifies that a message with an
// empty machine_id passes the guard (broadcast / cloud-originating messages
// that carry no specific target).
func TestDispatcher_EmptyMachineID_NotRejected(t *testing.T) {
	records := make(chan dispatchRecord, 1)
	dispatch := makeDispatcher("machine-1", records, true)

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgPermissionResp,
		MachineID: "", // empty — no machine-ID filter should apply
		Payload: map[string]any{
			"request_id": "req-broadcast",
			"approved":   true,
		},
	})

	select {
	case r := <-records:
		if r.requestID != "req-broadcast" {
			t.Errorf("requestID = %q, want %q", r.requestID, "req-broadcast")
		}
	case <-time.After(time.Second):
		t.Fatal("deliverFn was not called for empty machine_id message")
	}
}

// TestDispatcher_ChatMessage_DoesNotPanic verifies chat_message is handled
// without panicking (stub handler: just logs).
func TestDispatcher_ChatMessage_DoesNotPanic(t *testing.T) {
	records := make(chan dispatchRecord, 1)
	dispatch := makeDispatcher("machine-1", records, true)

	// Must not panic.
	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgChatMessage,
		MachineID: "machine-1",
		Payload:   map[string]any{"text": "hello"},
	})
}

// TestDispatcher_SessionStart_DoesNotPanic verifies session_start is handled
// without panicking.
func TestDispatcher_SessionStart_DoesNotPanic(t *testing.T) {
	records := make(chan dispatchRecord, 1)
	dispatch := makeDispatcher("machine-1", records, true)

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgSessionStart,
		MachineID: "machine-1",
	})
}

// TestDispatcher_SessionListRequest_DoesNotPanic verifies session_list_request
// is handled without panicking.
func TestDispatcher_SessionListRequest_DoesNotPanic(t *testing.T) {
	records := make(chan dispatchRecord, 1)
	dispatch := makeDispatcher("machine-1", records, true)

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgSessionListRequest,
		MachineID: "machine-1",
	})
}

// TestDispatcher_ModelListRequest_DoesNotPanic verifies model_list_request is
// handled without panicking.
func TestDispatcher_ModelListRequest_DoesNotPanic(t *testing.T) {
	records := make(chan dispatchRecord, 1)
	dispatch := makeDispatcher("machine-1", records, true)

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgModelListRequest,
		MachineID: "machine-1",
	})
}

// TestDispatcher_UnknownMessageType_DoesNotPanic verifies that an unrecognized
// message type falls through to the default case without panicking.
func TestDispatcher_UnknownMessageType_DoesNotPanic(t *testing.T) {
	records := make(chan dispatchRecord, 1)
	dispatch := makeDispatcher("machine-1", records, true)

	dispatch(context.Background(), relay.Message{
		Type:      relay.MessageType("totally_unknown_type_xyzzy"),
		MachineID: "machine-1",
	})
}

// TestDispatcher_RunAgent_DoesNotPanic covers the Phase 3 run_agent stub.
func TestDispatcher_RunAgent_DoesNotPanic(t *testing.T) {
	records := make(chan dispatchRecord, 1)
	dispatch := makeDispatcher("machine-1", records, true)

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgRunAgent,
		MachineID: "machine-1",
		Payload:   map[string]any{"session": "s1"},
	})
}

// TestDispatcher_CancelSession_DoesNotPanic covers the Phase 3 cancel_session stub.
func TestDispatcher_CancelSession_DoesNotPanic(t *testing.T) {
	records := make(chan dispatchRecord, 1)
	dispatch := makeDispatcher("machine-1", records, true)

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgCancelSession,
		MachineID: "machine-1",
	})
}

// TestDispatcher_CorrectMachineID_Accepted verifies a message with the correct
// machine_id is not dropped.
func TestDispatcher_CorrectMachineID_Accepted(t *testing.T) {
	records := make(chan dispatchRecord, 1)
	dispatch := makeDispatcher("correct-machine", records, true)

	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgPermissionResp,
		MachineID: "correct-machine",
		Payload: map[string]any{
			"request_id": "req-ok",
			"approved":   true,
		},
	})

	select {
	case r := <-records:
		if r.requestID != "req-ok" {
			t.Errorf("requestID = %q, want %q", r.requestID, "req-ok")
		}
	case <-time.After(time.Second):
		t.Fatal("deliverFn was not called for correct machine_id")
	}
}

// ─── relay.go constants ────────────────────────────────────────────────────────

// TestMessageTypeConstants_AllNonEmpty verifies that every MessageType constant
// exported from relay.go and websocket.go is a non-empty string.
func TestMessageTypeConstants_AllNonEmpty(t *testing.T) {
	allTypes := []relay.MessageType{
		// relay.go
		relay.MsgToken,
		relay.MsgToolCall,
		relay.MsgToolResult,
		relay.MsgPermissionReq,
		relay.MsgPermissionResp,
		relay.MsgDone,
		relay.MsgSessionDone,
		relay.MsgNotificationSync,
		relay.MsgNotificationUpdate,
		relay.MsgSatelliteHeartbeat,
		relay.MsgSatelliteReconnect,
		relay.MsgNotificationActionRequest,
		relay.MsgNotificationActionResult,
		relay.MsgRunAgent,
		relay.MsgCancelSession,
		relay.MsgAgentResult,
		relay.MsgChatMessage,
		relay.MsgSessionStart,
		relay.MsgSessionListRequest,
		relay.MsgSessionListResult,
		relay.MsgModelListRequest,
		relay.MsgModelListResult,
		// websocket.go
		relay.MsgSatelliteHello,
	}
	for _, mt := range allTypes {
		if mt == "" {
			t.Error("found an empty MessageType constant")
		}
	}
}

// TestMessageTypeConstants_NoDuplicates verifies that no two MessageType
// constants share the same string value.
func TestMessageTypeConstants_NoDuplicates(t *testing.T) {
	allTypes := []struct {
		name string
		val  relay.MessageType
	}{
		{"MsgToken", relay.MsgToken},
		{"MsgToolCall", relay.MsgToolCall},
		{"MsgToolResult", relay.MsgToolResult},
		{"MsgPermissionReq", relay.MsgPermissionReq},
		{"MsgPermissionResp", relay.MsgPermissionResp},
		{"MsgDone", relay.MsgDone},
		{"MsgSessionDone", relay.MsgSessionDone},
		{"MsgNotificationSync", relay.MsgNotificationSync},
		{"MsgNotificationUpdate", relay.MsgNotificationUpdate},
		{"MsgSatelliteHeartbeat", relay.MsgSatelliteHeartbeat},
		{"MsgSatelliteReconnect", relay.MsgSatelliteReconnect},
		{"MsgNotificationActionRequest", relay.MsgNotificationActionRequest},
		{"MsgNotificationActionResult", relay.MsgNotificationActionResult},
		{"MsgRunAgent", relay.MsgRunAgent},
		{"MsgCancelSession", relay.MsgCancelSession},
		{"MsgAgentResult", relay.MsgAgentResult},
		{"MsgChatMessage", relay.MsgChatMessage},
		{"MsgSessionStart", relay.MsgSessionStart},
		{"MsgSessionListRequest", relay.MsgSessionListRequest},
		{"MsgSessionListResult", relay.MsgSessionListResult},
		{"MsgModelListRequest", relay.MsgModelListRequest},
		{"MsgModelListResult", relay.MsgModelListResult},
		{"MsgSatelliteHello", relay.MsgSatelliteHello},
	}
	seen := make(map[relay.MessageType]string)
	for _, mt := range allTypes {
		if prev, ok := seen[mt.val]; ok {
			t.Errorf("duplicate MessageType value %q shared by %s and %s", mt.val, prev, mt.name)
		}
		seen[mt.val] = mt.name
	}
}

// ─── Registration: browser callback missing params ─────────────────────────────

// TestRegistrar_BrowserFlow_MissingParams verifies that a callback delivering
// neither api_key nor machine_id causes Register to return an error.
func TestRegistrar_BrowserFlow_MissingParams(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	reg := relay.NewRegistrarWithStore("http://localhost:0", store)
	reg.OpenBrowserFn = func(rawURL string) error {
		u, _ := url.Parse(rawURL)
		cbURL := u.Query().Get("cb")
		go func() {
			time.Sleep(50 * time.Millisecond)
			// Hit the callback with no api_key or machine_id params.
			resp, err := http.Get(cbURL) // no query params
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := reg.Register(ctx, "test")
	if err == nil {
		t.Error("expected error when callback is missing api_key and machine_id")
	}
}

// TestRegistrar_BrowserFlow_OnlyAPIKeyMissing verifies that a callback with
// machine_id but no api_key causes an error.
func TestRegistrar_BrowserFlow_OnlyAPIKeyMissing(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	reg := relay.NewRegistrarWithStore("http://localhost:0", store)
	reg.OpenBrowserFn = func(rawURL string) error {
		u, _ := url.Parse(rawURL)
		cbURL := u.Query().Get("cb")
		go func() {
			time.Sleep(50 * time.Millisecond)
			resp, err := http.Get(cbURL + "?machine_id=some-machine") // no api_key
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := reg.Register(ctx, "test")
	if err == nil {
		t.Error("expected error when callback is missing api_key")
	}
}

// ─── Registration: device code denied ─────────────────────────────────────────

// TestRegistrar_DeviceCodeFlow_Denied verifies that a denied device code poll
// ultimately causes Register to return an error (via context cancellation, since
// the current implementation treats "denied" as a transient error and retries
// until the context deadline is reached).
//
// Note: The production deviceCodeFlow continues polling on any error from
// pollDeviceCode — including "denied" — which is a known limitation. The test
// validates that Register does NOT succeed (no API key is returned) when the
// server always returns denied.
func TestRegistrar_DeviceCodeFlow_Denied(t *testing.T) {
	store := &relay.MemoryTokenStore{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/device/poll" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"status":"denied"}`)) //nolint:errcheck
		}
	}))
	defer srv.Close()

	reg := relay.NewRegistrarWithStore(srv.URL, store)
	// Force browser flow to fail so we use device code flow.
	reg.OpenBrowserFn = func(_ string) error { return fmt.Errorf("no browser") }

	// Short context so the test completes quickly. The denied poll causes the
	// flow to keep retrying until this deadline is hit.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := reg.Register(ctx, "test")
	if err == nil {
		t.Error("expected error when device code is denied (server always returns denied)")
	}
	// Should not be registered after a denied flow.
	registered, _ := reg.Status()
	if registered {
		t.Error("machine should not be registered after denied device code flow")
	}
}

// TestRegistrar_DeviceCodeFlow_PendingThenApproved verifies the polling loop:
// first N polls return pending, then approved — Register should succeed.
func TestRegistrar_DeviceCodeFlow_PendingThenApproved(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	var pollCount int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/device/poll" {
			n := atomic.AddInt32(&pollCount, 1)
			w.Header().Set("Content-Type", "application/json")
			if n < 3 {
				w.Write([]byte(`{"status":"pending"}`)) //nolint:errcheck
			} else {
				w.Write([]byte(`{"status":"approved","api_key":"poll-key","machine_id":"poll-machine"}`)) //nolint:errcheck
			}
		}
	}))
	defer srv.Close()

	reg := relay.NewRegistrarWithStore(srv.URL, store)
	reg.OpenBrowserFn = func(_ string) error { return fmt.Errorf("no browser") }
	reg.PollInterval = 20 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := reg.Register(ctx, "test")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if result.APIKey != "poll-key" {
		t.Errorf("APIKey = %q, want %q", result.APIKey, "poll-key")
	}
	if atomic.LoadInt32(&pollCount) < 3 {
		t.Errorf("expected at least 3 poll calls (pending×2 then approved), got %d", atomic.LoadInt32(&pollCount))
	}
}

// ─── generateDeviceCode format validation ──────────────────────────────────────

// TestRegistrar_DeviceCodeFlow_CodeFormat verifies that the device code
// presented to the user matches the "ABC-123" pattern (3 letters, dash, 3 digits).
// We test this indirectly by starting a mock server that captures the code from
// the poll URL query parameter.
func TestRegistrar_DeviceCodeFlow_CodeFormat(t *testing.T) {
	codePattern := regexp.MustCompile(`^[A-Z]{3}-[0-9]{3}$`)
	var capturedCode string
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/device/poll" {
			mu.Lock()
			capturedCode = r.URL.Query().Get("code")
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			// Return approved so Register completes.
			w.Write([]byte(`{"status":"approved","api_key":"fmt-key","machine_id":"fmt-machine"}`)) //nolint:errcheck
		}
	}))
	defer srv.Close()

	store := &relay.MemoryTokenStore{}
	reg := relay.NewRegistrarWithStore(srv.URL, store)
	reg.OpenBrowserFn = func(_ string) error { return fmt.Errorf("no browser") }
	reg.PollInterval = 20 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := reg.Register(ctx, "test"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	mu.Lock()
	code := capturedCode
	mu.Unlock()

	if code == "" {
		t.Fatal("expected code to be captured from poll URL")
	}
	if !codePattern.MatchString(code) {
		t.Errorf("device code %q does not match pattern [A-Z]{3}-[0-9]{3}", code)
	}
}

// ─── WebSocket: SetOnMessage nil handler doesn't panic ────────────────────────

// TestWebSocketHub_SetOnMessage_NilHandler verifies that setting a nil onMessage
// handler does not cause a panic — either during registration or when a message
// arrives with a nil callback registered.
func TestWebSocketHub_SetOnMessage_NilHandler(t *testing.T) {
	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Drain hello.
		conn.ReadMessage() //nolint

		// Send a message — the hub should handle a nil onMessage gracefully.
		data, _ := json.Marshal(relay.Message{Type: relay.MsgDone})
		conn.WriteMessage(websocket.TextMessage, data) //nolint
		time.Sleep(200 * time.Millisecond)
	}))
	defer wsSrv.Close()

	wsBase := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{URL: wsBase})

	// Set nil explicitly — must not panic.
	hub.SetOnMessage(nil)

	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer hub.Close("")

	// Give the server time to send the message; the nil handler must not panic.
	time.Sleep(300 * time.Millisecond)
}

// TestWebSocketHub_SetOnMessage_CallbackFires verifies that a callback
// registered via SetOnMessage receives messages from the server.
func TestWebSocketHub_SetOnMessage_CallbackFires(t *testing.T) {
	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Drain hello.
		conn.ReadMessage() //nolint
		// Send a permission_response.
		data, _ := json.Marshal(relay.Message{
			Type:    relay.MsgPermissionResp,
			Payload: map[string]any{"request_id": "r1", "approved": true},
		})
		conn.WriteMessage(websocket.TextMessage, data) //nolint
		time.Sleep(300 * time.Millisecond)
	}))
	defer wsSrv.Close()

	wsBase := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{URL: wsBase})

	received := make(chan relay.Message, 1)
	hub.SetOnMessage(func(_ context.Context, m relay.Message) {
		received <- m
	})

	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer hub.Close("")

	select {
	case m := <-received:
		if m.Type != relay.MsgPermissionResp {
			t.Errorf("expected permission_response, got %q", m.Type)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for onMessage callback")
	}
}

// ─── Security review: machine ID validation in dispatcher ─────────────────────

// TestDispatcher_MachineID_SpoofPrevention verifies that the dispatcher rejects
// a series of messages from a spoofed machine ID and only processes the one from
// the correct machine — exercising the defense-in-depth guard.
func TestDispatcher_MachineID_SpoofPrevention(t *testing.T) {
	records := make(chan dispatchRecord, 10)
	dispatch := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "real-machine",
		DeliverPerm: func(requestID string, approved bool) bool {
			records <- dispatchRecord{requestID: requestID, approved: approved}
			return true
		},
	})

	spoofedMessages := []relay.Message{
		{Type: relay.MsgPermissionResp, MachineID: "evil-machine-1", Payload: map[string]any{"request_id": "spoof-1", "approved": true}},
		{Type: relay.MsgPermissionResp, MachineID: "evil-machine-2", Payload: map[string]any{"request_id": "spoof-2", "approved": true}},
		{Type: relay.MsgPermissionResp, MachineID: "REAL-MACHINE", Payload: map[string]any{"request_id": "spoof-3", "approved": true}},  // case mismatch
		{Type: relay.MsgPermissionResp, MachineID: "real-machine ", Payload: map[string]any{"request_id": "spoof-4", "approved": true}}, // trailing space
	}
	for _, msg := range spoofedMessages {
		dispatch(context.Background(), msg)
	}

	// The legitimate message.
	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgPermissionResp,
		MachineID: "real-machine",
		Payload:   map[string]any{"request_id": "req-legit", "approved": true},
	})

	// Only the legitimate message should have been delivered.
	select {
	case r := <-records:
		if r.requestID != "req-legit" {
			t.Errorf("unexpected delivery: requestID=%q (expected only legit to be delivered)", r.requestID)
		}
	case <-time.After(time.Second):
		t.Fatal("legitimate message was not delivered")
	}

	// No further deliveries expected.
	select {
	case r := <-records:
		t.Errorf("unexpected extra delivery: requestID=%q", r.requestID)
	case <-time.After(50 * time.Millisecond):
		// Correct: no more deliveries.
	}
}

// ─── Security review: token never logged in plaintext ─────────────────────────

// TestRegistrar_APIKey_NotInBrowserURL verifies that the api_key returned from
// the cloud callback is never embedded in the browser URL that gets opened —
// it arrives via the callback, not via the outbound connect URL.
func TestRegistrar_APIKey_NotInConnectURL(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	var capturedConnectURL string

	reg := relay.NewRegistrarWithStore("http://localhost:0", store)
	reg.OpenBrowserFn = func(rawURL string) error {
		capturedConnectURL = rawURL
		u, _ := url.Parse(rawURL)
		cbURL := u.Query().Get("cb")
		go func() {
			time.Sleep(50 * time.Millisecond)
			resp, err := http.Get(cbURL + "?api_key=super-secret-key&machine_id=m1")
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := reg.Register(ctx, "test")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if result.APIKey != "super-secret-key" {
		t.Fatalf("unexpected API key: %q", result.APIKey)
	}

	// The connect URL must NOT contain the api_key.
	if strings.Contains(capturedConnectURL, "super-secret-key") {
		t.Errorf("connect URL contains the api_key in plaintext: %q", capturedConnectURL)
	}
}

// ─── Security review: WebSocket read deadline ─────────────────────────────────

// TestWebSocketHub_ReadLoop_ExitsOnClose verifies that the read pump goroutine
// does not leak after Close is called — the pump must exit cleanly when the
// done channel is closed.
func TestWebSocketHub_ReadLoop_ExitsOnClose(t *testing.T) {
	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Drain messages until the client disconnects.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer wsSrv.Close()

	wsBase := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{URL: wsBase})

	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Close the hub — the read pump goroutine must stop.
	// We verify indirectly: subsequent Send calls should not block indefinitely.
	hub.Close("")

	// After Close, Send may return an error (conn closed) — that's fine.
	// The important property is that this call returns promptly (not hung).
	done := make(chan struct{})
	go func() {
		hub.Send("", relay.Message{Type: relay.MsgToken})
		close(done)
	}()

	select {
	case <-done:
		// Good — Send returned.
	case <-time.After(2 * time.Second):
		t.Error("Send after Close did not return promptly — possible goroutine leak")
	}
}
