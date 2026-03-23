package stats

import (
	"math"
	"testing"
)

func TestHistogram_Percentiles(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	for i := 1; i <= 100; i++ {
		r.Histogram("latency", float64(i))
	}

	r.mu.Lock()
	vals := r.histValues["latency"]
	r.mu.Unlock()

	p50, p95, p99 := computePercentiles(vals)

	if math.Abs(p50-50) > 1 {
		t.Errorf("p50: got %.2f, want ~50", p50)
	}
	if math.Abs(p95-95) > 1 {
		t.Errorf("p95: got %.2f, want ~95", p95)
	}
	if math.Abs(p99-99) > 1 {
		t.Errorf("p99: got %.2f, want ~99", p99)
	}
}

func TestHistogram_KeyCapExceeded(t *testing.T) {
	t.Parallel()
	r := NewRegistry()

	// Fill up to the cap.
	for i := 0; i < maxHistogramKeys; i++ {
		r.Histogram("metric"+string(rune('A'+i%26))+string(rune('0'+i/26)), 1.0)
	}

	// Use a totally unique prefix to guarantee a new key.
	r.Histogram("overflow_key_257", 1.0)

	r.mu.Lock()
	count := len(r.histValues)
	r.mu.Unlock()

	if count > maxHistogramKeys {
		t.Errorf("expected at most %d histogram keys, got %d", maxHistogramKeys, count)
	}
}

func TestHistogram_TrimOnOverflow(t *testing.T) {
	t.Parallel()
	r := NewRegistry()

	for i := 0; i < maxHistogramSamples+1; i++ {
		r.Histogram("trim_test", float64(i))
	}

	r.mu.Lock()
	n := len(r.histValues["trim_test"])
	r.mu.Unlock()

	if n != histogramTrimTo {
		t.Errorf("expected %d values after trim, got %d", histogramTrimTo, n)
	}
}
