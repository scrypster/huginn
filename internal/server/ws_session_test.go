package server

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
)

func TestWSHubBroadcastToSession(t *testing.T) {
	hub := newWSHub()
	go hub.run()

	// Register two clients for different sessions
	c1 := &wsClient{send: make(chan WSMessage, 4), ctx: context.Background()}
	c2 := &wsClient{send: make(chan WSMessage, 4), ctx: context.Background()}

	hub.registerWithSession(c1, "session-A")
	hub.registerWithSession(c2, "session-B")

	// Broadcast to session-A only
	hub.broadcastToSession("session-A", WSMessage{Type: "test", Content: "hello-A"})

	// c1 should receive it
	select {
	case msg := <-c1.send:
		if msg.Content != "hello-A" {
			t.Errorf("c1 got wrong content: %s", msg.Content)
		}
	default:
		t.Error("c1 did not receive message")
	}

	// c2 should NOT receive it
	select {
	case msg := <-c2.send:
		t.Errorf("c2 received unexpected message: %+v", msg)
	default:
		// correct — no message
	}
}

func TestHandleWSMessage_SetPrimaryAgent(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(dir)
	sess := store.New("test session", "/workspace", "model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	hub := newWSHub()
	go hub.run()

	client := &wsClient{send: make(chan WSMessage, 4), ctx: context.Background()}
	hub.registerWithSession(client, sess.ID)

	s := &Server{store: store, wsHub: hub}
	c := &wsClient{send: make(chan WSMessage, 4), ctx: context.Background()}
	msg := WSMessage{
		Type:      "set_primary_agent",
		SessionID: sess.ID,
		Payload:   map[string]any{"agent": "Stacy"},
	}
	s.handleWSMessage(c, msg)

	// Verify session manifest was updated on disk
	loaded, err := store.Load(sess.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.PrimaryAgentID() != "Stacy" {
		t.Errorf("expected PrimaryAgentID Stacy, got %q", loaded.PrimaryAgentID())
	}

	// Verify broadcast was sent to the registered client
	select {
	case broadcast := <-client.send:
		if broadcast.Type != "primary_agent_changed" {
			t.Errorf("expected type primary_agent_changed, got %q", broadcast.Type)
		}
		agent, _ := broadcast.Payload["agent"].(string)
		if agent != "Stacy" {
			t.Errorf("expected agent Stacy in payload, got %q", agent)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout: primary_agent_changed broadcast not received")
	}
}

func TestHandleWSMessage_SetPrimaryAgent_EmptyAgent(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(dir)
	sess := store.New("test session", "/workspace", "model")
	store.SaveManifest(sess)

	hub := newWSHub()
	go hub.run()

	s := &Server{store: store, wsHub: hub}
	c := &wsClient{send: make(chan WSMessage, 4), ctx: context.Background()}

	// Empty agent name — should be a no-op
	msg := WSMessage{
		Type:      "set_primary_agent",
		SessionID: sess.ID,
		Payload:   map[string]any{"agent": ""},
	}
	s.handleWSMessage(c, msg) // must not panic or block

	// No broadcast should occur
	select {
	case extra := <-c.send:
		t.Errorf("unexpected message sent: %+v", extra)
	default:
		// correct — no message
	}
}

func TestWSHubBroadcastToSessionEmpty(t *testing.T) {
	hub := newWSHub()
	go hub.run()

	// A client with no session ID receives all broadcasts
	c := &wsClient{send: make(chan WSMessage, 4), ctx: context.Background()}
	hub.registerWithSession(c, "") // empty string = wildcard

	hub.broadcastToSession("session-X", WSMessage{Type: "test", Content: "hello"})

	// Client with no session receives all (empty sessionID = wildcard)
	select {
	case msg := <-c.send:
		if msg.Content != "hello" {
			t.Errorf("got wrong content: %s", msg.Content)
		}
	default:
		t.Error("client with no session did not receive broadcast")
	}
}
