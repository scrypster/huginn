package compact

import (
	"context"
	"fmt"

	"github.com/scrypster/huginn/internal/backend"
)

// Mode controls when automatic message compaction is triggered.
type Mode string

const (
	ModeAuto   Mode = "auto"
	ModeNever  Mode = "never"
	ModeAlways Mode = "always"
)

// CompactionStrategy defines how to compact a message history.
type CompactionStrategy interface {
	ShouldCompact(messages []backend.Message, budgetTokens int) bool
	Compact(ctx context.Context, messages []backend.Message, budget int, b backend.Backend, model string) ([]backend.Message, error)
}

// Config holds configuration for the Compactor.
type Config struct {
	Mode         Mode
	Trigger      float64
	BudgetTokens int
	Strategy     CompactionStrategy
}

// Compactor manages smart context compaction.
type Compactor struct {
	cfg Config
}

// New creates a new Compactor with the given configuration.
func New(cfg Config) *Compactor {
	if cfg.Trigger <= 0 {
		cfg.Trigger = 0.7
	}
	if cfg.BudgetTokens <= 0 {
		cfg.BudgetTokens = 32_000
	}
	return &Compactor{cfg: cfg}
}

// MaybeCompact compacts the message history if appropriate for the configured mode.
// Uses the backend's actual context window for the compaction budget.
// Returns (compacted_history, was_compacted, error).
func (c *Compactor) MaybeCompact(ctx context.Context, messages []backend.Message, b backend.Backend, model string) ([]backend.Message, bool, error) {
	if c.cfg.Strategy == nil {
		return messages, false, fmt.Errorf("compact: no strategy configured")
	}
	// Use backend's context window as the token budget
	budget := c.cfg.BudgetTokens
	if b != nil {
		budget = b.ContextWindow()
		if budget <= 0 {
			budget = c.cfg.BudgetTokens
		}
	}

	switch c.cfg.Mode {
	case ModeNever:
		return messages, false, nil
	case ModeAlways:
		result, err := c.cfg.Strategy.Compact(ctx, messages, budget, b, model)
		if err != nil {
			return messages, false, fmt.Errorf("compact: %w", err)
		}
		return result, true, nil
	default: // Auto
		if !c.cfg.Strategy.ShouldCompact(messages, budget) {
			return messages, false, nil
		}
		result, err := c.cfg.Strategy.Compact(ctx, messages, budget, b, model)
		if err != nil {
			return messages, false, fmt.Errorf("compact: %w", err)
		}
		return result, true, nil
	}
}

