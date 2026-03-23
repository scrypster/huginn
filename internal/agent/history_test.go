package agent

import (
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
)

// TestOrchestrator_ImportExportHistory verifies that ImportHistory followed by
// ExportHistory returns an identical slice of messages.
func TestOrchestrator_ImportExportHistory(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)

	msgs := []backend.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}

	o.ImportHistory(msgs)

	got := o.ExportHistory()
	if len(got) != len(msgs) {
		t.Fatalf("expected %d messages, got %d", len(msgs), len(got))
	}
	for i, m := range msgs {
		if got[i].Role != m.Role || got[i].Content != m.Content {
			t.Errorf("message[%d]: expected {role=%q content=%q}, got {role=%q content=%q}",
				i, m.Role, m.Content, got[i].Role, got[i].Content)
		}
	}
}

// TestOrchestrator_ExportHistory_ReturnsCopy verifies that mutating the returned
// slice does not affect the orchestrator's internal history.
func TestOrchestrator_ExportHistory_ReturnsCopy(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)

	msgs := []backend.Message{
		{Role: "user", Content: "hello"},
	}
	o.ImportHistory(msgs)

	exported := o.ExportHistory()
	// Mutate the exported copy.
	exported[0].Content = "mutated"

	// Internal history should be unaffected.
	internal := o.ExportHistory()
	if internal[0].Content != "hello" {
		t.Errorf("expected internal history to be unchanged, got %q", internal[0].Content)
	}
}

// TestOrchestrator_ImportHistory_Empty verifies that importing an empty slice
// clears the history.
func TestOrchestrator_ImportHistory_Empty(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)

	// First populate some history.
	o.ImportHistory([]backend.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	})

	// Now clear it.
	o.ImportHistory([]backend.Message{})

	got := o.ExportHistory()
	if len(got) != 0 {
		t.Errorf("expected empty history after importing empty slice, got %d messages", len(got))
	}
}
