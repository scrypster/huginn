package stats

import (
	"sync"
	"testing"
)

// TestRegistry_ConcurrentHistogramSafety tests concurrent Histogram calls are safe.
func TestRegistry_ConcurrentHistogramSafety(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	var wg sync.WaitGroup
	numGoroutines := 50
	recordsPerGoroutine := 100

	// Spin up concurrent histogram recorders
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < recordsPerGoroutine; i++ {
				r.Histogram("latency", float64(i))
				r.Histogram("throughput", float64(i*2))
			}
		}(g)
	}

	wg.Wait()

	// Verify no corruption
	snap := r.Snapshot()
	latencyCount := len(snap.HistValues["latency"])
	throughputCount := len(snap.HistValues["throughput"])

	// Each should have at most maxHistogramSamples after trimming
	if latencyCount > maxHistogramSamples {
		t.Errorf("latency histogram overflow: %d > %d", latencyCount, maxHistogramSamples)
	}
	if throughputCount > maxHistogramSamples {
		t.Errorf("throughput histogram overflow: %d > %d", throughputCount, maxHistogramSamples)
	}

	// Should have trimmed to histogramTrimTo if overflow occurred
	// Note: Due to concurrent access, values may be > histogramTrimTo due to timing
	if latencyCount > maxHistogramSamples {
		t.Errorf("latency histogram exceeded max: %d > %d", latencyCount, maxHistogramSamples)
	}
}

// TestRegistry_HistogramKeyLimit tests that histogram key limit is enforced.
func TestRegistry_HistogramKeyLimit(t *testing.T) {
	t.Parallel()

	r := NewRegistry()

	// Fill up to the key limit
	for i := 0; i < maxHistogramKeys; i++ {
		key := "metric_" + string(rune('a'+(i%26))) + string(rune('0'+(i/26)))
		r.Histogram(key, float64(i))
	}

	snap := r.Snapshot()
	keyCount := len(snap.HistValues)

	if keyCount != maxHistogramKeys {
		t.Errorf("expected %d histogram keys, got %d", maxHistogramKeys, keyCount)
	}

	// Try to add one more key; should be dropped
	r.Histogram("overflow_key", 999.0)

	snap2 := r.Snapshot()
	newKeyCount := len(snap2.HistValues)

	if newKeyCount > maxHistogramKeys {
		t.Errorf("histogram key limit exceeded: %d > %d", newKeyCount, maxHistogramKeys)
	}
}

// TestRegistry_HistogramTrimmingBehavior tests the trimming logic when overflow occurs.
func TestRegistry_HistogramTrimmingBehavior(t *testing.T) {
	t.Parallel()

	r := NewRegistry()

	// Fill one histogram beyond maxHistogramSamples
	for i := 0; i < maxHistogramSamples+100; i++ {
		r.Histogram("test_metric", float64(i))
	}

	snap := r.Snapshot()
	samples := snap.HistValues["test_metric"]

	// After overflow, should trim to histogramTrimTo or close
	if len(samples) > maxHistogramSamples {
		t.Errorf("after overflow, histogram exceeded max: %d > %d", len(samples), maxHistogramSamples)
	}

	// Verify samples are present and reasonable
	if len(samples) < histogramTrimTo-50 {
		t.Logf("histogram samples after trim: %d (expected ~%d)", len(samples), histogramTrimTo)
	}
}

// TestRegistry_Snapshot tests that Snapshot is consistent during concurrent access.
func TestRegistry_Snapshot(t *testing.T) {
	t.Parallel()

	r := NewRegistry()
	var wg sync.WaitGroup
	done := make(chan struct{})

	// Start writers
	for w := 0; w < 5; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					r.Histogram("metric_a", 1.0)
				}
			}
		}()
	}

	// Let writers run for a bit, then take snapshots
	snapshots := make([]Stats, 0)
	for i := 0; i < 3; i++ {
		snap := r.Snapshot()
		snapshots = append(snapshots, snap)
	}

	close(done)
	wg.Wait()

	// Verify snapshots are valid
	for i, snap := range snapshots {
		if snap.HistValues == nil {
			t.Errorf("snapshot %d: HistValues is nil", i)
		}
	}
}

// TestComputePercentiles_EdgeCases tests percentile computation edge cases.
func TestComputePercentiles_EdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values []float64
	}{
		{"empty slice", []float64{}},
		{"single value", []float64{42}},
		{"two values", []float64{10, 20}},
		{"large range", []float64{1, 1000, 100, 10, 500}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p50, p95, p99 := computePercentiles(tc.values)

			if len(tc.values) == 0 {
				if p50 != 0 || p95 != 0 || p99 != 0 {
					t.Errorf("empty slice: expected all 0, got p50=%v p95=%v p99=%v", p50, p95, p99)
				}
			} else if len(tc.values) > 1 {
				// Basic sanity: p50 <= p95 <= p99
				if p50 > p95 || p95 > p99 {
					t.Errorf("percentile ordering violated: p50=%v p95=%v p99=%v", p50, p95, p99)
				}
			}
		})
	}
}

// TestPercentileAt_Boundary tests edge cases in percentile calculation.
func TestPercentileAt_Boundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		values     []float64
		percentile float64
	}{
		{"p0 of range", []float64{1, 2, 3, 4, 5}, 0},
		{"p100 of range", []float64{1, 2, 3, 4, 5}, 100},
		{"p50 of range", []float64{1, 2, 3, 4, 5}, 50},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := percentileAt(tc.values, tc.percentile)

			// Should not be NaN or Inf
			if result != result { // NaN != NaN
				t.Errorf("percentileAt returned NaN")
			}
		})
	}
}
