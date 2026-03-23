package stats_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/stats"
)

// BenchmarkHistogram_ConcurrentWrites validates that the Registry mutex does
// not become a bottleneck at the expected call volume (~100 obs/sec across
// ~8 call sites). See Registry godoc for context.
func BenchmarkHistogram_ConcurrentWrites(b *testing.B) {
	r := stats.NewRegistry()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			r.Histogram("bench.latency", float64(i%100), "method", "test")
			i++
		}
	})
}

// BenchmarkRegistry_Collector_Record validates Record throughput under contention.
func BenchmarkRegistry_Collector_Record(b *testing.B) {
	r := stats.NewRegistry()
	c := r.Collector()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			c.Record("bench.counter", float64(i%50), "op", "test")
			i++
		}
	})
}
