package threadmgr

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
)

// TestSpawnThread_TimeoutExpires verifies that when CreateParams.Timeout is set,
// the goroutine stops and the thread is completed with status "completed-with-timeout"
// after the deadline fires — even when the LLM call is blocking.
func TestSpawnThread_TimeoutExpires(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test-timeout", "/tmp", "claude-haiku-4")

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:    "TimeoutBot",
		ModelID: "claude-haiku-4",
	})

	// blockingFakeBackend blocks until its channel is closed or ctx is cancelled.
	// Re-use the blockingFakeBackend defined in spawn_test.go (same package).
	blockCh := make(chan struct{})
	b := &blockingFakeBackend{block: blockCh}

	thread, err := tm.Create(CreateParams{
		SessionID: sess.ID,
		AgentID:   "TimeoutBot",
		Task:      "do something long",
		Timeout:   100 * time.Millisecond, // very short timeout
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify that Timeout was stored on the thread.
	if thread.Timeout != 100*time.Millisecond {
		t.Errorf("expected Timeout=100ms on thread, got %v", thread.Timeout)
	}

	broadcastFn := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, b, broadcastFn, ca, nil)

	// Wait up to 2 seconds for the thread to reach a terminal state.
	deadline := time.Now().Add(2 * time.Second)
	var got *Thread
	for time.Now().Before(deadline) {
		got, _ = tm.Get(thread.ID)
		if got != nil {
			switch got.Status {
			case StatusDone, StatusCancelled, StatusError:
				goto done
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

done:
	// Unblock the fake backend so its goroutine can exit cleanly.
	close(blockCh)

	got, ok := tm.Get(thread.ID)
	if !ok {
		t.Fatal("thread not found after timeout")
	}

	// Thread must have terminated (done is the expected status for a timeout).
	switch got.Status {
	case StatusDone, StatusCancelled:
		// acceptable — timeout may complete or cancel depending on timing
	default:
		t.Errorf("expected terminal status after timeout, got %s", got.Status)
	}

	// When StatusDone, the summary status must be "completed-with-timeout".
	if got.Status == StatusDone {
		if got.Summary == nil {
			t.Error("expected Summary to be set after timeout completion")
		} else if got.Summary.Status != "completed-with-timeout" {
			t.Errorf("expected Summary.Status='completed-with-timeout', got %q", got.Summary.Status)
		}
	}
}

// TestCreateParams_TimeoutFieldPresent verifies the Timeout field round-trips
// through CreateParams → Thread without needing a full goroutine spawn.
func TestCreateParams_TimeoutFieldPresent(t *testing.T) {
	tm := New()
	d := 5 * time.Minute
	thr, err := tm.Create(CreateParams{
		SessionID: "sess-timeout-field",
		AgentID:   "agent",
		Task:      "check timeout",
		Timeout:   d,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if thr.Timeout != d {
		t.Errorf("expected Timeout=%v, got %v", d, thr.Timeout)
	}
}

// TestCreateParams_ZeroTimeout_NoDeadline verifies that a zero Timeout does not
// impose a deadline — the thread struct has Timeout == 0.
func TestCreateParams_ZeroTimeout_NoDeadline(t *testing.T) {
	tm := New()
	thr, err := tm.Create(CreateParams{
		SessionID: "sess-no-timeout",
		AgentID:   "agent",
		Task:      "no deadline",
		Timeout:   0,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if thr.Timeout != 0 {
		t.Errorf("expected Timeout=0, got %v", thr.Timeout)
	}
}

// TestSpawnThread_NoTimeout_NormalCompletion verifies that without a Timeout the
// finish-tool path still works correctly (regression guard).
func TestSpawnThread_NoTimeout_NormalCompletion(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test-no-timeout", "/tmp", "claude-haiku-4")

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:    "FastBot",
		ModelID: "claude-haiku-4",
	})

	fb := &fakeBackend{
		response: &backend.ChatResponse{
			ToolCalls: []backend.ToolCall{
				{
					ID: "tc-timeout-1",
					Function: backend.ToolCallFunction{
						Name: "finish",
						Arguments: map[string]any{
							"summary": "done quickly",
							"status":  "completed",
						},
					},
				},
			},
			DoneReason:       "tool_calls",
			PromptTokens:     10,
			CompletionTokens: 5,
		},
	}

	thread, _ := tm.Create(CreateParams{
		SessionID: sess.ID,
		AgentID:   "FastBot",
		Task:      "fast task",
		Timeout:   0, // no timeout
	})

	broadcastFn := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)
	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, fb, broadcastFn, ca, nil)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := tm.Get(thread.ID)
		if got != nil && got.Status == StatusDone {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	got, ok := tm.Get(thread.ID)
	if !ok {
		t.Fatal("thread not found")
	}
	if got.Status != StatusDone {
		t.Errorf("expected StatusDone, got %s", got.Status)
	}
}
