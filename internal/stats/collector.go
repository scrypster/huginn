package stats

import (
	"sort"
	"sync"
	"time"
)

// maxEntries is the maximum number of records or histograms kept in memory.
// Oldest entries are dropped when the cap is exceeded.
const maxEntries = 10_000

// Per-metric histogram constants.
const (
	maxHistogramSamples = 1000 // max samples stored per unique metric key
	histogramTrimTo     = 800  // trim to this count when maxHistogramSamples is exceeded
	maxHistogramKeys    = 256  // max distinct histogram metric keys
)

// Collector is the interface for recording metrics.
// It is injected into every layer via constructor — no globals.
// Pass NoopCollector{} in tests.
type Collector interface {
	// Record records a single gauge/counter value.
	Record(metric string, value float64, tags ...string)
	// Histogram records a distribution value (e.g. latency).
	Histogram(metric string, value float64, tags ...string)
}

// NoopCollector discards all metrics. Use in tests.
type NoopCollector struct{}

func (NoopCollector) Record(metric string, value float64, tags ...string)    {}
func (NoopCollector) Histogram(metric string, value float64, tags ...string) {}

// metricEntry is a single recorded data point.
type metricEntry struct {
	Metric string
	Value  float64
	Tags   []string
	Time   time.Time
}

// Stats is a snapshot of all collected metrics.
type Stats struct {
	Records    []metricEntry
	Histograms []metricEntry
	// HistValues holds per-metric sample slices for percentile computation.
	HistValues map[string][]float64
}

// Registry collects metrics from all layers and exposes snapshots.
// It is thread-safe.
type Registry struct {
	mu         sync.Mutex
	records    []metricEntry
	histograms []metricEntry
	// histValues stores raw per-metric samples for percentile computation.
	histValues map[string][]float64
}

// NewRegistry creates a new Registry.
func NewRegistry() *Registry {
	return &Registry{
		histValues: make(map[string][]float64),
	}
}

// Histogram records a distribution sample directly on the Registry.
// It populates histValues for percentile computation and enforces
// per-key sample caps (maxHistogramSamples) and key count limits (maxHistogramKeys).
// Optional tags are accepted for API compatibility but not stored per-key.
func (r *Registry) Histogram(metric string, value float64, tags ...string) {
	_ = tags
	r.mu.Lock()
	defer r.mu.Unlock()
	vals, exists := r.histValues[metric]
	if !exists {
		if len(r.histValues) >= maxHistogramKeys {
			// Key cap exceeded — drop the new metric silently.
			return
		}
	}
	vals = append(vals, value)
	if len(vals) > maxHistogramSamples {
		// Trim oldest samples to keep memory bounded.
		vals = vals[len(vals)-histogramTrimTo:]
	}
	r.histValues[metric] = vals
}

// Collector returns a Collector backed by this Registry.
func (r *Registry) Collector() Collector {
	return &registryCollector{r: r}
}

// Snapshot returns a point-in-time copy of all collected metrics.
func (r *Registry) Snapshot() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap := Stats{
		Records:    make([]metricEntry, len(r.records)),
		Histograms: make([]metricEntry, len(r.histograms)),
		HistValues: make(map[string][]float64, len(r.histValues)),
	}
	copy(snap.Records, r.records)
	copy(snap.Histograms, r.histograms)
	for k, v := range r.histValues {
		cp := make([]float64, len(v))
		copy(cp, v)
		snap.HistValues[k] = cp
	}
	return snap
}

// Reset clears all collected metrics.
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = r.records[:0]
	r.histograms = r.histograms[:0]
	r.histValues = make(map[string][]float64)
}

type registryCollector struct {
	r *Registry
}

func (c *registryCollector) Record(metric string, value float64, tags ...string) {
	c.r.mu.Lock()
	defer c.r.mu.Unlock()
	c.r.records = append(c.r.records, metricEntry{
		Metric: metric,
		Value:  value,
		Tags:   tags,
		Time:   time.Now(),
	})
	if len(c.r.records) > maxEntries {
		c.r.records = c.r.records[len(c.r.records)-maxEntries:]
	}
}

func (c *registryCollector) Histogram(metric string, value float64, tags ...string) {
	c.r.mu.Lock()
	defer c.r.mu.Unlock()
	c.r.histograms = append(c.r.histograms, metricEntry{
		Metric: metric,
		Value:  value,
		Tags:   tags,
		Time:   time.Now(),
	})
	if len(c.r.histograms) > maxEntries {
		c.r.histograms = c.r.histograms[len(c.r.histograms)-maxEntries:]
	}
}

// computePercentiles returns the p50, p95, and p99 percentiles of vals.
// Returns 0, 0, 0 for an empty slice. The input slice is not modified.
func computePercentiles(vals []float64) (p50, p95, p99 float64) {
	if len(vals) == 0 {
		return 0, 0, 0
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)
	return percentileAt(sorted, 50), percentileAt(sorted, 95), percentileAt(sorted, 99)
}

// percentileAt returns the value at percentile p (0–100) of a sorted slice.
// Assumes vals is already sorted in ascending order.
func percentileAt(vals []float64, p float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	if p <= 0 {
		return vals[0]
	}
	if p >= 100 {
		return vals[len(vals)-1]
	}
	idx := p / 100 * float64(len(vals)-1)
	lo := int(idx)
	hi := lo + 1
	if hi >= len(vals) {
		return vals[lo]
	}
	frac := idx - float64(lo)
	return vals[lo]*(1-frac) + vals[hi]*frac
}
