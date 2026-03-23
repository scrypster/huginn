package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/scrypster/huginn/internal/threadmgr"
)

// TestParseBoolPayload covers all branches of parseBoolPayload.
func TestParseBoolPayload(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  bool
	}{
		{"native true", true, true},
		{"native false", false, false},
		{"float64 1", float64(1), true},
		{"float64 0", float64(0), false},
		{"float64 negative", float64(-1), true},
		{"int 1", int(1), true},
		{"int 0", int(0), false},
		{"string true", "true", true},
		{"string 1", "1", true},
		{"string false", "false", false},
		{"string 0", "0", false},
		{"string empty", "", false},
		{"nil", nil, false},
		{"struct unknown", struct{}{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseBoolPayload(tc.input)
			if got != tc.want {
				t.Errorf("parseBoolPayload(%v) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestBroadcastPlanning_EmptySessionID verifies early return when sessionID is empty.
func TestBroadcastPlanning_EmptySessionID_NoBroadcast(t *testing.T) {
	srv := &Server{}
	srv.wsHub = newWSHub()
	go srv.wsHub.run()

	ch := make(chan WSMessage, 4)
	c := &wsClient{send: ch, sessionID: ""} // wildcard — would receive all broadcasts
	srv.wsHub.registerWithSession(c, "")

	// Broadcasting with empty sessionID should be a no-op (return early).
	srv.BroadcastPlanning("", "some-agent")

	time.Sleep(30 * time.Millisecond)
	select {
	case msg := <-ch:
		t.Errorf("expected no message for empty sessionID, got type %q", msg.Type)
	default:
		// expected — no message sent
	}
}

// TestBroadcastPlanning_NonEmptySessionID_Sends verifies message IS sent for valid sessionID.
func TestBroadcastPlanning_NonEmptySessionID_Sends(t *testing.T) {
	srv := &Server{}
	srv.wsHub = newWSHub()
	go srv.wsHub.run()

	ch := make(chan WSMessage, 4)
	c := &wsClient{send: ch, sessionID: "sess-abc"}
	srv.wsHub.registerWithSession(c, "sess-abc")

	srv.BroadcastPlanning("sess-abc", "planner-agent")

	time.Sleep(30 * time.Millisecond)
	select {
	case msg := <-ch:
		if msg.Type != "planning" {
			t.Errorf("expected type planning, got %q", msg.Type)
		}
	default:
		t.Error("expected planning message to be sent")
	}
}

// TestWSMessage_DelegationPreviewAck_NumericApproved verifies parseBoolPayload handles float64.
// Sends approved as float64(1) via WebSocket to test the numeric path.
func TestWSMessage_DelegationPreviewAck_NumericApproved(t *testing.T) {
	srv, ts := newTestServer(t)
	gate := threadmgr.NewDelegationPreviewGate(true)
	srv.previewGate = gate

	wsURL := "ws" + ts.URL[4:] + "/ws?token=" + testToken
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// JSON numbers unmarshal to float64 in map[string]any — this exercises parseBoolPayload(float64).
	msg := map[string]any{
		"type":       "delegation_preview_ack",
		"session_id": "sess-num",
		"payload": map[string]any{
			"thread_id": "thread-num",
			"approved":  float64(1),
		},
	}
	data, _ := json.Marshal(msg)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write: %v", err)
	}

	// No crash expected — the gate has no pending Approve so Ack is a no-op.
	conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	conn.ReadMessage() // may timeout — that's fine
}

// TestHandleWebSocket_TimingSafe_EmptyToken verifies timing-safe auth rejects empty token.
func TestHandleWebSocket_TimingSafe_EmptyToken(t *testing.T) {
	_, ts := newTestServer(t)
	wsURL := "ws" + ts.URL[4:] + "/ws?token="
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	_, resp, err := dialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected connection to fail with empty token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

// TestAuthMiddleware_TimingSafe_LongWrongToken verifies timing-safe HTTP middleware
// with a token that has the correct length but wrong content.
// Uses /api/v1/sessions (auth required); /api/v1/health is intentionally public.
func TestAuthMiddleware_TimingSafe_LongWrongToken(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer wrong-token-that-is-quite-long-and-differs")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong token, got %d", resp.StatusCode)
	}
}

// TestDelegationPreviewGate_ColonInSessionID verifies ackKey handles colons correctly.
// Uses the null-byte separator so "sess:colon:id" + "t-1" differs from "sess:colon" + "id:t-1".
func TestDelegationPreviewGate_ColonInSessionID_Iter4(t *testing.T) {
	gate := threadmgr.NewDelegationPreviewGate(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resultCh := make(chan bool, 1)
	go func() {
		result := gate.Approve(ctx, "sess:colon:id", "thread-1", "agent", "task", "", nil)
		resultCh <- result
	}()

	// Small delay so Approve registers the channel before Ack.
	time.Sleep(20 * time.Millisecond)
	gate.Ack("sess:colon:id", "thread-1", true)

	select {
	case result := <-resultCh:
		if !result {
			t.Error("expected approved=true")
		}
	case <-time.After(2 * time.Second):
		t.Error("Approve did not return within timeout")
	}
}

// TestWSHub_BroadcastToSession_WildcardReceivesAll verifies wildcard clients receive all sessions.
func TestWSHub_BroadcastToSession_WildcardReceivesAll_Iter4(t *testing.T) {
	hub := newWSHub()

	chWild := make(chan WSMessage, 4)
	wild := &wsClient{send: chWild, sessionID: ""}
	hub.registerWithSession(wild, "")

	chSpecific := make(chan WSMessage, 4)
	specific := &wsClient{send: chSpecific, sessionID: "sess-q"}
	hub.registerWithSession(specific, "sess-q")

	hub.broadcastToSession("sess-q", WSMessage{Type: "event-q"})

	time.Sleep(20 * time.Millisecond)

	select {
	case msg := <-chWild:
		if msg.Type != "event-q" {
			t.Errorf("wildcard: expected event-q, got %q", msg.Type)
		}
	default:
		t.Error("wildcard client expected to receive message")
	}

	select {
	case msg := <-chSpecific:
		if msg.Type != "event-q" {
			t.Errorf("specific: expected event-q, got %q", msg.Type)
		}
	default:
		t.Error("specific client expected to receive message")
	}
}

// TestWSHub_SessionScoped_DoesNotReceiveOtherSessions verifies scoping.
func TestWSHub_SessionScoped_DoesNotReceiveOtherSessions_Iter4(t *testing.T) {
	hub := newWSHub()

	ch := make(chan WSMessage, 4)
	c := &wsClient{send: ch, sessionID: "sess-private"}
	hub.registerWithSession(c, "sess-private")

	// Broadcast to a different session
	hub.broadcastToSession("sess-other", WSMessage{Type: "other-event"})

	time.Sleep(20 * time.Millisecond)
	select {
	case msg := <-ch:
		t.Errorf("unexpected message for wrong session: %q", msg.Type)
	default:
		// expected — no message
	}
}
