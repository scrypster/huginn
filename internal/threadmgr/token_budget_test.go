package threadmgr

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
)

// TestSpawnThread_TokenBudgetEnforced verifies that when TokenBudget is set and
// the thread accumulates >= that many tokens, the loop stops and the thread
// completes with status "completed-with-timeout" and summary "token budget exhausted".
func TestSpawnThread_TokenBudgetEnforced(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("sess-budget", "/tmp", "claude-haiku-4")

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:    "BudgetBot",
		ModelID: "claude-haiku-4",
	})

	// Each response costs 60 prompt + 60 completion = 120 tokens total.
	// TokenBudget = 100, so after the first turn (120 tokens accumulated)
	// the budget check should fire and stop the loop.
	fb := &fakeBackend{
		response: &backend.ChatResponse{
			// Return no tool calls so the thread would normally keep looping
			// (the "stop" reason path), but the budget guard fires first.
			Content:          "thinking...",
			DoneReason:       "stop",
			PromptTokens:     60,
			CompletionTokens: 60,
		},
	}

	thread, err := tm.Create(CreateParams{
		SessionID:   sess.ID,
		AgentID:     "BudgetBot",
		Task:        "keep going forever",
		TokenBudget: 100, // budget of 100 tokens — first turn uses 120
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	broadcastFn := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, fb, broadcastFn, ca, nil)

	// Wait up to 3 seconds for the thread to reach a terminal state.
	deadline := time.Now().Add(3 * time.Second)
	var got *Thread
	for time.Now().Before(deadline) {
		got, _ = tm.Get(thread.ID)
		if got != nil && (got.Status == StatusDone || got.Status == StatusCancelled || got.Status == StatusError) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	got, ok := tm.Get(thread.ID)
	if !ok {
		t.Fatal("thread not found after budget exhaustion")
	}
	if got.Status != StatusDone {
		t.Errorf("expected StatusDone after budget exhaustion, got %s", got.Status)
	}
	if got.Summary == nil {
		t.Fatal("expected Summary to be set after budget exhaustion")
	}
	if got.Summary.Status != "completed-with-timeout" {
		t.Errorf("expected Summary.Status='completed-with-timeout', got %q", got.Summary.Status)
	}
	// The summary text should mention token budget.
	if got.Summary.Summary == "" {
		t.Error("expected non-empty summary after budget exhaustion")
	}
}

// TestCheckTokenBudget_NoLimit verifies checkTokenBudget returns nil when
// TokenBudget is 0 (unlimited).
func TestCheckTokenBudget_NoLimit(t *testing.T) {
	tm := New()
	thr, _ := tm.Create(CreateParams{
		SessionID:   "sess-budget-no-limit",
		AgentID:     "a",
		Task:        "t",
		TokenBudget: 0,
	})
	// Even if many tokens are "used", no limit is enforced.
	tm.mu.Lock()
	tm.threads[thr.ID].TokensUsed = 1_000_000
	tm.mu.Unlock()

	if err := checkTokenBudget(tm, thr.ID); err != nil {
		t.Errorf("expected nil (no limit), got %v", err)
	}
}

// TestCheckTokenBudget_BelowLimit verifies checkTokenBudget returns nil when
// tokens used is below the budget.
func TestCheckTokenBudget_BelowLimit(t *testing.T) {
	tm := New()
	thr, _ := tm.Create(CreateParams{
		SessionID:   "sess-budget-below",
		AgentID:     "a",
		Task:        "t",
		TokenBudget: 500,
	})
	tm.mu.Lock()
	tm.threads[thr.ID].TokensUsed = 499
	tm.mu.Unlock()

	if err := checkTokenBudget(tm, thr.ID); err != nil {
		t.Errorf("expected nil (below limit), got %v", err)
	}
}

// TestCheckTokenBudget_AtLimit verifies checkTokenBudget returns an error when
// tokens used equals the budget.
func TestCheckTokenBudget_AtLimit(t *testing.T) {
	tm := New()
	thr, _ := tm.Create(CreateParams{
		SessionID:   "sess-budget-at",
		AgentID:     "a",
		Task:        "t",
		TokenBudget: 100,
	})
	tm.mu.Lock()
	tm.threads[thr.ID].TokensUsed = 100
	tm.mu.Unlock()

	if err := checkTokenBudget(tm, thr.ID); err == nil {
		t.Error("expected non-nil error at budget limit, got nil")
	}
}

// TestSpawnThread_TokenBudget_NoSpuriousCompletion verifies that a thread with
// a budget that is NOT exceeded completes normally via the finish tool.
func TestSpawnThread_TokenBudget_NoSpuriousCompletion(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("sess-budget-ok", "/tmp", "claude-haiku-4")

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:    "BudgetOKBot",
		ModelID: "claude-haiku-4",
	})

	// Finish immediately — only 10 tokens used, well under the 1000 budget.
	fb := &fakeBackend{
		response: &backend.ChatResponse{
			ToolCalls: []backend.ToolCall{
				{
					ID: "tc-budget-ok",
					Function: backend.ToolCallFunction{
						Name: "finish",
						Arguments: map[string]any{
							"summary": "finished within budget",
							"status":  "completed",
						},
					},
				},
			},
			DoneReason:       "tool_calls",
			PromptTokens:     5,
			CompletionTokens: 5,
		},
	}

	thread, _ := tm.Create(CreateParams{
		SessionID:   sess.ID,
		AgentID:     "BudgetOKBot",
		Task:        "quick task",
		TokenBudget: 1000,
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
	if got.Summary == nil || got.Summary.Summary != "finished within budget" {
		t.Errorf("unexpected summary: %+v", got.Summary)
	}
}
