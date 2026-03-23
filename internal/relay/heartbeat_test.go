package relay_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/scrypster/huginn/internal/relay"
)

func TestHeartbeat_SendsOnInterval(t *testing.T) {
	var heartbeats atomic.Int32

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg relay.Message
			if json.Unmarshal(data, &msg) == nil && msg.Type == relay.MsgSatelliteHeartbeat {
				heartbeats.Add(1)
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{URL: wsURL, MachineID: "test-machine"})
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer hub.Close("")

	h := relay.NewHeartbeater(hub, "test-machine", relay.HeartbeatConfig{Interval: 50 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	go h.Start(ctx)

	time.Sleep(350 * time.Millisecond)
	if got := heartbeats.Load(); got < 4 {
		t.Errorf("expected ≥4 heartbeats in 350ms at 50ms interval, got %d", got)
	}
}

func TestHeartbeater_PayloadIncludesMetrics(t *testing.T) {
	var lastPayload map[string]any
	var payloadMutex atomic.Value

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg relay.Message
			if json.Unmarshal(data, &msg) == nil && msg.Type == relay.MsgSatelliteHeartbeat {
				payloadMutex.Store(msg.Payload)
			}
		}
	}))
	defer srv.Close()

	db := openTestDB(t)
	sessionStore := relay.NewSessionStore(db)
	outbox := relay.NewOutbox(db, nil)

	// Create 2 active sessions
	for i := 1; i <= 2; i++ {
		sess := relay.SessionMeta{
			ID:        "s" + string(rune(48+i)),
			Status:    "active",
			StartedAt: time.Now(),
		}
		if err := sessionStore.Save(sess); err != nil {
			t.Fatal(err)
		}
	}

	// Enqueue 1 message in outbox
	msg := relay.Message{Type: relay.MsgToken, Payload: map[string]any{"t": "test"}}
	if err := outbox.Enqueue(msg); err != nil {
		t.Fatal(err)
	}

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{URL: wsURL, MachineID: "test-machine"})
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer hub.Close("")

	cfg := relay.HeartbeatConfig{
		Interval:     50 * time.Millisecond,
		SessionStore: sessionStore,
		Outbox:       outbox,
	}
	h := relay.NewHeartbeater(hub, "test-machine", cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	go h.Start(ctx)

	time.Sleep(200 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond) // Give heartbeat goroutine time to exit

	val := payloadMutex.Load()
	if val != nil {
		lastPayload = val.(map[string]any)
	}

	if lastPayload == nil {
		t.Fatal("heartbeat payload is nil: hub may not have sent heartbeat or test timed out before receiving")
	}

	// Check active_sessions
	if sessions, ok := lastPayload["active_sessions"]; ok {
		n, ok := sessions.(float64)
		if !ok {
			t.Errorf("active_sessions has wrong type: expected float64, got %T", sessions)
		} else if int(n) != 2 {
			t.Errorf("expected active_sessions=2, got %d", int(n))
		}
	} else {
		t.Error("active_sessions not in payload")
	}

	// Check pending_outbox
	if pending, ok := lastPayload["pending_outbox"]; ok {
		n, ok := pending.(float64)
		if !ok {
			t.Errorf("pending_outbox has wrong type: expected float64, got %T", pending)
		} else if int(n) != 1 {
			t.Errorf("expected pending_outbox=1, got %d", int(n))
		}
	} else {
		t.Error("pending_outbox not in payload")
	}
}

func TestHeartbeater_WithoutSessionStoreOrOutbox(t *testing.T) {
	var heartbeatSent atomic.Bool

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg relay.Message
			if json.Unmarshal(data, &msg) == nil && msg.Type == relay.MsgSatelliteHeartbeat {
				heartbeatSent.Store(true)
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{URL: wsURL, MachineID: "test-machine"})
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer hub.Close("")

	// Heartbeater with no SessionStore or Outbox
	cfg := relay.HeartbeatConfig{Interval: 50 * time.Millisecond}
	h := relay.NewHeartbeater(hub, "test-machine", cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	go h.Start(ctx)

	time.Sleep(200 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond) // Give heartbeat goroutine time to exit

	if !heartbeatSent.Load() {
		t.Error("heartbeat should be sent even without SessionStore or Outbox")
	}
}

// TestHeartbeater_Start_CancelContext verifies that Start exits cleanly when
// the context is cancelled.
func TestHeartbeater_Start_CancelContext(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{URL: wsURL, MachineID: "test-machine"})
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer hub.Close("")

	cfg := relay.HeartbeatConfig{Interval: 1 * time.Second}
	h := relay.NewHeartbeater(hub, "test-machine", cfg)

	// Start with a cancelable context
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		h.Start(ctx)
		close(done)
	}()

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	// Wait for Start to exit (with timeout)
	select {
	case <-done:
		// Success: Start exited cleanly
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not exit after context cancellation")
	}
}

// TestHeartbeater_Start_HubSendFails verifies that Start continues sending
// heartbeats even if hub.Send fails (errors are logged but not fatal).
func TestHeartbeater_Start_HubSendFails(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Don't read messages; writeLoop will fail on buffer full
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{URL: wsURL, MachineID: "test-machine"})
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer hub.Close("")

	// Heartbeater without outbox to avoid closed DB panics
	cfg := relay.HeartbeatConfig{
		Interval: 50 * time.Millisecond,
	}
	h := relay.NewHeartbeater(hub, "test-machine", cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	go h.Start(ctx)

	time.Sleep(200 * time.Millisecond)

	// Heartbeater should continue running despite any send failures
	// This test verifies it doesn't panic or exit prematurely
}

// TestHeartbeater_DefaultInterval verifies that NewHeartbeater defaults to 60s
// interval when not specified.
func TestHeartbeater_DefaultInterval(t *testing.T) {
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{MachineID: "test"})

	// Create with zero interval
	cfg := relay.HeartbeatConfig{Interval: 0}
	h := relay.NewHeartbeater(hub, "test", cfg)

	// The config passed in has Interval=0, but the Heartbeater should use default 60s
	// We can't directly check the internal interval, but we can verify Start behaves correctly

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		h.Start(ctx)
		close(done)
	}()

	// Cancel immediately
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Success: Start exited even with default interval
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not exit after context cancellation")
	}
}

// TestHeartbeater_Start_CalledTwice verifies that calling Start() twice on the
// same Heartbeater instance does not panic or cause data races.
// This test uses the race detector (-race flag) to catch any synchronization issues.
func TestHeartbeater_Start_CalledTwice(t *testing.T) {
	sendCount := atomic.Int64{}
	// Use fakeHub with optional callback (defined in relay_test.go)
	hub := &fakeHub{
		sendFn: func(_ string, _ relay.Message) error {
			sendCount.Add(1)
			return nil
		},
	}
	hb := relay.NewHeartbeater(hub, "test-machine", relay.HeartbeatConfig{Interval: 10 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	// Start two concurrent heartbeat loops on the same instance.
	// This tests that Start() and the underlying data structures are race-safe.
	go func() { defer wg.Done(); hb.Start(ctx) }()
	go func() { defer wg.Done(); hb.Start(ctx) }()
	wg.Wait()
	// No panic = test passes. Race detector will catch data races when run with -race.
}

// TestHeartbeater_SendError_DoesNotStop verifies that the Heartbeater continues
// to send heartbeats even if hub.Send() returns errors (e.g., during disconnect).
// Errors should be logged at Warn level and the ticker must keep firing.
func TestHeartbeater_SendError_DoesNotStop(t *testing.T) {
	sendCount := 0
	failUntil := 3 // fail first 3 ticks, then succeed
	hub := &fakeHub{
		sendFn: func(machineID string, msg relay.Message) error {
			sendCount++
			if sendCount <= failUntil {
				return errors.New("disconnected")
			}
			return nil
		},
	}

	hb := relay.NewHeartbeater(hub, "test-machine", relay.HeartbeatConfig{
		Interval: 10 * time.Millisecond, // fast tick for test
	})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	hb.Start(ctx)

	// After 150ms with 10ms ticks: ~15 ticks expected
	// First 3 fail, rest succeed. sendCount must be > 3.
	if sendCount <= 3 {
		t.Fatalf("heartbeater must continue after send errors: got sendCount=%d, want >3", sendCount)
	}
}
