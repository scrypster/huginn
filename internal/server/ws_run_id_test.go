package server

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
)

// ── minimal stub backend ──────────────────────────────────────────────────────

// runIDStubBackend is a no-op Backend for testing the WS run_id echo behaviour.
// returnErr, when non-nil, causes ChatCompletion to return an error so tests can
// exercise the error path without a real LLM.
type runIDStubBackend struct {
	returnErr error
}

func (s *runIDStubBackend) ChatCompletion(_ context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
	if s.returnErr != nil {
		return nil, s.returnErr
	}
	return &backend.ChatResponse{Content: ""}, nil
}
func (s *runIDStubBackend) Health(_ context.Context) error   { return nil }
func (s *runIDStubBackend) Shutdown(_ context.Context) error { return nil }
func (s *runIDStubBackend) ContextWindow() int               { return 8192 }

// drainToCompletion reads messages from the client send channel until a
// "done" or "error" message is received or the deadline fires.
// It returns the final message (done/error) or fails the test.
func drainToCompletion(t *testing.T, c *wsClient) WSMessage {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-c.send:
			if msg.Type == "done" || msg.Type == "error" {
				return msg
			}
		case <-deadline:
			t.Fatal("timed out waiting for done or error message from WS handler")
		}
	}
	panic("unreachable")
}

// newTestServer builds a minimal Server wired with the provided backend.
// store and wsHub are initialised so handleWSMessage can run safely.
func newTestServerWithBackend(t *testing.T, b backend.Backend) *Server {
	t.Helper()
	models := &modelconfig.Models{Reasoner: "stub-model"}
	orch, err := agent.NewOrchestrator(b, models, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("agent.NewOrchestrator: %v", err)
	}
	hub := newWSHub()
	go hub.run()
	t.Cleanup(func() { hub.stop() })
	return &Server{orch: orch, wsHub: hub}
}

// ── done path: run_id echoed ──────────────────────────────────────────────────

// TestHandleWSMessage_Chat_RunIDEchoedInDone is the primary regression test for
// issue #12.  Before the fix, the server sent {type:"done"} without RunID, so
// the frontend's `if (!msg.run_id || ...)` guard always ignored it and
// streaming.value was permanently stuck at true after the first response.
func TestHandleWSMessage_Chat_RunIDEchoedInDone(t *testing.T) {
	s := newTestServerWithBackend(t, &runIDStubBackend{})
	c := &wsClient{send: make(chan WSMessage, 16), ctx: context.Background()}

	const wantRunID = "test-run-abc-123"
	s.handleWSMessage(c, WSMessage{
		Type:      "chat",
		Content:   "hello",
		SessionID: "sess-run-id-test",
		RunID:     wantRunID,
	})

	msg := drainToCompletion(t, c)
	if msg.Type != "done" {
		t.Errorf("expected type 'done', got %q (content: %q)", msg.Type, msg.Content)
	}
	if msg.RunID != wantRunID {
		t.Errorf("done.RunID = %q, want %q — server must echo the client's run_id so "+
			"the frontend stale-event guard can reset streaming state", msg.RunID, wantRunID)
	}
}

// TestHandleWSMessage_Chat_RunIDEchoedInDone_EmptyRunID verifies that when the
// client sends no run_id (older clients), done is still sent and RunID is empty.
func TestHandleWSMessage_Chat_RunIDEchoedInDone_EmptyRunID(t *testing.T) {
	s := newTestServerWithBackend(t, &runIDStubBackend{})
	c := &wsClient{send: make(chan WSMessage, 16), ctx: context.Background()}

	s.handleWSMessage(c, WSMessage{
		Type:      "chat",
		Content:   "hello",
		SessionID: "sess-no-run-id",
		// No RunID set — simulates older client.
	})

	msg := drainToCompletion(t, c)
	if msg.Type != "done" {
		t.Errorf("expected type 'done', got %q", msg.Type)
	}
	if msg.RunID != "" {
		t.Errorf("done.RunID = %q, want empty when client sent no run_id", msg.RunID)
	}
}

// ── error path: run_id echoed ─────────────────────────────────────────────────

// TestHandleWSMessage_Chat_RunIDEchoedInError verifies that when the backend
// returns an error, the error message carries the same run_id as the request.
func TestHandleWSMessage_Chat_RunIDEchoedInError(t *testing.T) {
	stubErr := &runIDStubBackend{returnErr: context.DeadlineExceeded}
	s := newTestServerWithBackend(t, stubErr)
	c := &wsClient{send: make(chan WSMessage, 16), ctx: context.Background()}

	const wantRunID = "err-run-xyz-789"
	s.handleWSMessage(c, WSMessage{
		Type:      "chat",
		Content:   "fail please",
		SessionID: "sess-err-run-id",
		RunID:     wantRunID,
	})

	msg := drainToCompletion(t, c)
	if msg.Type != "error" {
		t.Errorf("expected type 'error', got %q", msg.Type)
	}
	if msg.RunID != wantRunID {
		t.Errorf("error.RunID = %q, want %q — run_id must be echoed in error messages too",
			msg.RunID, wantRunID)
	}
}

// TestHandleWSMessage_Chat_NilOrchestratorNoRunID verifies that "orchestrator
// not initialized" errors sent before any run_id is established do NOT carry a
// run_id (the client error handler allows this for pre-run error messages).
func TestHandleWSMessage_Chat_NilOrchestratorNoRunID(t *testing.T) {
	hub := newWSHub()
	go hub.run()
	t.Cleanup(func() { hub.stop() })

	s := &Server{orch: nil, wsHub: hub}
	c := &wsClient{send: make(chan WSMessage, 4), ctx: context.Background()}

	s.handleWSMessage(c, WSMessage{
		Type:      "chat",
		Content:   "hello",
		SessionID: "sess-nil-orch",
		RunID:     "some-run-id",
	})

	select {
	case msg := <-c.send:
		if msg.Type != "error" {
			t.Errorf("expected 'error', got %q", msg.Type)
		}
		// Pre-run error (orch == nil) must NOT carry run_id: the client guard
		// uses `&&` not `||` for errors, so an empty run_id is handled correctly.
		if msg.RunID != "" {
			t.Errorf("pre-run error should have empty RunID, got %q", msg.RunID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for error message")
	}
}
