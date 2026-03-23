package threadmgr

import (
	"errors"
	"testing"
)

func TestCostAccumulator_RecordAndTotal(t *testing.T) {
	ca := NewCostAccumulator(10.0)
	ca.Record("t-1", 1_000_000, 500_000, "claude-sonnet-4")
	// sonnet: $3/1M prompt, $15/1M completion
	// cost = 3.00 + 7.50 = $10.50
	ca.mu.Lock()
	total := ca.SessionTotal
	threadCost := ca.ThreadCosts["t-1"]
	ca.mu.Unlock()

	if total <= 0 {
		t.Errorf("expected positive total, got %f", total)
	}
	if threadCost != total {
		t.Errorf("thread cost %f should equal session total %f when single thread", threadCost, total)
	}
}

func TestCostAccumulator_OllamaZeroCost(t *testing.T) {
	ca := NewCostAccumulator(100.0)
	ca.Record("t-1", 9_999_999, 9_999_999, "llama3:8b")
	ca.mu.Lock()
	total := ca.SessionTotal
	ca.mu.Unlock()
	if total != 0.0 {
		t.Errorf("Ollama/unknown model should cost $0, got %f", total)
	}
}

func TestCostAccumulator_CheckBudget_UnderBudget(t *testing.T) {
	ca := NewCostAccumulator(100.0)
	ca.Record("t-1", 100, 100, "claude-haiku-4")
	if err := ca.CheckBudget(); err != nil {
		t.Errorf("expected no error under budget, got: %v", err)
	}
}

func TestCostAccumulator_CheckBudget_Exceeded(t *testing.T) {
	ca := NewCostAccumulator(0.001) // $0.001 budget — will be exceeded immediately
	ca.Record("t-1", 1_000_000, 1_000_000, "claude-opus-4")
	err := ca.CheckBudget()
	if err == nil {
		t.Fatal("expected ErrBudgetExceeded, got nil")
	}
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Errorf("expected ErrBudgetExceeded, got %T: %v", err, err)
	}
}

func TestCostAccumulator_MultipleThreads(t *testing.T) {
	ca := NewCostAccumulator(100.0)
	ca.Record("t-1", 100_000, 50_000, "claude-haiku-4")
	ca.Record("t-2", 100_000, 50_000, "claude-haiku-4")
	ca.mu.Lock()
	t1 := ca.ThreadCosts["t-1"]
	t2 := ca.ThreadCosts["t-2"]
	total := ca.SessionTotal
	ca.mu.Unlock()
	if t1 <= 0 || t2 <= 0 {
		t.Errorf("each thread should have positive cost; t1=%f t2=%f", t1, t2)
	}
	if total != t1+t2 {
		t.Errorf("session total %f should equal t1+t2 = %f", total, t1+t2)
	}
}

func TestCostAccumulator_Total_ReturnsSessionTotal(t *testing.T) {
	ca := NewCostAccumulator(0)
	// Before any recording, total should be 0
	if ca.Total() != 0 {
		t.Errorf("expected 0, got %f", ca.Total())
	}
	// After recording, Total() should return a positive value.
	ca.Record("t-1", 1_000_000, 1_000_000, "claude-sonnet-4")
	ca.mu.RLock()
	total := ca.SessionTotal
	ca.mu.RUnlock()
	if ca.Total() != total {
		t.Errorf("Total() %f != SessionTotal %f", ca.Total(), total)
	}
	if ca.Total() <= 0 {
		t.Errorf("expected positive total after recording, got %f", ca.Total())
	}
}

func TestCostAccumulator_ZeroBudgetNeverBlocks(t *testing.T) {
	ca := NewCostAccumulator(0) // 0 = unlimited
	ca.Record("t-1", 999_999_999, 999_999_999, "claude-opus-4")
	if err := ca.CheckBudget(); err != nil {
		t.Errorf("zero budget means unlimited; expected no error, got: %v", err)
	}
}
