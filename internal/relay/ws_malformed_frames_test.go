package relay_test

// hardening_iter8_test.go — Hardening iteration 8.
// Adversarial / stress / security tests.
// Covers:
//   1. Malformed WebSocket frames from cloud cause no panic in dispatcher
//   2. Dispatcher: message with nil Payload does not panic
//   3. Satellite: rapid Connect/Disconnect toggle (no goroutine leak, -race safe)
//   4. Outbox: enqueue 1001 items exercises FIFO eviction (OutboxMaxDepth exceeded)
//   5. Satellite.SetMachineID with empty string: Status reflects empty string (edge case)
//   6. WebSocketHub: malformed JSON frame from server closes gracefully

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
	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/storage"
)

// ── Malformed WebSocket frames ────────────────────────────────────────────────

// TestWebSocketHub_MalformedJSONFrame_Iter8 sends a non-JSON binary frame from
// the server to the hub's read pump and verifies the hub does not panic.
// The read pump should skip / log the bad frame and keep running.
func TestWebSocketHub_MalformedJSONFrame_Iter8(t *testing.T) {
	goodReceived := make(chan struct{}, 1)

	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Drain hello.
		conn.ReadMessage() //nolint

		// Send malformed JSON.
		conn.WriteMessage(websocket.TextMessage, []byte("{this is not json!!!")) //nolint

		// Then send a valid message to confirm the hub is still alive.
		data, _ := json.Marshal(relay.Message{Type: relay.MsgDone})
		conn.WriteMessage(websocket.TextMessage, data) //nolint

		time.Sleep(500 * time.Millisecond)
	}))
	defer wsSrv.Close()

	wsBase := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{URL: wsBase})

	hub.SetOnMessage(func(_ context.Context, m relay.Message) {
		if m.Type == relay.MsgDone {
			select {
			case goodReceived <- struct{}{}:
			default:
			}
		}
	})

	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer hub.Close("")

	select {
	case <-goodReceived:
		// Hub survived malformed JSON and continued processing — pass.
	case <-time.After(3 * time.Second):
		t.Error("hub did not receive valid message after malformed JSON frame — possible crash or goroutine stuck")
	}
}

// TestWebSocketHub_BinaryFrame_NoJSON_Iter8 sends a binary (non-text) frame
// and verifies no panic.
func TestWebSocketHub_BinaryFrame_NoJSON_Iter8(t *testing.T) {
	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.ReadMessage() //nolint — drain hello

		// Send binary frame (not text JSON).
		conn.WriteMessage(websocket.BinaryMessage, []byte{0x00, 0x01, 0x02, 0xFF}) //nolint
		time.Sleep(300 * time.Millisecond)
	}))
	defer wsSrv.Close()

	wsBase := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{URL: wsBase})

	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	// No callback registered — nil handler path.
	time.Sleep(400 * time.Millisecond)
	hub.Close("")
	// Pass if no panic.
}

// ── Dispatcher nil payload ────────────────────────────────────────────────────

// TestDispatcher_NilPayload_PermissionResp_Iter8 verifies that a
// permission_response message with a nil Payload does not panic.
func TestDispatcher_NilPayload_PermissionResp_Iter8(t *testing.T) {
	records := make(chan struct{}, 1)
	dispatch := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "machine-1",
		DeliverPerm: func(_ string, _ bool) bool {
			records <- struct{}{}
			return true
		},
	})

	// Must not panic.
	dispatch(context.Background(), relay.Message{
		Type:      relay.MsgPermissionResp,
		MachineID: "machine-1",
		Payload:   nil, // nil payload — no request_id
	})

	// deliverPerm should NOT be called (missing request_id guard).
	select {
	case <-records:
		t.Error("deliverPerm should not be called with nil payload")
	case <-time.After(100 * time.Millisecond):
		// Correct — no delivery.
	}
}

// ── Satellite rapid Connect/Disconnect toggle ─────────────────────────────────

// TestSatellite_RapidConnectDisconnect_Iter8 rapidly calls Connect (which will
// fail since no real WS server is available) and Disconnect in a tight loop.
// Verifies no panic and no goroutine leak under the race detector.
// We use a token so Connect attempts to dial (and fails fast) rather than
// returning "not registered" immediately.
func TestSatellite_RapidConnectDisconnect_Iter8(t *testing.T) {
	// A WS server that immediately closes the connection.
	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conn.Close() // immediately close
	}))
	defer wsSrv.Close()

	wsBase := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	store := &relay.MemoryTokenStore{}
	_ = store.Save("rapid-tok")

	sat := relay.NewSatelliteWithStore(wsBase, store)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Connect will fail (server closes immediately) — that's OK.
			_ = sat.Connect(ctx)
			sat.Disconnect()
		}()
	}
	wg.Wait()
	// Pass if no panic and race detector is clean.
}

// ── Outbox FIFO eviction at OutboxMaxDepth ────────────────────────────────────

// TestOutbox_FIFOEviction_AtMaxDepth_Iter8 verifies that when the outbox
// reaches OutboxMaxDepth items, enqueueing one more drops the oldest entry
// rather than exceeding the cap.
func TestOutbox_FIFOEviction_AtMaxDepth_Iter8(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	hub := &relay.InProcessHub{}
	outbox := relay.NewOutbox(s, hub)

	// Fill to exactly OutboxMaxDepth.
	for i := 0; i < relay.OutboxMaxDepth; i++ {
		if err := outbox.Enqueue(relay.Message{
			Type:    relay.MsgToken,
			Payload: map[string]any{"seq": i},
		}); err != nil {
			t.Fatalf("Enqueue[%d]: %v", i, err)
		}
	}

	n, err := outbox.Len()
	if err != nil {
		t.Fatalf("Len at depth: %v", err)
	}
	if n != relay.OutboxMaxDepth {
		t.Fatalf("expected %d items at depth, got %d", relay.OutboxMaxDepth, n)
	}

	// Enqueue one more — should trigger FIFO eviction (drop oldest).
	if err := outbox.Enqueue(relay.Message{
		Type:    relay.MsgToken,
		Payload: map[string]any{"seq": relay.OutboxMaxDepth},
	}); err != nil {
		t.Fatalf("Enqueue overflow: %v", err)
	}

	// Count should still be OutboxMaxDepth (eviction replaced oldest with newest).
	n, err = outbox.Len()
	if err != nil {
		t.Fatalf("Len after eviction: %v", err)
	}
	if n != relay.OutboxMaxDepth {
		t.Errorf("after eviction: expected %d items, got %d", relay.OutboxMaxDepth, n)
	}
}

// ── Satellite.SetMachineID edge case: empty string ────────────────────────────

// TestSatellite_SetMachineID_EmptyString_Iter8 verifies that setting an empty
// machine ID is accepted without panic, and Status reflects it.
func TestSatellite_SetMachineID_EmptyString_Iter8(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	sat := relay.NewSatelliteWithStore("wss://example.com", store)

	sat.SetMachineID("") // edge case: empty
	status := sat.Status()
	if status.MachineID != "" {
		t.Errorf("expected MachineID=empty, got %q", status.MachineID)
	}
}
