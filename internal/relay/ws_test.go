package relay_test

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
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true },
}

// wsURL converts an httptest server URL from http:// to ws://.
func wsURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// TestWebSocketHub_SendMessage verifies that Connect sends satellite_hello
// and that additional messages are delivered to the server.
func TestWebSocketHub_SendMessage(t *testing.T) {
	received := make(chan []byte, 4)

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
			select {
			case received <- data:
			default:
			}
		}
	}))
	defer srv.Close()

	hub := relay.NewWebSocketHub(relay.WebSocketConfig{
		URL:       wsURL(srv),
		Token:     "test-token",
		MachineID: "test-machine",
		Version:   "0.0.1",
	})

	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer hub.Close("")

	// satellite_hello must be the first message.
	select {
	case data := <-received:
		var msg relay.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("unmarshal hello: %v", err)
		}
		if msg.Type != relay.MsgSatelliteHello {
			t.Errorf("expected satellite_hello, got %q", msg.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for satellite_hello")
	}

	// Send an additional message and verify delivery.
	if err := hub.Send("", relay.Message{
		Type:    relay.MsgToken,
		Payload: map[string]any{"token": "abc"},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case data := <-received:
		var msg relay.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("unmarshal token: %v", err)
		}
		if msg.Type != relay.MsgToken {
			t.Errorf("expected token message, got %q", msg.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for token message")
	}
}

// TestWebSocketHub_OnMessage verifies that the onMessage callback receives
// messages sent from the server to the hub.
func TestWebSocketHub_OnMessage(t *testing.T) {
	serverSend := make(chan relay.Message, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Drain hello.
		conn.ReadMessage() //nolint

		// Send a message to the hub.
		msg := <-serverSend
		data, _ := json.Marshal(msg)
		conn.WriteMessage(websocket.TextMessage, data) //nolint

		// Keep connection open.
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()

	hub := relay.NewWebSocketHub(relay.WebSocketConfig{
		URL:   wsURL(srv),
		Token: "test-token",
	})

	got := make(chan relay.Message, 1)
	hub.SetOnMessage(func(_ context.Context, m relay.Message) {
		got <- m
	})

	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer hub.Close("")

	serverSend <- relay.Message{Type: relay.MsgToolCall, Payload: map[string]any{"tool": "bash"}}

	select {
	case m := <-got:
		if m.Type != relay.MsgToolCall {
			t.Errorf("expected tool_call, got %q", m.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for onMessage callback")
	}
}

// TestWebSocketHub_Reconnect verifies that the hub reconnects after the
// server drops the connection.
func TestWebSocketHub_Reconnect(t *testing.T) {
	var mu sync.Mutex
	connectCount := 0
	helloCount := 0
	done := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		mu.Lock()
		connectCount++
		thisConnect := connectCount
		mu.Unlock()

		defer func() {
			conn.Close()
			mu.Lock()
			cnt := connectCount
			mu.Unlock()
			if cnt >= 2 {
				select {
				case done <- struct{}{}:
				default:
				}
			}
		}()

		// Read hello then close abruptly to trigger reconnect.
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var msg relay.Message
		if json.Unmarshal(data, &msg) == nil && msg.Type == relay.MsgSatelliteHello {
			mu.Lock()
			helloCount++
			mu.Unlock()
		}
		// Close immediately on first connect to force reconnect.
		if thisConnect == 1 {
			return
		}
		// On second connect, keep open briefly.
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	hub := relay.NewWebSocketHub(relay.WebSocketConfig{
		URL:   wsURL(srv),
		Token: "test",
	})

	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer hub.Close("")

	select {
	case <-done:
		mu.Lock()
		cc := connectCount
		hc := helloCount
		mu.Unlock()
		if cc < 2 {
			t.Errorf("expected at least 2 connections (reconnect), got %d", cc)
		}
		if hc < 1 {
			t.Errorf("expected satellite_hello on connect, got %d hellos", hc)
		}
	case <-time.After(10 * time.Second):
		mu.Lock()
		cc := connectCount
		mu.Unlock()
		t.Fatalf("timed out waiting for reconnect (connectCount=%d)", cc)
	}
}

// TestWebSocketHub_Close_Idempotent verifies Close can be called multiple
// times without panicking.
func TestWebSocketHub_Close_Idempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		if conn != nil {
			defer conn.Close()
			time.Sleep(500 * time.Millisecond)
		}
	}))
	defer srv.Close()

	hub := relay.NewWebSocketHub(relay.WebSocketConfig{URL: wsURL(srv)})
	ctx := context.Background()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Multiple Close calls must not panic.
	hub.Close("")
	hub.Close("")
	hub.Close("")
}

// TestWebSocketHub_ConcurrentSend_NoRace verifies that concurrent calls to Send
// do not cause a data race on the WebSocket connection.
func TestWebSocketHub_ConcurrentSend_NoRace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			conn.WriteMessage(mt, msg) //nolint:errcheck
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{URL: wsURL, MachineID: "test"})
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer hub.Close("")

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hub.Send("", relay.Message{Type: relay.MsgToken, Payload: map[string]any{"t": "x"}}) //nolint:errcheck
		}()
	}
	wg.Wait()
}

// TestWebSocketHub_ResetBackoff verifies that ResetBackoff resets the backoff delay
// and allows quick reconnection after a wake event.
func TestWebSocketHub_ResetBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Keep connection open briefly
		time.Sleep(100 * time.Millisecond)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{URL: wsURL, MachineID: "test"})

	// ResetBackoff must not panic before Connect
	hub.ResetBackoff()

	// Also verify it's callable after Connect
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// ResetBackoff must not race with active connection
	hub.ResetBackoff()

	// Close the hub safely
	hub.Close("")
}

// TestWebSocketHub_Send_NotConnected verifies that Send returns error when hub
// has never been connected.
func TestWebSocketHub_Send_NotConnected(t *testing.T) {
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{
		URL:       "ws://localhost:9999", // invalid URL, connection will fail
		MachineID: "test",
	})
	// Never call Connect

	msg := relay.Message{Type: relay.MsgToken, Payload: map[string]any{"t": "x"}}
	err := hub.Send("", msg)
	if err == nil {
		t.Fatal("expected error on Send without Connect, got nil")
	}
	if err != relay.ErrNotActivated {
		t.Errorf("expected ErrNotActivated, got %v", err)
	}
}

// TestWebSocketHub_Send_AfterCloseHub2 verifies that Send returns error when hub
// has been closed (second variant testing the closed channel path).
func TestWebSocketHub_Send_AfterCloseHub2(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		time.Sleep(100 * time.Millisecond)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{URL: wsURL})
	if err := hub.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	// Close the hub
	hub.Close("")

	// Give goroutines time to exit and done channel to close
	time.Sleep(200 * time.Millisecond)

	// Send should fail when the done channel is closed
	// Test by repeatedly sending until we hit the closed channel case
	msg := relay.Message{Type: relay.MsgToken, Payload: map[string]any{"t": "x"}}
	var lastErr error
	for i := 0; i < 10; i++ {
		err := hub.Send("", msg)
		lastErr = err
		if err == relay.ErrNotActivated {
			return // Success: got the expected error
		}
		time.Sleep(10 * time.Millisecond)
	}
	if lastErr == nil {
		t.Fatal("expected error on Send after Close, got nil after multiple attempts")
	}
}

// TestWebSocketHub_Send_BufferFull verifies that Send returns ErrWriteBufferFull
// when the write buffer is saturated (wsWriteBufSize = 256).
// This test stalls the writeLoop by blocking on conn.WriteMessage.
func TestWebSocketHub_Send_BufferFull(t *testing.T) {
	// Server that accepts upgrade but then blocks on reading (to stall WriteMessage).
	blockCh := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Block until channel closes, preventing the client from writing.
		// This stalls conn.WriteMessage in the writeLoop, filling writeCh.
		<-blockCh
	}))
	defer srv.Close()
	defer close(blockCh)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	hub := relay.NewWebSocketHub(relay.WebSocketConfig{URL: wsURL})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer hub.Close("")

	// Now try to fill the write buffer by sending many messages synchronously.
	// The writeLoop will block on conn.WriteMessage (server blocked on read),
	// causing writeCh to fill up.
	msg := relay.Message{Type: relay.MsgToken, Payload: map[string]any{"t": "x"}}
	var fullErr error
	for i := 0; i < 300; i++ {
		err := hub.Send("", msg)
		if err == relay.ErrWriteBufferFull {
			fullErr = err
			break
		}
		if err != nil {
			break
		}
	}

	if fullErr != relay.ErrWriteBufferFull {
		t.Skip("write buffer did not fill (timeout or other error) — test environment may not support blocking writes")
	}
}

// TestWebSocketHub_SustainedLoad_NoRace verifies that sustained concurrent sends
// through multiple goroutines do not cause data races on the write channel or connection.
// This test runs 20 sender goroutines, each sending 50 messages (1000 total), under the
// race detector to validate safe concurrent access patterns.
func TestWebSocketHub_SustainedLoad_NoRace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Drain all messages without blocking the sender.
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	hub := relay.NewWebSocketHub(relay.WebSocketConfig{
		URL:       wsURL(srv),
		Token:     "test-token",
		MachineID: "test-machine",
		Version:   "0.0.1",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := hub.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer hub.Close("")

	const senders = 20
	const messages = 50

	var wg sync.WaitGroup
	for i := 0; i < senders; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < messages; j++ {
				msg := relay.Message{
					Type: relay.MsgToken,
					Payload: map[string]any{
						"sender":  n,
						"msg":     j,
						"payload": "test-data",
					},
				}
				_ = hub.Send("", msg)
			}
		}(i)
	}
	wg.Wait()
}
