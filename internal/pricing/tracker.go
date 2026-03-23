package pricing

import (
	"fmt"
	"sync"
)

// UsageEntry records token usage and cost for a single model in a session.
type UsageEntry struct {
	Model            string
	PromptTokens     int
	CompletionTokens int
	Cost             float64
}

// SessionTracker tracks per-session cost. Safe for concurrent use.
type SessionTracker struct {
	mu      sync.Mutex
	table   map[string]PricingEntry
	entries map[string]*UsageEntry
}

// NewSessionTracker creates a tracker backed by the given pricing table.
func NewSessionTracker(table map[string]PricingEntry) *SessionTracker {
	return &SessionTracker{
		table:   table,
		entries: make(map[string]*UsageEntry),
	}
}

// Add records token usage for a model and accumulates cost.
func (t *SessionTracker) Add(model string, promptTokens, completionTokens int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[model]
	if !ok {
		e = &UsageEntry{Model: model}
		t.entries[model] = e
	}
	e.PromptTokens += promptTokens
	e.CompletionTokens += completionTokens
	e.Cost += CalculateCost(t.table, model, promptTokens, completionTokens)
}

// SessionCost returns total USD cost for the current session.
func (t *SessionTracker) SessionCost() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	var total float64
	for _, e := range t.entries {
		total += e.Cost
	}
	return total
}

// Reset clears all accumulated session data.
func (t *SessionTracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = make(map[string]*UsageEntry)
}

// Breakdown returns usage entries for all models used in the session.
func (t *SessionTracker) Breakdown() []UsageEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]UsageEntry, 0, len(t.entries))
	for _, e := range t.entries {
		result = append(result, *e)
	}
	return result
}

// StatusBarText returns cost string for display. Empty if cost is $0.00.
func (t *SessionTracker) StatusBarText() string {
	cost := t.SessionCost()
	if cost == 0 {
		return ""
	}
	return FormatCost(cost)
}

// FormatCost formats a USD cost for display.
func FormatCost(cost float64) string {
	if cost == 0 {
		return "$0.00"
	}
	return fmt.Sprintf("~$%.4f", cost)
}
