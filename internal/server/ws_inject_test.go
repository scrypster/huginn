package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/scrypster/huginn/internal/threadmgr"
)

// dialWS dials the test server's /ws endpoint and returns a connected Conn.
func dialWS(t *testing.T, tsURL string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + tsURL[4:] + "/ws?token=" + testToken
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WS dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// sendInject writes a thread_inject message over the WebSocket.
func sendInject(t *testing.T, conn *websocket.Conn, threadID, content string) {
	t.Helper()
	msg := map[string]any{
		"type": "thread_inject",
		"payload": map[string]any{
			"thread_id": threadID,
			"content":   content,
		},
	}
	data, _ := json.Marshal(msg)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// readWSMsg reads a single message with a 150ms deadline.
// Returns nil if no message arrives (no-op / timeout).
func readWSMsg(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	_, data, err := conn.ReadMessage()
	if err != nil {
		return nil // timeout or closed — expected in noop tests
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return m
}

// TestWSThreadInject_NilTM_IsNoop verifies that thread_inject with no
// ThreadManager wired returns nothing and does not crash.
func TestWSThreadInject_NilTM_IsNoop(t *testing.T) {
	_, ts := newTestServer(t)
	conn := dialWS(t, ts.URL)

	sendInject(t, conn, "t-noop", "hello")

	msg := readWSMsg(t, conn)
	if msg != nil {
		t.Errorf("expected no response for nil TM, got type %q", msg["type"])
	}
}

// TestWSThreadInject_EmptyThreadID_IsNoop verifies that an empty thread_id
// causes an early return with no response.
func TestWSThreadInject_EmptyThreadID_IsNoop(t *testing.T) {
	srv, ts := newTestServer(t)
	tm := threadmgr.New()
	srv.SetThreadManager(tm)

	conn := dialWS(t, ts.URL)
	sendInject(t, conn, "", "content")

	msg := readWSMsg(t, conn)
	if msg != nil {
		t.Errorf("expected no response for empty thread_id, got type %q", msg["type"])
	}
}

// TestWSThreadInject_UnknownThread_SendsError verifies unknown thread IDs
// return a structured thread_inject_error (reason=not_found).
func TestWSThreadInject_UnknownThread_SendsError(t *testing.T) {
	srv, ts := newTestServer(t)
	tm := threadmgr.New()
	srv.SetThreadManager(tm)

	conn := dialWS(t, ts.URL)
	sendInject(t, conn, "thread-does-not-exist", "hello")

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var resp map[string]any
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: expected thread_inject_error, got error: %v", err)
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			continue
		}
		if resp["type"] == "thread_inject_error" {
			break
		}
	}

	if resp["type"] != "thread_inject_error" {
		t.Errorf("expected type thread_inject_error, got %q", resp["type"])
	}
	payload, _ := resp["payload"].(map[string]any)
	if payload["reason"] != "not_found" {
		t.Errorf("error payload reason = %v, want %q", payload["reason"], "not_found")
	}
}

// TestWSThreadInject_NotBlocked_SendsError verifies that thread input is
// accepted only while blocked; otherwise reason=not_waiting.
func TestWSThreadInject_NotBlocked_SendsError(t *testing.T) {
	srv, ts := newTestServer(t)
	tm := threadmgr.New()
	srv.SetThreadManager(tm)

	thread, err := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-inject",
		AgentID:   "agent-x",
		Task:      "test inject",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	conn := dialWS(t, ts.URL)
	sendInject(t, conn, thread.ID, "please check this")

	// Must receive thread_inject_error (not_waiting)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var resp map[string]any
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read: expected thread_inject_error, got error: %v", err)
		}
		if err := json.Unmarshal(data, &resp); err != nil {
			continue
		}
		if resp["type"] == "thread_inject_error" {
			break
		}
		// Skip unrelated messages (e.g. pong)
	}

	if resp["type"] != "thread_inject_error" {
		t.Errorf("expected type thread_inject_error, got %q", resp["type"])
	}
	payload, _ := resp["payload"].(map[string]any)
	if payload["thread_id"] != thread.ID {
		t.Errorf("error payload thread_id = %v, want %q", payload["thread_id"], thread.ID)
	}
	if payload["reason"] != "not_waiting" {
		t.Errorf("error payload reason = %v, want %q", payload["reason"], "not_waiting")
	}
}

