package server_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/stats"
	"github.com/scrypster/huginn/internal/threadmgr"
)

func TestCostSink_UsesSessionIDNotThreadID(t *testing.T) {
	tm := threadmgr.New()
	thr, err := tm.Create(threadmgr.CreateParams{
		SessionID: "sess-123",
		AgentID:   "agent-a",
		Task:      "task",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got stats.CostEvent
	ca := threadmgr.NewCostAccumulator(0)
	ca.SetCostSink(func(threadID string, costUSD float64, prompt, completion int) {
		// Simulate the fixed main.go wiring: resolve session via tm.
		sessionID := threadID
		if t2, ok := tm.Get(threadID); ok {
			sessionID = t2.SessionID
		}
		got = stats.CostEvent{
			SessionID:        sessionID,
			CostUSD:          costUSD,
			PromptTokens:     prompt,
			CompletionTokens: completion,
		}
	})

	ca.Record(thr.ID, 100, 50, "claude-sonnet-4-6")

	if got.SessionID != "sess-123" {
		t.Errorf("expected SessionID=%q, got %q", "sess-123", got.SessionID)
	}
	if got.SessionID == thr.ID {
		t.Error("SessionID must not equal threadID")
	}
}
