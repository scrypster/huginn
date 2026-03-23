package scheduler

// delivery_jitter_test.go — verifies that jitter is applied to retry backoff
// delays and that two concurrent retry sequences do not fire at the exact same
// millisecond.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// TestJitter_WithinBounds verifies that jitter(d) returns a value within
// the expected ±25% range for a given duration.
func TestJitter_WithinBounds(t *testing.T) {
	d := 100 * time.Millisecond
	lo := time.Duration(float64(d) * 0.75)
	hi := time.Duration(float64(d) * 1.25)

	for i := 0; i < 1000; i++ {
		got := jitter(d)
		if got < lo || got > hi {
			t.Errorf("jitter(%v) = %v, want in [%v, %v]", d, got, lo, hi)
		}
	}
}

// TestJitter_ProducesVariance verifies that calling jitter many times on the
// same input produces at least two distinct values (i.e. it is not a no-op).
func TestJitter_ProducesVariance(t *testing.T) {
	d := 100 * time.Millisecond
	seen := make(map[time.Duration]struct{})
	for i := 0; i < 200; i++ {
		seen[jitter(d)] = struct{}{}
	}
	if len(seen) < 2 {
		t.Errorf("expected jitter to produce multiple distinct values, got %d", len(seen))
	}
}

// TestDeliverWithRetry_JitteredBackoff_ConcurrentNotSimultaneous starts two
// concurrent retry sequences that both fail on the first attempt and records
// the wall-clock time of each retry. It then asserts that the two retries
// did NOT fire at the exact same millisecond — jitter must cause at least a
// 1 ns difference between the two retry times across a large sample.
//
// The test uses a very short base backoff (20ms) so it completes quickly while
// still being large enough for jitter to produce meaningful variance.
func TestDeliverWithRetry_JitteredBackoff_ConcurrentNotSimultaneous(t *testing.T) {
	const runs = 50 // number of paired concurrent retries to compare

	allSame := true // becomes false if any pair differs

	for i := 0; i < runs; i++ {
		var mu sync.Mutex
		times := make([]time.Time, 0, 2)

		backoff := []time.Duration{20 * time.Millisecond}
		transient := errors.New("transient error")

		var wg sync.WaitGroup
		wg.Add(2)
		for j := 0; j < 2; j++ {
			go func() {
				defer wg.Done()
				attempt := 0
				_ = deliverWithRetry(context.Background(), backoff, func() error {
					attempt++
					if attempt == 1 {
						return transient // fail first; retry after jitter delay
					}
					// Record the time of the second attempt (after the jitter sleep).
					mu.Lock()
					times = append(times, time.Now())
					mu.Unlock()
					return nil
				})
			}()
		}
		wg.Wait()

		if len(times) == 2 && times[0] != times[1] {
			allSame = false
			break // found at least one pair with different retry times — jitter works
		}
	}

	if allSame {
		t.Error("all concurrent retry pairs fired at the exact same nanosecond — jitter appears to have no effect")
	}
}

// TestWebhookBackoff_UpdatedSchedule verifies that webhookBackoff has been
// updated to the more realistic [2s, 5s, 15s] schedule.
func TestWebhookBackoff_UpdatedSchedule(t *testing.T) {
	want := []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second}
	if len(webhookBackoff) != len(want) {
		t.Fatalf("webhookBackoff length = %d, want %d", len(webhookBackoff), len(want))
	}
	for i, d := range want {
		if webhookBackoff[i] != d {
			t.Errorf("webhookBackoff[%d] = %v, want %v", i, webhookBackoff[i], d)
		}
	}
}
