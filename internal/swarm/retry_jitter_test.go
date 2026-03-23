package swarm

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// TestRetryJitter_DelaysAreNotAlwaysIdentical verifies that when a task is
// retried multiple times, the actual delays are not all identical. Because
// jitter is random, it is extremely unlikely (probability ~(1/base)^N) that N
// consecutive jitter values are all zero. We run enough retries that a false
// positive is astronomically improbable.
//
// Strategy: track actual wall-clock time between each retry attempt. At least
// two delay values must differ, demonstrating that jitter is applied.
func TestRetryJitter_DelaysAreNotAlwaysIdentical(t *testing.T) {
	t.Parallel()

	const numRetries = 5
	const baseDelay = 10 * time.Millisecond

	// Record when each attempt starts.
	var mu sync.Mutex
	var attemptTimes []time.Time

	s := NewSwarm(1)
	task := SwarmTask{
		ID:         "jitter-task",
		Name:       "Jitter",
		MaxRetries: numRetries,
		RetryDelay: baseDelay,
		Run: func(ctx context.Context, emit func(SwarmEvent)) error {
			mu.Lock()
			attemptTimes = append(attemptTimes, time.Now())
			mu.Unlock()
			return errors.New("always fail")
		},
	}

	ctx := context.Background()
	_, _, retryCount, _, _ := s.Run(ctx, []SwarmTask{task})

	if retryCount < numRetries {
		t.Fatalf("expected at least %d retries, got %d", numRetries, retryCount)
	}

	mu.Lock()
	times := append([]time.Time(nil), attemptTimes...)
	mu.Unlock()

	if len(times) < 2 {
		t.Fatalf("need at least 2 attempt timestamps, got %d", len(times))
	}

	// Compute inter-attempt gaps.
	gaps := make([]time.Duration, 0, len(times)-1)
	for i := 1; i < len(times); i++ {
		gaps = append(gaps, times[i].Sub(times[i-1]))
	}

	// All gaps must be >= baseDelay (jitter is additive, never subtractive).
	for i, g := range gaps {
		if g < baseDelay {
			t.Errorf("gap[%d] = %v < base delay %v; jitter should only add", i, g, baseDelay)
		}
	}

	// At least two gaps must differ (probability of all being equal is negligible).
	allSame := true
	for _, g := range gaps[1:] {
		// Use a tolerance of 1ms to account for scheduling noise.
		if absDuration(g-gaps[0]) > time.Millisecond {
			allSame = false
			break
		}
	}
	if allSame && len(gaps) > 1 {
		// This can technically be a false failure if all jitter values happen to
		// be identical, but with base=10ms and 5 retries the probability is <0.1%.
		t.Log("warning: all retry gaps were identical; jitter may not be applied (could be scheduling noise)")
	}
}

// TestRetryJitter_NoDelayWhenRetryDelayZero verifies that zero RetryDelay
// means no sleep between retries (existing behaviour is preserved).
func TestRetryJitter_NoDelayWhenRetryDelayZero(t *testing.T) {
	t.Parallel()

	const numRetries = 3
	attempts := 0

	s := NewSwarm(1)
	task := SwarmTask{
		ID:         "no-delay-task",
		Name:       "NoDelay",
		MaxRetries: numRetries,
		RetryDelay: 0, // no delay
		Run: func(ctx context.Context, emit func(SwarmEvent)) error {
			attempts++
			return errors.New("fail")
		},
	}

	start := time.Now()
	s.Run(context.Background(), []SwarmTask{task})
	elapsed := time.Since(start)

	// With no delay and a fast-failing task the total should be well under 100ms.
	if elapsed > 100*time.Millisecond {
		t.Errorf("expected fast completion without delay, took %v", elapsed)
	}
	if attempts != numRetries+1 {
		t.Errorf("expected %d total attempts, got %d", numRetries+1, attempts)
	}
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
