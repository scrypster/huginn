package pricing

import (
	"testing"
)

// TestSessionTracker_Float64Precision documents the float64 accumulation
// precision limit for cost tracking.
//
// P3-1: SessionTracker uses float64 += for accumulated cost. At millions of
// tokens across many sessions, floating-point rounding introduces error.
//
// This test verifies the error stays within 0.01% for typical usage.
// Float64 has 52-bit mantissa (~15-17 significant decimal digits), so for
// $0.01 per 1M tokens across 100M tokens = $1.00, rounding error is
// on the order of 1e-13 USD — negligible for display purposes.
//
// Conclusion: float64 precision is ACCEPTABLE for this use case.
// Documented here as a regression guard; no code change needed.
func TestSessionTracker_Float64Precision(t *testing.T) {
	t.Parallel()

	// Simulate 1000 Add() calls of 100k tokens each = 100M total tokens.
	// At a hypothetical rate of $0.01/1M tokens, total cost ≈ $1.00.
	tracker := NewSessionTracker(map[string]PricingEntry{
		"test-model": {
			PromptPer1M:     0.01,
			CompletionPer1M: 0.01,
		},
	})

	const calls = 1000
	const tokensPerCall = 100_000

	for i := 0; i < calls; i++ {
		tracker.Add("test-model", tokensPerCall, 0)
	}

	totalCost := tracker.SessionCost()
	expectedCost := float64(calls) * float64(tokensPerCall) / 1_000_000 * 0.01

	// Tolerance: 0.01% of expected value.
	tolerance := expectedCost * 0.0001
	diff := totalCost - expectedCost
	if diff < 0 {
		diff = -diff
	}

	if diff > tolerance {
		t.Errorf("float64 precision error too large: expected %.10f, got %.10f, diff %.2e (tolerance %.2e)",
			expectedCost, totalCost, diff, tolerance)
	}
	t.Logf("P3-1 float64 precision: expected=%.10f got=%.10f diff=%.2e (within %.4f%%)",
		expectedCost, totalCost, diff, diff/expectedCost*100)
}
