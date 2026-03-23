package agents

import (
	"context"
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/scrypster/huginn/internal/storage"
)

// TestLoadRecentSummaries_SkipsCorruptEntry verifies that LoadRecentSummaries
// skips entries with invalid JSON (the `continue` branch).
func TestLoadRecentSummaries_SkipsCorruptEntry(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ms := NewMemoryStore(s, "test-machine")
	ctx := context.Background()

	// Store a valid summary.
	good := SessionSummary{
		SessionID: "sess-good",
		AgentName: "Bot",
		MachineID: "test-machine",
		Timestamp: time.Now(),
		Summary:   "valid summary",
	}
	if err := ms.SaveSummary(ctx, good); err != nil {
		t.Fatalf("SaveSummary: %v", err)
	}

	// Directly write a corrupt JSON value with the right key format.
	corruptKey := []byte(SummaryKey("test-machine", "Bot", "sess-corrupt"))
	if err := s.DB().Set(corruptKey, []byte("not valid json!!!"), &pebble.WriteOptions{Sync: true}); err != nil {
		t.Fatalf("Set corrupt: %v", err)
	}

	// LoadRecentSummaries should skip the corrupt entry and return only the valid one.
	results, err := ms.LoadRecentSummaries(ctx, "Bot", 10)
	if err != nil {
		t.Fatalf("LoadRecentSummaries: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result (skipping corrupt), got %d", len(results))
	}
	if len(results) > 0 && results[0].Summary != "valid summary" {
		t.Errorf("expected 'valid summary', got %q", results[0].Summary)
	}
}

// TestLoadRecentDelegations_SkipsCorruptEntry verifies that LoadRecentDelegations
// skips entries with invalid JSON.
func TestLoadRecentDelegations_SkipsCorruptEntry(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	ms := NewMemoryStore(s, "test-machine")
	ctx := context.Background()
	ts := time.Now()

	// Store a valid delegation.
	good := DelegationEntry{
		From:      "A",
		To:        "B",
		Question:  "q",
		Answer:    "a",
		Timestamp: ts,
	}
	if err := ms.AppendDelegation(ctx, good); err != nil {
		t.Fatalf("AppendDelegation: %v", err)
	}

	// Directly write a corrupt JSON value for a delegation key.
	corruptKey := []byte(DelegationKey("test-machine", "A", "B", ts.Add(time.Second)))
	if err := s.DB().Set(corruptKey, []byte("{bad json"), &pebble.WriteOptions{Sync: true}); err != nil {
		t.Fatalf("Set corrupt: %v", err)
	}

	results, err := ms.LoadRecentDelegations(ctx, "A", "B", 10)
	if err != nil {
		t.Fatalf("LoadRecentDelegations: %v", err)
	}
	// Should have 1 valid + 0 corrupt = 1 result.
	if len(results) != 1 {
		t.Errorf("expected 1 result (skipping corrupt), got %d", len(results))
	}
}
