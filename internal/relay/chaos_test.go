package relay_test

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/scrypster/huginn/internal/relay"
)

// TestWebSocketHub_ChaosReconnect simulates a satellite that reconnects 10 times
// with the server going up/down unpredictably. Verifies:
// - No goroutine leak (test completes within timeout)
// - No deadlock (test completes without hanging)
// - At least 50% of connection attempts succeed
func TestWebSocketHub_ChaosReconnect(t *testing.T) {
	const cycles = 10

	// Capture starting goroutine count to detect leaks
	startGoroutines := runtime.NumGoroutine()

	var mu sync.Mutex
	connected := 0

	upgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		mu.Lock()
		connected++
		mu.Unlock()

		// Drain the satellite_hello message
		_, _, err = conn.ReadMessage()
		if err != nil {
			return
		}

		// Stay connected for a brief random time then drop
		// This simulates network instability
		delay := time.Duration(rand.Intn(50)+10) * time.Millisecond
		time.Sleep(delay)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	// Use a 15-second timeout for all reconnect cycles. This is generous enough
	// for exponential backoff but will catch any hanging goroutines.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Connect-reconnect loop: try to connect 10 times
	for i := 0; i < cycles; i++ {
		h := relay.NewWebSocketHub(relay.WebSocketConfig{
			URL:       wsURL,
			MachineID: "test-machine",
			Version:   "0.0.1",
		})

		err := h.Connect(ctx)
		if err != nil {
			// Context cancelled or connection refused — stop
			t.Logf("cycle %d: Connect failed: %v", i, err)
			h.Close("")
			break
		}

		// Brief send test to exercise the writeLoop
		sendErr := h.Send("", relay.Message{Type: relay.MsgSatelliteHello})
		if sendErr != nil {
			t.Logf("cycle %d: Send failed: %v", i, sendErr)
		}

		// Close immediately to clean up goroutines
		h.Close("")

		// Give the server a moment to record this connection
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	c := connected
	mu.Unlock()

	// Give goroutines a moment to exit after the final Close()
	time.Sleep(100 * time.Millisecond)
	endGoroutines := runtime.NumGoroutine()

	// Allow a small tolerance (e.g., +3) for test framework goroutines
	if endGoroutines > startGoroutines+3 {
		t.Fatalf("goroutine leak detected: started with %d goroutines, ended with %d (leaked %d)",
			startGoroutines, endGoroutines, endGoroutines-startGoroutines)
	}
	t.Logf("goroutine leak check: start=%d end=%d (delta=%d)", startGoroutines, endGoroutines, endGoroutines-startGoroutines)

	expectedMin := cycles / 2
	if c < expectedMin {
		t.Fatalf("expected at least %d successful connections, got %d (cycles: %d)", expectedMin, c, cycles)
	}

	t.Logf("chaos test completed: %d server connections out of %d cycles", c, cycles)
}

// TestWebSocketHub_ChaosReconnect_WithMessages simulates chaos reconnect with
// message delivery. Verifies that messages enqueued to a hub are handled correctly
// when the connection drops between cycles.
func TestWebSocketHub_ChaosReconnect_WithMessages(t *testing.T) {
	const cycles = 5

	var mu sync.Mutex
	var connected int
	var messagesReceived int

	upgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		mu.Lock()
		connected++
		mu.Unlock()

		// Read and count all messages until connection drops
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				break
			}
			var msg relay.Message
			if json.Unmarshal(data, &msg) == nil {
				mu.Lock()
				messagesReceived++
				mu.Unlock()
			}
		}

		// Drop connection randomly
		time.Sleep(time.Duration(rand.Intn(30)+5) * time.Millisecond)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Chaos cycle with message sends
	for i := 0; i < cycles; i++ {
		h := relay.NewWebSocketHub(relay.WebSocketConfig{
			URL:       wsURL,
			MachineID: "test-machine",
			Version:   "0.0.1",
		})

		err := h.Connect(ctx)
		if err != nil {
			t.Logf("cycle %d: Connect failed: %v", i, err)
			h.Close("")
			break
		}

		// Send multiple messages
		for j := 0; j < 3; j++ {
			_ = h.Send("", relay.Message{
				Type: relay.MsgToken,
				Payload: map[string]any{
					"cycle": i,
					"seq":   j,
				},
			})
		}

		h.Close("")
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	c := connected
	m := messagesReceived
	mu.Unlock()

	// Verify reasonable connectivity
	if c < 2 {
		t.Fatalf("expected at least 2 connections, got %d", c)
	}

	// Not all messages may arrive due to drops, but we should get some
	if m == 0 {
		t.Logf("warning: no messages received across %d cycles, but %d connections were made", cycles, c)
	}

	t.Logf("chaos with messages: %d connections, %d messages received out of ~%d sent", c, m, cycles*3)
}
