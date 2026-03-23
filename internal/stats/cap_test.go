package stats

import (
	"testing"
)

func TestRegistry_MaxEntriesCap_Records(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	c := r.Collector()

	// Insert more than maxEntries records.
	for i := 0; i < maxEntries+500; i++ {
		c.Record("counter", float64(i))
	}

	snap := r.Snapshot()
	if len(snap.Records) > maxEntries {
		t.Errorf("expected records capped at %d, got %d", maxEntries, len(snap.Records))
	}
	// The most recent entry should be preserved.
	last := snap.Records[len(snap.Records)-1]
	if last.Value != float64(maxEntries+499) {
		t.Errorf("expected last value %d, got %v", maxEntries+499, last.Value)
	}
}

func TestRegistry_MaxEntriesCap_Histograms(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	c := r.Collector()

	for i := 0; i < maxEntries+500; i++ {
		c.Histogram("latency", float64(i))
	}

	snap := r.Snapshot()
	if len(snap.Histograms) > maxEntries {
		t.Errorf("expected histograms capped at %d, got %d", maxEntries, len(snap.Histograms))
	}
	last := snap.Histograms[len(snap.Histograms)-1]
	if last.Value != float64(maxEntries+499) {
		t.Errorf("expected last value %d, got %v", maxEntries+499, last.Value)
	}
}
