package main

import (
	"testing"

	"github.com/scrypster/huginn/internal/storage"
	"github.com/scrypster/huginn/internal/symbol"
)

func TestToSymbolEdges_MapsStorageEdgeFields(t *testing.T) {
	in := []storage.Edge{
		{
			From:       "a.go",
			To:         "b.go",
			Symbol:     "Compute",
			Confidence: "HIGH",
			Kind:       "Call",
		},
	}

	got := toSymbolEdges(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(got))
	}
	if got[0].From != "a.go" || got[0].To != "b.go" || got[0].Symbol != "Compute" {
		t.Fatalf("unexpected mapped edge: %+v", got[0])
	}
	if got[0].Confidence != symbol.ConfHigh {
		t.Fatalf("expected HIGH confidence mapping, got %q", got[0].Confidence)
	}
	if got[0].Kind != symbol.EdgeCall {
		t.Fatalf("expected Call kind mapping, got %q", got[0].Kind)
	}
}
