package compact_test

import (
	"context"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/compact"
)

// TestExtractiveCompact_ZeroMessages verifies that compacting an empty slice
// does not panic and returns at least the summary message.
func TestExtractiveCompact_ZeroMessages(t *testing.T) {
	s := compact.NewExtractiveStrategy()
	result, err := s.Compact(context.Background(), []backend.Message{}, 1000, nil, "")
	if err != nil {
		t.Fatalf("Compact(empty): unexpected error: %v", err)
	}
	if len(result) == 0 {
		t.Error("Compact(empty): expected at least the summary message, got none")
	}
}

// TestShouldCompact_ZeroMessages verifies that ShouldCompact never returns
// true for an empty message slice (0 tokens cannot exceed any threshold).
func TestShouldCompact_ZeroMessages(t *testing.T) {
	s := compact.NewExtractiveStrategy()
	if s.ShouldCompact([]backend.Message{}, 100) {
		t.Error("ShouldCompact(empty, 100): expected false for empty message list")
	}
}

// TestExtractiveCompact_SingleOversizedMessage verifies that a single message
// whose token count exceeds the budget does not cause an infinite loop or
// panic. The extractive strategy must always terminate.
func TestExtractiveCompact_SingleOversizedMessage(t *testing.T) {
	// Build a message whose estimated token count exceeds the budget by far.
	huge := strings.Repeat("word ", 50_000) // ~50 000 words → ~50 000 tokens
	msgs := []backend.Message{
		{Role: "user", Content: huge},
	}
	s := compact.NewExtractiveStrategy()

	// Budget is 1 token — the single message vastly exceeds it.
	// The strategy must still return (no infinite loop) and produce a result.
	result, err := s.Compact(context.Background(), msgs, 1, nil, "")
	if err != nil {
		t.Fatalf("Compact(oversized): unexpected error: %v", err)
	}
	if len(result) == 0 {
		t.Error("Compact(oversized): expected at least the summary message, got none")
	}
}

// TestMaybeCompact_NeverMode_ZeroMessages verifies ModeNever with empty
// input returns the same empty slice without error.
func TestMaybeCompact_NeverMode_ZeroMessages(t *testing.T) {
	c := compact.New(compact.Config{
		Mode:         compact.ModeNever,
		Strategy:     compact.NewExtractiveStrategy(),
		BudgetTokens: 1000,
	})
	result, compacted, err := c.MaybeCompact(context.Background(), []backend.Message{}, nil, "")
	if err != nil {
		t.Fatalf("MaybeCompact(Never, empty): unexpected error: %v", err)
	}
	if compacted {
		t.Error("MaybeCompact(Never, empty): expected compacted=false")
	}
	if len(result) != 0 {
		t.Errorf("MaybeCompact(Never, empty): expected 0 messages back, got %d", len(result))
	}
}

// TestMaybeCompact_AlwaysMode_ZeroMessages verifies ModeAlways with empty
// input produces a summary message (the extractive summary header) and no error.
func TestMaybeCompact_AlwaysMode_ZeroMessages(t *testing.T) {
	c := compact.New(compact.Config{
		Mode:         compact.ModeAlways,
		Strategy:     compact.NewExtractiveStrategy(),
		BudgetTokens: 1000,
	})
	result, compacted, err := c.MaybeCompact(context.Background(), []backend.Message{}, nil, "")
	if err != nil {
		t.Fatalf("MaybeCompact(Always, empty): unexpected error: %v", err)
	}
	if !compacted {
		t.Error("MaybeCompact(Always, empty): expected compacted=true")
	}
	if len(result) == 0 {
		t.Error("MaybeCompact(Always, empty): expected summary message in result")
	}
}
