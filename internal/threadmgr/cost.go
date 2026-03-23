package threadmgr

import (
	"errors"
	"strings"
	"sync"
)

var ErrBudgetExceeded = errors.New("session cost budget exceeded")

type pricingEntry struct {
	inputPerM  float64
	outputPerM float64
}

var modelPricing = []struct {
	substr string
	price  pricingEntry
}{
	{"claude-opus-4", pricingEntry{inputPerM: 15.00, outputPerM: 75.00}},
	{"claude-sonnet-4", pricingEntry{inputPerM: 3.00, outputPerM: 15.00}},
	{"claude-haiku-4", pricingEntry{inputPerM: 0.80, outputPerM: 4.00}},
	{"gpt-4o-mini", pricingEntry{inputPerM: 0.15, outputPerM: 0.60}},
	{"gpt-4o", pricingEntry{inputPerM: 2.50, outputPerM: 10.00}},
	{"gpt-4", pricingEntry{inputPerM: 30.00, outputPerM: 60.00}},
}

func lookupPrice(model string) pricingEntry {
	lower := strings.ToLower(model)
	for _, entry := range modelPricing {
		if strings.Contains(lower, entry.substr) {
			return entry.price
		}
	}
	return pricingEntry{}
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
	cost := price.inputPerM*float64(promptTokens)/1_000_000 +
		price.outputPerM*float64(completionTokens)/1_000_000

	ca.mu.Lock()
	ca.ThreadCosts[threadID] += cost
	ca.SessionTotal += cost
	sink := ca.sink
	ca.mu.Unlock()

	if sink != nil && cost > 0 {
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
