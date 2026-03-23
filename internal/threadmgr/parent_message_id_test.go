package threadmgr

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
)

// ─── Create: ParentMessageID propagation ────────────────────────────────────

func TestCreate_ParentMessageID_PropagatestoThread(t *testing.T) {
	tm := New()
	thread, err := tm.Create(CreateParams{
		SessionID:       "sess-1",
		AgentID:         "coder",
		Task:            "fix bug",
		ParentMessageID: "msg-42",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, ok := tm.Get(thread.ID)
	if !ok {
		t.Fatal("thread not found after Create")
	}
	if got.ParentMessageID != "msg-42" {
		t.Errorf("ParentMessageID = %q, want %q", got.ParentMessageID, "msg-42")
	}
}

func TestCreate_EmptyParentMessageID_ThreadHasEmptyField(t *testing.T) {
	tm := New()
	thread, err := tm.Create(CreateParams{
		SessionID: "sess-1",
		AgentID:   "coder",
		Task:      "do work",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, _ := tm.Get(thread.ID)
	if got.ParentMessageID != "" {
		t.Errorf("expected empty ParentMessageID, got %q", got.ParentMessageID)
	}
}

// ─── SpawnThread: thread_started broadcast includes parent_message_id ─────────

// spawnFinishBackend returns a finish tool call immediately so SpawnThread
// completes in one turn without blocking.
type spawnFinishBackend struct{}

func (b *spawnFinishBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	if req.OnToken != nil {
		req.OnToken("working")
	}
	return &backend.ChatResponse{
		ToolCalls: []backend.ToolCall{
			{
				ID: "tc-finish",
				Function: backend.ToolCallFunction{
					Name: "finish",
					Arguments: map[string]any{
						"summary": "done",
						"status":  "completed",
					},
				},
			},
		},
		DoneReason:       "tool_calls",
		PromptTokens:     10,
		CompletionTokens: 5,
	}, nil
}
func (b *spawnFinishBackend) Health(_ context.Context) error   { return nil }
func (b *spawnFinishBackend) Shutdown(_ context.Context) error { return nil }
func (b *spawnFinishBackend) ContextWindow() int               { return 4096 }

// waitForBroadcast blocks until broadcastCh receives a payload for the given
// msgType, or times out. Returns the payload and whether it was found.
func waitForBroadcast(t *testing.T, broadcastCh <-chan capturedBroadcast, wantType string, timeout time.Duration) (map[string]any, bool) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case b := <-broadcastCh:
			if b.msgType == wantType {
				return b.payload, true
			}
		case <-deadline.C:
			return nil, false
		}
	}
}

func makeSpawnFixture(t *testing.T) (tm *ThreadManager, store *session.Store, sess *session.Session, reg *agents.AgentRegistry) {
	t.Helper()
	tm = New()
	store = session.NewStore(t.TempDir())
	sess = store.New("test", "/tmp", "claude-haiku-4")
	reg = agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:    "coder",
		ModelID: "claude-haiku-4",
	})
	return
}

func TestSpawnThread_ParentMessageID_IncludedInThreadStartedBroadcast(t *testing.T) {
	tm, store, sess, reg := makeSpawnFixture(t)

	thread, _ := tm.Create(CreateParams{
		SessionID:       sess.ID,
		AgentID:         "coder",
		Task:            "build it",
		ParentMessageID: "msg-parent-99",
	})

	broadcastCh := make(chan capturedBroadcast, 16)
	var bmu sync.Mutex
	broadcastFn := func(sessionID, msgType string, payload map[string]any) {
		bmu.Lock()
		broadcastCh <- capturedBroadcast{sessionID, msgType, payload}
		bmu.Unlock()
	}

	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, &spawnFinishBackend{}, broadcastFn, NewCostAccumulator(0), nil)

	payload, ok := waitForBroadcast(t, broadcastCh, "thread_started", 2*time.Second)
	if !ok {
		t.Fatal("did not receive thread_started broadcast within timeout")
	}
	val, exists := payload["parent_message_id"]
	if !exists {
		t.Error("expected parent_message_id in thread_started payload, not found")
	}
	if val != "msg-parent-99" {
		t.Errorf("parent_message_id = %v, want %q", val, "msg-parent-99")
	}

	// Wait for the goroutine to fully complete before t.TempDir() cleanup.
	waitForBroadcast(t, broadcastCh, "thread_done", 3*time.Second)
}

func TestSpawnThread_EmptyParentMessageID_OmittedFromThreadStartedBroadcast(t *testing.T) {
	tm, store, sess, reg := makeSpawnFixture(t)

	thread, _ := tm.Create(CreateParams{
		SessionID: sess.ID,
		AgentID:   "coder",
		Task:      "do work",
		// ParentMessageID intentionally empty
	})

	broadcastCh := make(chan capturedBroadcast, 16)
	var bmu sync.Mutex
	broadcastFn := func(sessionID, msgType string, payload map[string]any) {
		bmu.Lock()
		broadcastCh <- capturedBroadcast{sessionID, msgType, payload}
		bmu.Unlock()
	}

	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, &spawnFinishBackend{}, broadcastFn, NewCostAccumulator(0), nil)

	payload, ok := waitForBroadcast(t, broadcastCh, "thread_started", 2*time.Second)
	if !ok {
		t.Fatal("did not receive thread_started broadcast within timeout")
	}
	if _, exists := payload["parent_message_id"]; exists {
		t.Error("parent_message_id should NOT be present in thread_started payload when empty")
	}

	// Wait for the goroutine to fully complete before t.TempDir() cleanup.
	waitForBroadcast(t, broadcastCh, "thread_done", 3*time.Second)
}
