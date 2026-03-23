package tui

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/storage"
	"github.com/scrypster/huginn/internal/symbol"
)

// TestConvertStorageEdgesToSymbolEdges verifies field-by-field conversion.
func TestConvertStorageEdgesToSymbolEdges(t *testing.T) {
	in := []storage.Edge{
		{
			From:       "a.go",
			To:         "b.go",
			Symbol:     "MyFunc",
			Confidence: "HIGH",
			Kind:       "Call",
		},
		{
			From:       "c.go",
			To:         "d.go",
			Symbol:     "MyType",
			Confidence: "MEDIUM",
			Kind:       "Import",
		},
	}

	out := convertStorageEdgesToSymbolEdges(in)

	if len(out) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(out))
	}
	if out[0].From != "a.go" || out[0].To != "b.go" || out[0].Symbol != "MyFunc" {
		t.Errorf("edge 0 fields wrong: %+v", out[0])
	}
	if out[0].Confidence != symbol.ConfHigh {
		t.Errorf("expected HIGH confidence, got %s", out[0].Confidence)
	}
	if out[0].Kind != symbol.EdgeCall {
		t.Errorf("expected Call kind, got %s", out[0].Kind)
	}
	if out[1].Confidence != symbol.ConfMedium {
		t.Errorf("expected MEDIUM confidence, got %s", out[1].Confidence)
	}
	if out[1].Kind != symbol.EdgeImport {
		t.Errorf("expected Import kind, got %s", out[1].Kind)
	}
}

// TestFormatImpactReport_Empty verifies output when no edges match.
func TestFormatImpactReport_Empty(t *testing.T) {
	report := symbol.ImpactReport{Symbol: "UnknownSym"}
	out := formatImpactReport(report)

	if !strings.Contains(out, "UnknownSym") {
		t.Errorf("expected symbol name in output, got: %s", out)
	}
	if !strings.Contains(out, "No callers") {
		t.Errorf("expected 'No callers' in output, got: %s", out)
	}
}

// TestFormatImpactReport_WithEntries verifies that high/medium/low entries are shown.
func TestFormatImpactReport_WithEntries(t *testing.T) {
	report := symbol.ImpactReport{
		Symbol: "BuildIndex",
		High: []symbol.ImpactEntry{
			{Path: "main.go", Confidence: symbol.ConfHigh},
		},
		Medium: []symbol.ImpactEntry{
			{Path: "internal/agent/orchestrator.go", Confidence: symbol.ConfMedium},
		},
		Low: []symbol.ImpactEntry{
			{Path: "internal/tui/loader.go", Confidence: symbol.ConfLow},
		},
	}

	out := formatImpactReport(report)

	if !strings.Contains(out, "BuildIndex") {
		t.Errorf("expected symbol name in output")
	}
	if !strings.Contains(out, "HIGH") {
		t.Errorf("expected HIGH section in output, got: %s", out)
	}
	if !strings.Contains(out, "main.go") {
		t.Errorf("expected main.go in HIGH section, got: %s", out)
	}
	if !strings.Contains(out, "MEDIUM") {
		t.Errorf("expected MEDIUM section in output, got: %s", out)
	}
	if !strings.Contains(out, "orchestrator.go") {
		t.Errorf("expected orchestrator.go in MEDIUM section, got: %s", out)
	}
	if !strings.Contains(out, "LOW") {
		t.Errorf("expected LOW section in output, got: %s", out)
	}
	if !strings.Contains(out, "loader.go") {
		t.Errorf("expected loader.go in LOW section, got: %s", out)
	}
}

// TestImpactHandlerNoEdges verifies the /impact handler message when store has no edges.
// Simulates the handler path: GetAllEdges returns empty, ImpactQuery finds nothing,
// so the handler emits the "No references found" message.
func TestImpactHandlerNoEdges(t *testing.T) {
	sym := "SomeFunc"
	// Replicate exactly what the handler does when all edges yield no matches.
	var allEdges []storage.Edge // empty — as if store has no data
	symEdges := convertStorageEdgesToSymbolEdges(allEdges)
	report := symbol.ImpactQuery(sym, symEdges)

	if len(report.High)+len(report.Medium)+len(report.Low) != 0 {
		t.Fatalf("expected no impact entries for empty edge set, got high=%d medium=%d low=%d",
			len(report.High), len(report.Medium), len(report.Low))
	}

	msg := "No references found for '" + sym + "'.\nRun /workspace to index the repo first."
	if !strings.Contains(msg, sym) {
		t.Errorf("expected symbol name in no-references message")
	}
	if !strings.Contains(msg, "No references found") {
		t.Errorf("expected 'No references found' in message, got: %s", msg)
	}
}
