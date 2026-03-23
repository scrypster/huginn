package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestWSRateLimit_ErrorFrameOnExcess verifies that a WebSocket client that sends
// more than wsRateLimitMsgs messages in wsRateLimitWindow receives an error frame
// rather than having the message silently dropped.
func TestWSRateLimit_ErrorFrameOnExcess(t *testing.T) {
	t.Parallel()

	_, ts := newTestServer(t)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?token=" + testToken

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Set a read deadline so the test doesn't hang forever.
	conn.SetReadDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck

	// Send wsRateLimitMsgs+1 ping messages in rapid succession within the window.
	// The first wsRateLimitMsgs should be handled normally (pong returned).
	// At least one subsequent message should trigger an error frame.
	totalToSend := wsRateLimitMsgs + 5

	for i := 0; i < totalToSend; i++ {
		data, _ := json.Marshal(WSMessage{Type: "ping"})
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			// Connection may be closed by server after many messages — that's fine.
			break
		}
	}

	// Drain responses, looking for an error frame.
	gotError := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)) //nolint:errcheck
		_, respData, err := conn.ReadMessage()
		if err != nil {
			// Timeout or closed — stop reading.
			break
		}
		var msg WSMessage
		if jsonErr := json.Unmarshal(respData, &msg); jsonErr != nil {
			continue
		}
		if msg.Type == "error" && strings.Contains(msg.Content, "rate limit") {
			gotError = true
			break
		}
	}

	if !gotError {
		t.Error("expected at least one error frame with 'rate limit' content after exceeding WS rate limit")
	}
}

// TestWSRateLimit_MetricCounterIncremented verifies that the wsRateLimitExceeded
// atomic counter on the server is incremented when a client exceeds the rate limit.
func TestWSRateLimit_MetricCounterIncremented(t *testing.T) {
	t.Parallel()

	srv, ts := newTestServer(t)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?token=" + testToken

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck

	// Send enough messages to exceed the rate limit.
	for i := 0; i < wsRateLimitMsgs+3; i++ {
		data, _ := json.Marshal(WSMessage{Type: "ping"})
		if wErr := conn.WriteMessage(websocket.TextMessage, data); wErr != nil {
			break
		}
	}

	// Drain responses briefly to let the server process the messages.
	drain := time.Now().Add(2 * time.Second)
	for time.Now().Before(drain) {
		conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond)) //nolint:errcheck
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}

	// The metric counter must be > 0.
	if count := srv.WSRateLimitExceeded(); count == 0 {
		t.Error("expected wsRateLimitExceeded counter > 0 after client exceeded rate limit")
	}
}

// TestWSRateLimit_WindowResetAllowsNewMessages verifies that after the rate-limit
// window resets, the client can send messages again without triggering rate limits.
func TestWSRateLimit_WindowResetAllowsNewMessages(t *testing.T) {
	t.Parallel()

	_, ts := newTestServer(t)
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws?token=" + testToken

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Exhaust the rate limit within the window.
	for i := 0; i < wsRateLimitMsgs+1; i++ {
		data, _ := json.Marshal(WSMessage{Type: "ping"})
		_ = conn.WriteMessage(websocket.TextMessage, data)
	}

	// Wait for the window to expire (wsRateLimitWindow + a small buffer).
	time.Sleep(wsRateLimitWindow + 100*time.Millisecond)

	// Now send a single ping — it should receive a pong (not a rate-limit error).
	data, _ := json.Marshal(WSMessage{Type: "ping"})
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write after window reset: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(3 * time.Second)) //nolint:errcheck
	deadline := time.Now().Add(3 * time.Second)
	gotPong := false
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond)) //nolint:errcheck
		_, respData, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var msg WSMessage
		if jsonErr := json.Unmarshal(respData, &msg); jsonErr != nil {
			continue
		}
		if msg.Type == "pong" {
			gotPong = true
			break
		}
	}

	if !gotPong {
		t.Error("expected pong after rate-limit window reset, but did not receive one")
	}
}

// TestWSRateAllow_FixedWindowLogic unit-tests the wsClient.wsRateAllow method
// directly without going through the full HTTP/WS stack.
func TestWSRateAllow_FixedWindowLogic(t *testing.T) {
	t.Parallel()

	c := &wsClient{msgWindowStart: time.Now()}

	// First wsRateLimitMsgs calls must all return true.
	for i := 0; i < wsRateLimitMsgs; i++ {
		if !c.wsRateAllow() {
			t.Fatalf("iteration %d: expected allow=true", i)
		}
	}

	// The next call (wsRateLimitMsgs+1) must return false.
	if c.wsRateAllow() {
		t.Fatal("expected allow=false after limit exhausted within window")
	}
}

// TestWSRateAllow_WindowReset verifies the window-reset branch of wsRateAllow.
func TestWSRateAllow_WindowReset(t *testing.T) {
	t.Parallel()

	// Start with an ancient window so the reset path is taken immediately.
	c := &wsClient{msgWindowStart: time.Now().Add(-wsRateLimitWindow - time.Second)}

	// Any call should trigger a window reset and return true.
	if !c.wsRateAllow() {
		t.Fatal("expected allow=true after window reset")
	}
}
