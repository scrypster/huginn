package server

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/threadmgr"
)

func newHubServer(sessionID string) (*Server, *wsClient) {
	hub := newWSHub()
	s := &Server{wsHub: hub}
	c := &wsClient{send: make(chan WSMessage, 8), ctx: context.Background()}
	hub.registerWithSession(c, sessionID)
	return s, c
}

func TestServer_BroadcastPlanning_EmitsToSession(t *testing.T) {
	s, c := newHubServer("sess-test")

	s.BroadcastPlanning("sess-test", "Alex")

	select {
	case msg := <-c.send:
		if msg.Type != "planning" {
			t.Errorf("expected type %q, got %q", "planning", msg.Type)
		}
		if msg.SessionID != "sess-test" {
			t.Errorf("expected session_id %q, got %q", "sess-test", msg.SessionID)
		}
		agent, _ := msg.Payload["agent"].(string)
		if agent != "Alex" {
			t.Errorf("expected payload agent %q, got %q", "Alex", agent)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout: no planning message received")
	}
}

func TestServer_BroadcastPlanningDone_EmitsToSession(t *testing.T) {
	s, c := newHubServer("sess-test")

	s.BroadcastPlanningDone("sess-test")

	select {
	case msg := <-c.send:
		if msg.Type != "planning_done" {
			t.Errorf("expected type %q, got %q", "planning_done", msg.Type)
		}
		if msg.SessionID != "sess-test" {
			t.Errorf("expected session_id %q, got %q", "sess-test", msg.SessionID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout: no planning_done message received")
	}
}

func TestHandleWSMessage_ThreadCancel(t *testing.T) {
	tm := threadmgr.New()
	thread, err := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-1",
		AgentID:   "Coder",
		Task:      "some task",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Simulate a running thread by setting cancel
	cancelled := false
	tm.Start(thread.ID, nil, func() { cancelled = true })

	s := &Server{tm: tm}
	c := &wsClient{send: make(chan WSMessage, 4), ctx: context.Background()}
	msg := WSMessage{
		Type:    "thread_cancel",
		Payload: map[string]any{"thread_id": thread.ID},
	}
	s.handleWSMessage(c, msg)

	time.Sleep(10 * time.Millisecond)

	got, _ := tm.Get(thread.ID)
	if got.Status != threadmgr.StatusCancelled {
		t.Errorf("expected StatusCancelled, got %s", got.Status)
	}
	if !cancelled {
		t.Error("expected cancel func to be called")
	}
}

func TestHandleWSMessage_ThreadInject_NotWaiting(t *testing.T) {
	tm := threadmgr.New()
	thread, err := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-1",
		AgentID:   "Helper",
		Task:      "help needed",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ch, ok := tm.GetInputCh(thread.ID)
	if !ok {
		t.Fatal("thread not found")
	}

	s := &Server{tm: tm}
	c := &wsClient{send: make(chan WSMessage, 4), ctx: context.Background()}
	msg := WSMessage{
		Type: "thread_inject",
		Payload: map[string]any{
			"thread_id": thread.ID,
			"content":   "dropped message",
		},
	}
	s.handleWSMessage(c, msg)

	select {
	case got := <-c.send:
		if got.Type != "thread_inject_error" {
			t.Fatalf("expected thread_inject_error, got %q", got.Type)
		}
		reason, _ := got.Payload["reason"].(string)
		if reason != "not_waiting" {
			t.Fatalf("expected reason not_waiting, got %q", reason)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout: expected thread_inject_error")
	}

	// No input should be delivered while thread is not blocked.
	select {
	case extra := <-ch:
		t.Errorf("unexpected input delivered while not waiting: %q", extra)
	default:
		// expected
	}
}

func TestHandleWSMessage_ThreadInject_UnknownThread(t *testing.T) {
	tm := threadmgr.New()
	s := &Server{tm: tm}
	c := &wsClient{send: make(chan WSMessage, 4), ctx: context.Background()}
	msg := WSMessage{
		Type: "thread_inject",
		Payload: map[string]any{
			"thread_id": "thread-does-not-exist",
			"content":   "here is the clarification",
		},
	}
	s.handleWSMessage(c, msg)

	select {
	case got := <-c.send:
		if got.Type != "thread_inject_error" {
			t.Fatalf("expected thread_inject_error, got %q", got.Type)
		}
		reason, _ := got.Payload["reason"].(string)
		if reason != "not_found" {
			t.Fatalf("expected reason not_found, got %q", reason)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout: expected thread_inject_error")
	}
}
