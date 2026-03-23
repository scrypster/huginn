package stats

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// NoopCollector
// ---------------------------------------------------------------------------

func TestNoopCollector_Record(t *testing.T) {
	t.Parallel()
	var c NoopCollector
	// Must not panic regardless of inputs.
	c.Record("metric", 1.0)
	c.Record("metric", -999.99, "tag1", "tag2")
	c.Record("", 0)
}

func TestNoopCollector_Histogram(t *testing.T) {
	t.Parallel()
	var c NoopCollector
	c.Histogram("latency", 42.5)
	c.Histogram("latency", 0, "env:prod")
	c.Histogram("", -1)
}

// ---------------------------------------------------------------------------
// Registry – basic flow
// ---------------------------------------------------------------------------

func TestNewRegistry_NotNil(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
}

func TestRegistry_SnapshotEmpty(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	snap := r.Snapshot()
	if len(snap.Records) != 0 {
		t.Errorf("expected 0 records, got %d", len(snap.Records))
	}
	if len(snap.Histograms) != 0 {
		t.Errorf("expected 0 histograms, got %d", len(snap.Histograms))
	}
}

func TestRegistry_RecordAndSnapshot(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	c := r.Collector()

	before := time.Now()
	c.Record("cpu", 0.75, "host:a")
	after := time.Now()

	snap := r.Snapshot()
	if len(snap.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(snap.Records))
	}
	e := snap.Records[0]
	if e.Metric != "cpu" {
		t.Errorf("metric: got %q, want %q", e.Metric, "cpu")
	}
	if e.Value != 0.75 {
		t.Errorf("value: got %v, want 0.75", e.Value)
	}
	if len(e.Tags) != 1 || e.Tags[0] != "host:a" {
		t.Errorf("tags: got %v, want [host:a]", e.Tags)
	}
	if e.Time.Before(before) || e.Time.After(after) {
		t.Errorf("timestamp out of range: %v", e.Time)
	}
}

func TestRegistry_HistogramAndSnapshot(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	c := r.Collector()

	c.Histogram("req.latency", 123.4, "route:/api")

	snap := r.Snapshot()
	if len(snap.Histograms) != 1 {
		t.Fatalf("expected 1 histogram, got %d", len(snap.Histograms))
	}
	e := snap.Histograms[0]
	if e.Metric != "req.latency" {
		t.Errorf("metric: got %q", e.Metric)
	}
	if e.Value != 123.4 {
		t.Errorf("value: got %v", e.Value)
	}
}

func TestRegistry_MultipleEntries(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	c := r.Collector()

	for i := 0; i < 10; i++ {
		c.Record("counter", float64(i))
		c.Histogram("latency", float64(i)*2)
	}

	snap := r.Snapshot()
	if len(snap.Records) != 10 {
		t.Errorf("records: got %d, want 10", len(snap.Records))
	}
	if len(snap.Histograms) != 10 {
		t.Errorf("histograms: got %d, want 10", len(snap.Histograms))
	}
}

func TestRegistry_Reset(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	c := r.Collector()

	c.Record("m", 1.0)
	c.Histogram("h", 2.0)

	r.Reset()
	snap := r.Snapshot()
	if len(snap.Records) != 0 {
		t.Errorf("after Reset, records: got %d, want 0", len(snap.Records))
	}
	if len(snap.Histograms) != 0 {
		t.Errorf("after Reset, histograms: got %d, want 0", len(snap.Histograms))
	}
}

func TestRegistry_ResetThenRecord(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	c := r.Collector()

	c.Record("old", 1.0)
	r.Reset()
	c.Record("new", 2.0)

	snap := r.Snapshot()
	if len(snap.Records) != 1 {
		t.Fatalf("expected 1 record after reset+record, got %d", len(snap.Records))
	}
	if snap.Records[0].Metric != "new" {
		t.Errorf("expected 'new', got %q", snap.Records[0].Metric)
	}
}

// ---------------------------------------------------------------------------
// Registry – snapshot isolation (mutations to snapshot don't affect registry)
// ---------------------------------------------------------------------------

func TestRegistry_SnapshotIsIsolated(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	c := r.Collector()
	c.Record("x", 1.0)

	snap1 := r.Snapshot()
	// Mutate the snapshot slice
	snap1.Records[0] = metricEntry{Metric: "tampered"}

	snap2 := r.Snapshot()
	if snap2.Records[0].Metric != "x" {
		t.Errorf("snapshot mutation leaked into registry: got %q", snap2.Records[0].Metric)
	}
}

// ---------------------------------------------------------------------------
// Registry – concurrent access (data race detection via -race)
// ---------------------------------------------------------------------------

func TestRegistry_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	c := r.Collector()

	var wg sync.WaitGroup
	const goroutines = 50
	const opsEach = 100

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsEach; j++ {
				c.Record("m", float64(j))
				c.Histogram("h", float64(j))
			}
		}(i)
	}

	// Snapshot concurrently while writers run
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				_ = r.Snapshot()
			}
		}
	}()

	wg.Wait()
	close(done)

	snap := r.Snapshot()
	total := len(snap.Records) + len(snap.Histograms)
	if total == 0 {
		t.Error("expected some entries after concurrent writes")
	}
}

func TestRegistry_ConcurrentResetAndRecord(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	c := r.Collector()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			c.Record("m", float64(i))
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			r.Reset()
		}
	}()

	wg.Wait()
	// Just verify no panic and state is consistent
	_ = r.Snapshot()
}

// ---------------------------------------------------------------------------
// Collector interface compliance
// ---------------------------------------------------------------------------

func TestRegistry_CollectorImplementsInterface(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	var _ Collector = r.Collector()
	var _ Collector = NoopCollector{}
}

// ---------------------------------------------------------------------------
// FormatTable
// ---------------------------------------------------------------------------

func TestFormatTable_Empty(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	snap := r.Snapshot()
	out := FormatTable(snap)
	if !strings.Contains(out, "no stats collected yet") {
		t.Errorf("expected 'no stats collected yet', got: %q", out)
	}
}

func TestFormatTable_RecordsOnly(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	c := r.Collector()
	c.Record("index.files", 42.0)
	c.Record("index.errors", 3.0)

	snap := r.Snapshot()
	out := FormatTable(snap)

	if !strings.Contains(out, "METRIC") {
		t.Errorf("missing header row: %q", out)
	}
	if !strings.Contains(out, "index.files") {
		t.Errorf("missing metric 'index.files': %q", out)
	}
	if !strings.Contains(out, "index.errors") {
		t.Errorf("missing metric 'index.errors': %q", out)
	}
	if !strings.Contains(out, "42") {
		t.Errorf("missing value 42: %q", out)
	}
}

func TestFormatTable_HistogramsOnly(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	c := r.Collector()
	c.Histogram("req.latency", 150.5)

	snap := r.Snapshot()
	out := FormatTable(snap)

	if !strings.Contains(out, "req.latency") {
		t.Errorf("missing histogram metric: %q", out)
	}
	if !strings.Contains(out, "ms") {
		t.Errorf("expected 'ms' suffix for histogram: %q", out)
	}
	if !strings.Contains(out, "p99") {
		t.Errorf("expected p99 label: %q", out)
	}
}

func TestFormatTable_MixedRecordsAndHistograms(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	c := r.Collector()
	c.Record("files.indexed", 100.0)
	c.Histogram("bfs.duration", 25.0)

	snap := r.Snapshot()
	out := FormatTable(snap)

	if !strings.Contains(out, "files.indexed") {
		t.Errorf("missing record: %q", out)
	}
	if !strings.Contains(out, "bfs.duration") {
		t.Errorf("missing histogram: %q", out)
	}
}

// FormatTable must use last-value-wins semantics for repeated metrics.
func TestFormatTable_LastValueWins(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	c := r.Collector()

	c.Record("counter", 1.0)
	c.Record("counter", 999.0) // most recent

	snap := r.Snapshot()
	out := FormatTable(snap)

	// Should contain 999, and importantly should NOT emit duplicate rows
	// (exact count is hard to assert without parsing, but we verify 999 appears)
	if !strings.Contains(out, "999") {
		t.Errorf("expected last value 999 in output: %q", out)
	}

	// Count occurrences of "counter" — should appear exactly once in the table body
	occurrences := strings.Count(out, "counter")
	if occurrences != 1 {
		t.Errorf("'counter' should appear once (last-value-wins), got %d times: %q", occurrences, out)
	}
}

// FormatTable output must be deterministic (sorted).
func TestFormatTable_DeterministicOrder(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	c := r.Collector()

	c.Record("z.metric", 1.0)
	c.Record("a.metric", 2.0)
	c.Record("m.metric", 3.0)

	snap := r.Snapshot()
	out1 := FormatTable(snap)
	out2 := FormatTable(snap)

	if out1 != out2 {
		t.Errorf("FormatTable is non-deterministic:\nfirst:  %q\nsecond: %q", out1, out2)
	}

	// Also verify alphabetical ordering: 'a' before 'm' before 'z'
	posA := strings.Index(out1, "a.metric")
	posM := strings.Index(out1, "m.metric")
	posZ := strings.Index(out1, "z.metric")
	if posA < 0 || posM < 0 || posZ < 0 {
		t.Fatal("not all metrics found in output")
	}
	if !(posA < posM && posM < posZ) {
		t.Errorf("output not sorted alphabetically: posA=%d posM=%d posZ=%d", posA, posM, posZ)
	}
}

func TestFormatTable_ZeroValueRecorded(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	c := r.Collector()
	c.Record("zero.metric", 0.0)

	snap := r.Snapshot()
	out := FormatTable(snap)
	// A zero value should still appear (it was recorded)
	if strings.Contains(out, "no stats collected yet") {
		t.Error("zero value should not be treated as 'no stats'")
	}
	if !strings.Contains(out, "zero.metric") {
		t.Errorf("expected zero.metric in output: %q", out)
	}
}

func TestFormatTable_NegativeValue(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	c := r.Collector()
	c.Record("delta", -5.5)

	snap := r.Snapshot()
	out := FormatTable(snap)
	if !strings.Contains(out, "-5.5") {
		t.Errorf("expected negative value in output: %q", out)
	}
}
