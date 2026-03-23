package stats

import (
	"regexp"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// prometheusHistogramBuckets are shared buckets for all histogram metrics.
// They cover latency (ms), token counts, and cost (USD) with a single set of
// boundaries — appropriate for a ConstHistogram approximation where we don't
// know the unit in advance.
var prometheusHistogramBuckets = []float64{
	.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 25, 50, 100, 250, 500, 1000,
}

// metricNameInvalidChars matches any character not allowed in a Prometheus metric name.
var metricNameInvalidChars = regexp.MustCompile(`[^a-zA-Z0-9_]`)

// sanitizeMetricName converts a Huginn metric name (e.g. "llm.latency_ms") to a
// valid Prometheus metric name (e.g. "huginn_llm_latency_ms").
func sanitizeMetricName(name string) string {
	// Replace dots and dashes with underscores.
	name = strings.NewReplacer(".", "_", "-", "_").Replace(name)
	// Strip any remaining invalid characters.
	name = metricNameInvalidChars.ReplaceAllString(name, "")
	// Prefix with huginn_ namespace.
	return "huginn_" + name
}

// PrometheusCollector implements prometheus.Collector over a stats.Registry.
// It is registered once at startup and calls Registry.Snapshot() lazily at
// each scrape — no per-request MustRegister, no panics.
//
// It is an "unchecked" collector: Describe() sends nothing, so the prometheus
// registry performs no advance schema validation. This is the recommended
// pattern for dynamic metric sets (see prometheus.UncheckedCollector).
type PrometheusCollector struct {
	reg *Registry
}

// NewPrometheusCollector creates a PrometheusCollector backed by reg.
func NewPrometheusCollector(reg *Registry) *PrometheusCollector {
	return &PrometheusCollector{reg: reg}
}

// Describe implements prometheus.Collector.
// We send nothing — this makes the collector "unchecked", which is correct
// for dynamic metric sets where names are only known at scrape time.
func (c *PrometheusCollector) Describe(_ chan<- *prometheus.Desc) {}

// Collect implements prometheus.Collector.
// Called by the prometheus HTTP handler at each scrape. Takes a snapshot of
// the Registry and emits Gauge metrics for the most-recent record of each
// metric name, plus ConstHistograms for histogram metrics.
func (c *PrometheusCollector) Collect(ch chan<- prometheus.Metric) {
	snap := c.reg.Snapshot()

	// Emit the most-recent value for each unique record metric name.
	// Deduplicate by name: last entry in the slice wins (most recent).
	seen := make(map[string]struct{}, len(snap.Records))
	// Iterate in reverse so we emit the latest record first.
	for i := len(snap.Records) - 1; i >= 0; i-- {
		entry := snap.Records[i]
		pname := sanitizeMetricName(entry.Metric)
		if _, already := seen[pname]; already {
			continue
		}
		seen[pname] = struct{}{}

		desc := prometheus.NewDesc(pname, entry.Metric, nil, nil)
		m, err := prometheus.NewConstMetric(desc, prometheus.GaugeValue, entry.Value)
		if err == nil {
			ch <- m
		}
	}

	// Emit ConstHistograms for histogram metrics.
	for metric, samples := range snap.HistValues {
		if len(samples) == 0 {
			continue
		}
		pname := sanitizeMetricName(metric) + "_hist"
		desc := prometheus.NewDesc(pname, metric+" histogram", nil, nil)

		buckets := make(map[float64]uint64, len(prometheusHistogramBuckets))
		var sum float64
		for _, v := range samples {
			sum += v
			for _, b := range prometheusHistogramBuckets {
				if v <= b {
					buckets[b]++
				}
			}
		}
		m, err := prometheus.NewConstHistogram(desc,
			uint64(len(samples)), sum, buckets)
		if err == nil {
			ch <- m
		}
	}
}
