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

func TestEvaluateDAG_SpawnsReadyThread(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("dag-test", "/tmp", "claude-haiku-4")
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Worker", ModelID: "claude-haiku-4"})

	// Backend immediately calls finish()
	fb := &fakeBackend{
		response: &backend.ChatResponse{
			ToolCalls: []backend.ToolCall{
				{
					ID: "tc-1",
					Function: backend.ToolCallFunction{
						Name:      "finish",
						Arguments: map[string]any{"summary": "upstream done"},
					},
				},
			},
			DoneReason: "tool_calls",
		},
	}

	upstream, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Worker", Task: "upstream"})
	downstream, _ := tm.Create(CreateParams{
		SessionID: sess.ID,
		AgentID:   "Worker",
		Task:      "downstream",
		DependsOn: []string{upstream.ID},
	})

	broadcast := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	// Spawn upstream with a DAG callback that spawns downstream
	dagFn := func() {
		tm.EvaluateDAG(context.Background(), sess.ID, store, sess, reg, fb, broadcast, ca)
	}
	tm.SpawnThread(context.Background(), upstream.ID, store, sess, reg, fb, broadcast, ca, dagFn)

	// Wait for both threads to complete
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		up, _ := tm.Get(upstream.ID)
		dn, _ := tm.Get(downstream.ID)
		if up != nil && up.Status == StatusDone && dn != nil && dn.Status == StatusDone {
			return // success
		}
		time.Sleep(20 * time.Millisecond)
	}

	up, _ := tm.Get(upstream.ID)
	dn, _ := tm.Get(downstream.ID)
	t.Errorf("timeout: upstream=%v downstream=%v", up.Status, dn.Status)
}

func TestEvaluateDAG_DoesNotSpawnBlocked(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("dag-test2", "/tmp", "claude-haiku-4")
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Worker", ModelID: "claude-haiku-4"})

	fb := &fakeBackend{
		response: &backend.ChatResponse{
			Content:    "thinking",
			DoneReason: "stop",
		},
	}

	upstream, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Worker", Task: "up"})
	downstream, _ := tm.Create(CreateParams{
		SessionID: sess.ID,
		AgentID:   "Worker",
		Task:      "down",
		DependsOn: []string{upstream.ID},
	})
	_ = downstream

	// Manually transition upstream to StatusThinking (simulates it already running).
	// This means upstream is no longer StatusQueued, so EvaluateDAG won't re-spawn it.
	// downstream depends on upstream which is not StatusDone, so IsReady returns false.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tm.Start(upstream.ID, ctx, cancel)

	var mu sync.Mutex
	var spawnCount int
	countBroadcast := func(_, msgType string, payload map[string]any) {
		if msgType == "thread_started" {
			mu.Lock()
			spawnCount++
			mu.Unlock()
		}
	}

	ca := NewCostAccumulator(0)
	tm.EvaluateDAG(context.Background(), sess.ID, store, sess, reg, fb, countBroadcast, ca)

	// Give it a moment
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	count := spawnCount
	mu.Unlock()

	// downstream depends on upstream (StatusThinking, not StatusDone) — must NOT be spawned
	if count > 0 {
		t.Errorf("expected 0 spawned threads (upstream not done), got %d", count)
	}
}
