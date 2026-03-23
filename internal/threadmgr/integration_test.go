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

// TestIntegration_FullChain tests:
// 1. Create upstream + downstream threads
// 2. SpawnThread upstream (calls finish() → Complete → EvaluateDAG)
// 3. EvaluateDAG spawns downstream (also calls finish())
// 4. Both threads end up StatusDone
// 5. thread_done is broadcast for both
// 6. No data races under -race
func TestIntegration_FullChain(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("integration", "/tmp", "claude-haiku-4")

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:    "Worker",
		ModelID: "claude-haiku-4",
	})

	// Scripted backend: always responds with finish()
	finishResponse := &backend.ChatResponse{
		ToolCalls: []backend.ToolCall{
			{
				ID: "tc-finish",
				Function: backend.ToolCallFunction{
					Name:      "finish",
					Arguments: map[string]any{"summary": "task complete", "status": "completed"},
				},
			},
		},
		DoneReason:       "tool_calls",
		PromptTokens:     50,
		CompletionTokens: 20,
	}
	fb := &fakeBackend{response: finishResponse}

	upstream, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Worker", Task: "upstream work"})
	downstream, _ := tm.Create(CreateParams{
		SessionID: sess.ID,
		AgentID:   "Worker",
		Task:      "downstream work",
		DependsOn: []string{upstream.ID},
	})

	var mu sync.Mutex
	var doneIDs []string
	broadcast := func(_, msgType string, payload map[string]any) {
		if msgType == "thread_done" {
			if tid, ok := payload["thread_id"].(string); ok {
				mu.Lock()
				doneIDs = append(doneIDs, tid)
				mu.Unlock()
			}
		}
	}

	ca := NewCostAccumulator(0)
	ctx := context.Background()

	dagFn := func() {
		tm.EvaluateDAG(ctx, sess.ID, store, sess, reg, fb, broadcast, ca)
	}
	tm.SpawnThread(ctx, upstream.ID, store, sess, reg, fb, broadcast, ca, dagFn)

	// Wait up to 5s for both threads to complete
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		up, _ := tm.Get(upstream.ID)
		dn, _ := tm.Get(downstream.ID)
		if up != nil && up.Status == StatusDone && dn != nil && dn.Status == StatusDone {
			goto verify
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timeout: threads did not complete")

verify:
	mu.Lock()
	doneCopy := make([]string, len(doneIDs))
	copy(doneCopy, doneIDs)
	mu.Unlock()

	if len(doneCopy) < 2 {
		t.Errorf("expected 2 thread_done broadcasts, got %d: %v", len(doneCopy), doneCopy)
	}

	// Verify upstream has non-nil summary
	up, _ := tm.Get(upstream.ID)
	if up.Summary == nil {
		t.Error("upstream should have a FinishSummary")
	}

	// Log accumulated session cost (SessionTotal is exported)
	ca.mu.Lock()
	total := ca.SessionTotal
	ca.mu.Unlock()
	t.Logf("session total cost: $%.6f", total)
}

func TestIntegration_BudgetExceededAbortsThread(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("budget-test", "/tmp", "claude-opus-4")

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:    "Expensive",
		ModelID: "claude-opus-4",
	})

	// Backend returns high token counts to blow the budget
	fb := &fakeBackend{
		response: &backend.ChatResponse{
			Content:          "thinking...",
			DoneReason:       "stop",
			PromptTokens:     10_000_000,
			CompletionTokens: 10_000_000,
		},
	}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Expensive", Task: "expensive task"})

	var broadcastedTypes []string
	var bmu sync.Mutex
	broadcast := func(_, msgType string, _ map[string]any) {
		bmu.Lock()
		broadcastedTypes = append(broadcastedTypes, msgType)
		bmu.Unlock()
	}

	// Tiny budget: $0.01
	ca := NewCostAccumulator(0.01)

	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, fb, broadcast, ca, nil)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := tm.Get(thread.ID)
		if got != nil && (got.Status == StatusDone || got.Status == StatusError) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	got, _ := tm.Get(thread.ID)
	if got == nil {
		t.Fatal("thread not found")
	}
	// Should have terminated (done or error) due to budget
	if got.Status == StatusThinking || got.Status == StatusQueued {
		t.Errorf("expected thread to terminate due to budget, still in status %s", got.Status)
	}
}
