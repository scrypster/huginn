package relay_test

// dispatcher_run_agent_test.go — tests for MsgRunAgent / MsgAgentResult handling.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/relay"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// collectAgentResults collects all MsgAgentResult messages from hub.
func collectAgentResults(hub *collectingHub) []relay.Message {
	var out []relay.Message
	for _, m := range hub.Collect() {
		if m.Type == relay.MsgAgentResult {
			out = append(out, m)
		}
	}
	return out
}

// waitForDoneResult waits for a MsgAgentResult with done=true.
func waitForDoneResult(t *testing.T, hub *collectingHub, timeout time.Duration) relay.Message {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, m := range hub.Collect() {
			if m.Type != relay.MsgAgentResult {
				continue
			}
			if done, _ := m.Payload["done"].(bool); done {
				return m
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for done MsgAgentResult")
	return relay.Message{}
}

// ─── TestDispatcher_RunAgent_StreamsTokensAndDone ─────────────────────────────

// TestDispatcher_RunAgent_StreamsTokensAndDone verifies that a valid run_agent
// message causes the RunAgent callback to be called, token frames to be sent as
// MsgAgentResult, and a final done=true MsgAgentResult to be sent.
func TestDispatcher_RunAgent_StreamsTokensAndDone(t *testing.T) {
	hub := &collectingHub{}
	active := relay.NewActiveSessions()

	dispatched := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		Active:    active,
		RunAgent: func(ctx context.Context, agentName, prompt, sessionID string, onToken func(string)) error {
			onToken("hello")
			onToken(" world")
			return nil
		},
	})

	dispatched(context.Background(), relay.Message{
		Type: relay.MsgRunAgent,
		Payload: map[string]any{
			"run_id":     "run-001",
			"agent_name": "my-agent",
			"prompt":     "say hello",
			"session_id": "sess-abc",
		},
	})

	done := waitForDoneResult(t, hub, 3*time.Second)

	// Verify done frame fields.
	if runID, _ := done.Payload["run_id"].(string); runID != "run-001" {
		t.Errorf("done run_id = %q, want run-001", runID)
	}
	if errStr, _ := done.Payload["error"].(string); errStr != "" {
		t.Errorf("done error = %q, want empty", errStr)
	}

	// Count token frames (non-done MsgAgentResult).
	results := collectAgentResults(hub)
	var tokenFrames []relay.Message
	for _, r := range results {
		if done2, _ := r.Payload["done"].(bool); !done2 {
			tokenFrames = append(tokenFrames, r)
		}
	}
	if len(tokenFrames) != 2 {
		t.Errorf("expected 2 token frames, got %d", len(tokenFrames))
	}

	// Verify token field name and run_id propagation.
	for _, tf := range tokenFrames {
		if _, hasToken := tf.Payload["token"]; !hasToken {
			t.Errorf("token frame missing 'token' key — got keys: %v", tokKeys(tf.Payload))
		}
		if rid, _ := tf.Payload["run_id"].(string); rid != "run-001" {
			t.Errorf("token frame run_id = %q, want run-001", rid)
		}
	}
}

// ─── TestDispatcher_RunAgent_UnknownAgent_ErrorResult ─────────────────────────

// TestDispatcher_RunAgent_UnknownAgent_ErrorResult verifies that when the
// RunAgent callback returns an error (e.g. agent not found), the dispatcher
// sends a done=true MsgAgentResult with a non-empty error field.
func TestDispatcher_RunAgent_UnknownAgent_ErrorResult(t *testing.T) {
	hub := &collectingHub{}
	active := relay.NewActiveSessions()

	dispatched := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		Active:    active,
		RunAgent: func(ctx context.Context, agentName, prompt, sessionID string, onToken func(string)) error {
			return errors.New(`agent "ghost" not found`)
		},
	})

	dispatched(context.Background(), relay.Message{
		Type: relay.MsgRunAgent,
		Payload: map[string]any{
			"run_id":     "run-002",
			"agent_name": "ghost",
			"prompt":     "hello",
			"session_id": "",
		},
	})

	done := waitForDoneResult(t, hub, 3*time.Second)

	if runID, _ := done.Payload["run_id"].(string); runID != "run-002" {
		t.Errorf("done run_id = %q, want run-002", runID)
	}
	errStr, _ := done.Payload["error"].(string)
	if errStr == "" {
		t.Error("expected non-empty error in done frame when RunAgent returns error")
	}
}

// ─── TestDispatcher_RunAgent_NilCallback_ErrorResult ──────────────────────────

// TestDispatcher_RunAgent_NilCallback_ErrorResult verifies that when RunAgent
// is nil in the config, the dispatcher sends a done=true MsgAgentResult with
// an error rather than silently dropping the message.
func TestDispatcher_RunAgent_NilCallback_ErrorResult(t *testing.T) {
	hub := &collectingHub{}

	dispatched := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		RunAgent:  nil, // not wired
	})

	dispatched(context.Background(), relay.Message{
		Type: relay.MsgRunAgent,
		Payload: map[string]any{
			"run_id":     "run-003",
			"agent_name": "some-agent",
			"prompt":     "do something",
			"session_id": "",
		},
	})

	// Handler is synchronous for nil-callback path (no goroutine).
	done := waitForDoneResult(t, hub, 2*time.Second)

	if runID, _ := done.Payload["run_id"].(string); runID != "run-003" {
		t.Errorf("done run_id = %q, want run-003", runID)
	}
	errStr, _ := done.Payload["error"].(string)
	if errStr == "" {
		t.Error("expected non-empty error when RunAgent is nil")
	}
	if doneVal, _ := done.Payload["done"].(bool); !doneVal {
		t.Error("expected done=true")
	}
}

// ─── TestDispatcher_RunAgent_MissingFields_ErrorResult ────────────────────────

// TestDispatcher_RunAgent_MissingFields_ErrorResult verifies that a run_agent
// message with missing required fields (run_id, agent_name, or prompt) sends
// a done=true MsgAgentResult with an error and does not call RunAgent.
func TestDispatcher_RunAgent_MissingFields_ErrorResult(t *testing.T) {
	hub := &collectingHub{}
	runAgentCalled := false

	dispatched := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		RunAgent: func(ctx context.Context, agentName, prompt, sessionID string, onToken func(string)) error {
			runAgentCalled = true
			return nil
		},
	})

	// Missing prompt — run_id and agent_name present but prompt empty.
	dispatched(context.Background(), relay.Message{
		Type: relay.MsgRunAgent,
		Payload: map[string]any{
			"run_id":     "run-004",
			"agent_name": "my-agent",
			"prompt":     "", // missing
			"session_id": "",
		},
	})

	done := waitForDoneResult(t, hub, 2*time.Second)

	if doneVal, _ := done.Payload["done"].(bool); !doneVal {
		t.Error("expected done=true for missing-fields case")
	}
	errStr, _ := done.Payload["error"].(string)
	if errStr == "" {
		t.Error("expected non-empty error for missing fields")
	}
	if runAgentCalled {
		t.Error("RunAgent should not be called when required fields are missing")
	}
}

// ─── TestDispatcher_RunAgent_Cancellation ─────────────────────────────────────

// TestDispatcher_RunAgent_Cancellation verifies that cancelling the run via a
// cancel_session message (using run_id as the session key) stops the in-flight
// run and results in a done frame being sent.
func TestDispatcher_RunAgent_Cancellation(t *testing.T) {
	hub := &collectingHub{}
	active := relay.NewActiveSessions()

	// RunAgent blocks until ctx is cancelled.
	dispatched := relay.NewDispatcher(relay.DispatcherConfig{
		MachineID: "m1",
		Hub:       hub,
		Active:    active,
		RunAgent: func(ctx context.Context, agentName, prompt, sessionID string, onToken func(string)) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})

	const runID = "run-cancel"

	dispatched(context.Background(), relay.Message{
		Type: relay.MsgRunAgent,
		Payload: map[string]any{
			"run_id":     runID,
			"agent_name": "my-agent",
			"prompt":     "do work",
			"session_id": "",
		},
	})

	// Give the goroutine time to start and block.
	time.Sleep(20 * time.Millisecond)

	// Cancel via cancel_session using run_id as the key.
	dispatched(context.Background(), relay.Message{
		Type:    relay.MsgCancelSession,
		Payload: map[string]any{"session_id": runID},
	})

	// The goroutine should finish and send a done frame.
	done := waitForDoneResult(t, hub, 3*time.Second)

	if runID2, _ := done.Payload["run_id"].(string); runID2 != runID {
		t.Errorf("done run_id = %q, want %q", runID2, runID)
	}
	// Cancellation does NOT set error (context.Canceled is excluded).
	errStr, _ := done.Payload["error"].(string)
	if errStr != "" {
		t.Errorf("done error = %q, want empty for cancellation", errStr)
	}
}
