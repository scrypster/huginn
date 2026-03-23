package symbol

import (
	"testing"
)

func TestBuildSymbolIndex_Empty(t *testing.T) {
	idx := BuildSymbolIndex(nil)
	if len(idx) != 0 {
		t.Errorf("expected empty index, got %d entries", len(idx))
	}
}

func TestBuildSymbolIndex_GroupsBySymbol(t *testing.T) {
	edges := []Edge{
		{From: "a.go", To: "b.go", Symbol: "Foo", Confidence: ConfHigh, Kind: EdgeCall},
		{From: "c.go", To: "b.go", Symbol: "Foo", Confidence: ConfMedium, Kind: EdgeCall},
		{From: "d.go", To: "e.go", Symbol: "Bar", Confidence: ConfLow, Kind: EdgeImport},
	}
	idx := BuildSymbolIndex(edges)

	if len(idx["Foo"]) != 2 {
		t.Errorf("expected 2 edges for Foo, got %d", len(idx["Foo"]))
	}
	if len(idx["Bar"]) != 1 {
		t.Errorf("expected 1 edge for Bar, got %d", len(idx["Bar"]))
	}
	if len(idx["Missing"]) != 0 {
		t.Errorf("expected 0 edges for Missing, got %d", len(idx["Missing"]))
	}
}

func TestBuildSymbolIndex_O1Lookup(t *testing.T) {
	// Build a large edge set and verify O(1) lookup
	edges := make([]Edge, 10000)
	for i := range edges {
		edges[i] = Edge{
			From:       "caller.go",
			To:         "target.go",
			Symbol:     "Sym" + string(rune('A'+i%26)),
			Confidence: ConfHigh,
			Kind:       EdgeCall,
		}
	}
	idx := BuildSymbolIndex(edges)

	// Lookup a specific symbol — this is O(1) map access
	got := idx["SymA"]
	if len(got) == 0 {
		t.Fatal("expected edges for SymA")
	}
	for _, e := range got {
		if e.Symbol != "SymA" {
			t.Errorf("expected Symbol=SymA, got %q", e.Symbol)
		}
	}
}

func TestImpactQueryIndexed_MatchesImpactQuery(t *testing.T) {
	edges := []Edge{
		{From: "a.go", Symbol: "MyFunc", Confidence: ConfHigh},
		{From: "b.go", Symbol: "MyFunc", Confidence: ConfMedium},
		{From: "c.go", Symbol: "MyFunc", Confidence: ConfLow},
		{From: "d.go", Symbol: "MyFunc", Confidence: ConfHigh},
		{From: "e.go", Symbol: "OtherFunc", Confidence: ConfHigh},
	}

	// Both methods should produce the same result
	linear := ImpactQuery("MyFunc", edges)
	idx := BuildSymbolIndex(edges)
	indexed := ImpactQueryIndexed("MyFunc", idx)

	if len(linear.High) != len(indexed.High) {
		t.Errorf("High: linear=%d, indexed=%d", len(linear.High), len(indexed.High))
	}
	if len(linear.Medium) != len(indexed.Medium) {
		t.Errorf("Medium: linear=%d, indexed=%d", len(linear.Medium), len(indexed.Medium))
	}
	if len(linear.Low) != len(indexed.Low) {
		t.Errorf("Low: linear=%d, indexed=%d", len(linear.Low), len(indexed.Low))
	}
}

func TestImpactQueryIndexed_NoMatchReturnsEmpty(t *testing.T) {
	edges := []Edge{
		{From: "a.go", Symbol: "Foo", Confidence: ConfHigh},
	}
	idx := BuildSymbolIndex(edges)
	report := ImpactQueryIndexed("NotFound", idx)
	if len(report.High)+len(report.Medium)+len(report.Low) != 0 {
		t.Error("expected empty report for missing symbol")
	}
}

func TestImpactQueryIndexed_TieredConfidence(t *testing.T) {
	edges := []Edge{
		{From: "h.go", Symbol: "Target", Confidence: ConfHigh},
		{From: "m.go", Symbol: "Target", Confidence: ConfMedium},
		{From: "l.go", Symbol: "Target", Confidence: ConfLow},
		{From: "u.go", Symbol: "Target", Confidence: "UNKNOWN"},
	}
	idx := BuildSymbolIndex(edges)
	report := ImpactQueryIndexed("Target", idx)

	if len(report.High) != 1 {
		t.Errorf("expected 1 high, got %d", len(report.High))
	}
	if len(report.Medium) != 1 {
		t.Errorf("expected 1 medium, got %d", len(report.Medium))
	}
	// ConfLow + UNKNOWN → 2 entries in Low
	if len(report.Low) != 2 {
		t.Errorf("expected 2 low, got %d", len(report.Low))
	}
}
