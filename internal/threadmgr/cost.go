package threadmgr

import (
	"errors"
	"sync"

	"github.com/scrypster/huginn/internal/pricing"
)

var ErrBudgetExceeded = errors.New("session cost budget exceeded")

// lookupPrice resolves per-1M-token pricing for model. This used to be a
// private substring table that disagreed with internal/pricing/table.go on
// several entries (most notably claude-opus-4 pricing). It now defers to
// pricing.Lookup against pricing.DefaultTable — internal/pricing is the
// single verified source of pricing truth for the whole codebase. Unknown
// models (e.g. local Ollama models) resolve to a zero entry, same as before.
func lookupPrice(model string) pricing.PricingEntry {
	entry, _ := pricing.Lookup(pricing.DefaultTable, model)
	return entry
}

// CostSinkFn is an optional callback invoked (under lock-release, non-blocking) each
// time Record computes a positive cost. threadID may be used as a surrogate session_id
// when persisting to storage. Implementations must not block.
type CostSinkFn func(threadID string, costUSD float64, promptTokens, completionTokens int)

type CostAccumulator struct {
	mu           sync.RWMutex
	ThreadCosts  map[string]float64
	SessionTotal float64
	GlobalBudget float64
	sink         CostSinkFn // optional; nil = no-op
}

func NewCostAccumulator(budgetUSD float64) *CostAccumulator {
	return &CostAccumulator{
		ThreadCosts:  make(map[string]float64),
		GlobalBudget: budgetUSD,
	}
}

// SetCostSink installs fn as the post-Record callback. Thread-safe; may be called
// before or after any Record calls. Replaces any previously installed sink.
func (ca *CostAccumulator) SetCostSink(fn CostSinkFn) {
	ca.mu.Lock()
	ca.sink = fn
	ca.mu.Unlock()
}

func (ca *CostAccumulator) Record(threadID string, promptTokens, completionTokens int, model string) {
	price := lookupPrice(model)
	cost := price.PromptPer1M*float64(promptTokens)/1_000_000 +
		price.CompletionPer1M*float64(completionTokens)/1_000_000

	ca.mu.Lock()
	ca.ThreadCosts[threadID] += cost
	ca.SessionTotal += cost
	sink := ca.sink
	ca.mu.Unlock()

	if sink != nil && (promptTokens > 0 || completionTokens > 0) {
		sink(threadID, cost, promptTokens, completionTokens)
	}
}

// Total returns the current session total cost in USD. Thread-safe.
func (ca *CostAccumulator) Total() float64 {
	ca.mu.RLock()
	defer ca.mu.RUnlock()
	return ca.SessionTotal
}

func (ca *CostAccumulator) CheckBudget() error {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	if ca.GlobalBudget <= 0 {
		return nil
	}
	if ca.SessionTotal >= ca.GlobalBudget {
		return ErrBudgetExceeded
	}
	return nil
}
