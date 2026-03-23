package symbol

import (
	"sort"
	"testing"
)

// ---------------------------------------------------------------------------
// Types / constants
// ---------------------------------------------------------------------------

func TestSymbolKindConstants(t *testing.T) {
	kinds := []SymbolKind{
		KindFunction, KindClass, KindInterface, KindType,
		KindVariable, KindImport, KindExport,
	}
	// All values should be non-empty strings and distinct.
	seen := make(map[SymbolKind]bool)
	for _, k := range kinds {
		if string(k) == "" {
			t.Errorf("SymbolKind constant is empty")
		}
		if seen[k] {
			t.Errorf("duplicate SymbolKind value: %q", k)
		}
		seen[k] = true
	}
}

func TestConfidenceConstants(t *testing.T) {
	confs := []Confidence{ConfHigh, ConfMedium, ConfLow}
	seen := make(map[Confidence]bool)
	for _, c := range confs {
		if string(c) == "" {
			t.Errorf("Confidence constant is empty")
		}
		if seen[c] {
			t.Errorf("duplicate Confidence value: %q", c)
		}
		seen[c] = true
	}
}

func TestEdgeKindConstants(t *testing.T) {
	kinds := []EdgeKind{
		EdgeImport, EdgeCall, EdgeInstantiation, EdgeExtends, EdgeImplements,
	}
	seen := make(map[EdgeKind]bool)
	for _, k := range kinds {
		if string(k) == "" {
			t.Errorf("EdgeKind constant is empty")
		}
		if seen[k] {
			t.Errorf("duplicate EdgeKind value: %q", k)
		}
		seen[k] = true
	}
}

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

type mockExtractor struct {
	lang string
}

func (m *mockExtractor) Language() string { return m.lang }
func (m *mockExtractor) Extract(path string, content []byte) ([]Symbol, []Edge, error) {
	return []Symbol{{Name: "TestSym", Kind: KindFunction, Path: path}}, nil, nil
}

func TestNewRegistry_Empty(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if r.ExtractorFor("foo.go") != nil {
		t.Error("expected nil extractor for unregistered extension")
	}
}

func TestRegistry_RegisterAndExtractorFor(t *testing.T) {
	r := NewRegistry()
	ext := &mockExtractor{lang: "go"}
	r.Register(ext, ".go")

	got := r.ExtractorFor("main.go")
	if got == nil {
		t.Fatal("expected extractor for .go, got nil")
	}
	if got.Language() != "go" {
		t.Errorf("expected language 'go', got %q", got.Language())
	}
}

func TestRegistry_RegisterMultipleExtensions(t *testing.T) {
	r := NewRegistry()
	ext := &mockExtractor{lang: "typescript"}
	r.Register(ext, ".ts", ".tsx", ".js", ".jsx")

	for _, path := range []string{"app.ts", "component.tsx", "util.js", "page.jsx"} {
		if r.ExtractorFor(path) == nil {
			t.Errorf("expected extractor for %s, got nil", path)
		}
	}
}

func TestRegistry_ExtractorForCaseInsensitive(t *testing.T) {
	r := NewRegistry()
	ext := &mockExtractor{lang: "go"}
	r.Register(ext, ".go")

	// Extensions should be matched case-insensitively.
	if r.ExtractorFor("MAIN.GO") == nil {
		t.Error("expected extractor for .GO (uppercase)")
	}
	if r.ExtractorFor("Main.Go") == nil {
		t.Error("expected extractor for .Go (mixed case)")
	}
}

func TestRegistry_FallbackUsedWhenNoMatch(t *testing.T) {
	r := NewRegistry()
	fallback := &mockExtractor{lang: "heuristic"}
	r.SetFallback(fallback)

	got := r.ExtractorFor("script.rb")
	if got == nil {
		t.Fatal("expected fallback extractor, got nil")
	}
	if got.Language() != "heuristic" {
		t.Errorf("expected 'heuristic', got %q", got.Language())
	}
}

func TestRegistry_SpecificExtractorPreferredOverFallback(t *testing.T) {
	r := NewRegistry()
	specific := &mockExtractor{lang: "go"}
	fallback := &mockExtractor{lang: "heuristic"}
	r.Register(specific, ".go")
	r.SetFallback(fallback)

	got := r.ExtractorFor("main.go")
	if got.Language() != "go" {
		t.Errorf("expected specific extractor 'go', got %q", got.Language())
	}
}

func TestRegistry_NoExtractorNoFallback_ReturnsNil(t *testing.T) {
	r := NewRegistry()
	if r.ExtractorFor("unknown.xyz") != nil {
		t.Error("expected nil for unknown extension with no fallback")
	}
}

func TestRegistry_Extract_NoExtractor_ReturnsEmpty(t *testing.T) {
	r := NewRegistry()
	syms, edges, err := r.Extract("unknown.xyz", []byte("hello"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if syms != nil || edges != nil {
		t.Error("expected nil slices when no extractor registered")
	}
}

func TestRegistry_Extract_WithExtractor(t *testing.T) {
	r := NewRegistry()
	ext := &mockExtractor{lang: "go"}
	r.Register(ext, ".go")

	syms, edges, err := r.Extract("main.go", []byte("package main"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(syms))
	}
	if syms[0].Name != "TestSym" {
		t.Errorf("expected symbol name 'TestSym', got %q", syms[0].Name)
	}
	if edges != nil {
		t.Error("expected nil edges from mock extractor")
	}
}

func TestRegistry_Languages_Empty(t *testing.T) {
	r := NewRegistry()
	langs := r.Languages()
	if len(langs) != 0 {
		t.Errorf("expected 0 languages, got %d", len(langs))
	}
}

func TestRegistry_Languages_Unique(t *testing.T) {
	r := NewRegistry()
	goExt := &mockExtractor{lang: "go"}
	tsExt := &mockExtractor{lang: "typescript"}

	// Register two extensions for the same language.
	r.Register(goExt, ".go")
	r.Register(tsExt, ".ts", ".tsx")

	langs := r.Languages()
	sort.Strings(langs)

	// Should have exactly "go" and "typescript" — no duplicates.
	if len(langs) != 2 {
		t.Errorf("expected 2 unique languages, got %d: %v", len(langs), langs)
	}
	if langs[0] != "go" || langs[1] != "typescript" {
		t.Errorf("unexpected languages: %v", langs)
	}
}

func TestRegistry_Languages_DeduplicatesSharedLanguage(t *testing.T) {
	r := NewRegistry()
	ext := &mockExtractor{lang: "go"}
	// Same language registered under two extensions.
	r.Register(ext, ".go", ".gox")

	langs := r.Languages()
	if len(langs) != 1 {
		t.Errorf("expected 1 language (deduped), got %d: %v", len(langs), langs)
	}
}

// ---------------------------------------------------------------------------
// ImpactQuery
// ---------------------------------------------------------------------------

func TestImpactQuery_EmptyEdges(t *testing.T) {
	report := ImpactQuery("MyFunc", nil)
	if report.Symbol != "MyFunc" {
		t.Errorf("expected symbol 'MyFunc', got %q", report.Symbol)
	}
	if len(report.High)+len(report.Medium)+len(report.Low) != 0 {
		t.Error("expected empty report for nil edges")
	}
}

func TestImpactQuery_NoMatchingSymbol(t *testing.T) {
	edges := []Edge{
		{From: "a.go", To: "b.go", Symbol: "OtherFunc", Confidence: ConfHigh, Kind: EdgeCall},
	}
	report := ImpactQuery("MyFunc", edges)
	if len(report.High)+len(report.Medium)+len(report.Low) != 0 {
		t.Error("expected empty report when no edges match symbol")
	}
}

func TestImpactQuery_HighConfidence(t *testing.T) {
	edges := []Edge{
		{From: "caller.go", To: "callee.go", Symbol: "Foo", Confidence: ConfHigh, Kind: EdgeCall},
	}
	report := ImpactQuery("Foo", edges)
	if len(report.High) != 1 {
		t.Fatalf("expected 1 high-confidence entry, got %d", len(report.High))
	}
	if report.High[0].Path != "caller.go" {
		t.Errorf("expected path 'caller.go', got %q", report.High[0].Path)
	}
	if report.High[0].Confidence != ConfHigh {
		t.Errorf("expected ConfHigh, got %q", report.High[0].Confidence)
	}
}

func TestImpactQuery_MediumConfidence(t *testing.T) {
	edges := []Edge{
		{From: "caller.go", Symbol: "Bar", Confidence: ConfMedium, Kind: EdgeCall},
	}
	report := ImpactQuery("Bar", edges)
	if len(report.Medium) != 1 {
		t.Fatalf("expected 1 medium-confidence entry, got %d", len(report.Medium))
	}
	if len(report.High) != 0 || len(report.Low) != 0 {
		t.Error("expected no high or low entries")
	}
}

func TestImpactQuery_LowConfidence(t *testing.T) {
	edges := []Edge{
		{From: "caller.go", Symbol: "Baz", Confidence: ConfLow, Kind: EdgeCall},
	}
	report := ImpactQuery("Baz", edges)
	if len(report.Low) != 1 {
		t.Fatalf("expected 1 low-confidence entry, got %d", len(report.Low))
	}
}

func TestImpactQuery_UnknownConfidenceGoesToLow(t *testing.T) {
	edges := []Edge{
		{From: "caller.go", Symbol: "Mystery", Confidence: "UNKNOWN", Kind: EdgeCall},
	}
	report := ImpactQuery("Mystery", edges)
	// The switch default falls through to Low.
	if len(report.Low) != 1 {
		t.Fatalf("expected unknown confidence to land in low bucket, got %d", len(report.Low))
	}
}

func TestImpactQuery_MixedConfidences(t *testing.T) {
	edges := []Edge{
		{From: "a.go", Symbol: "MyFunc", Confidence: ConfHigh},
		{From: "b.go", Symbol: "MyFunc", Confidence: ConfMedium},
		{From: "c.go", Symbol: "MyFunc", Confidence: ConfLow},
		{From: "d.go", Symbol: "MyFunc", Confidence: ConfHigh},
		{From: "e.go", Symbol: "OtherFunc", Confidence: ConfHigh}, // different symbol
	}
	report := ImpactQuery("MyFunc", edges)
	if len(report.High) != 2 {
		t.Errorf("expected 2 high entries, got %d", len(report.High))
	}
	if len(report.Medium) != 1 {
		t.Errorf("expected 1 medium entry, got %d", len(report.Medium))
	}
	if len(report.Low) != 1 {
		t.Errorf("expected 1 low entry, got %d", len(report.Low))
	}
}

func TestImpactQuery_PreservesPath(t *testing.T) {
	edges := []Edge{
		{From: "internal/foo/bar.go", Symbol: "DoThing", Confidence: ConfHigh},
	}
	report := ImpactQuery("DoThing", edges)
	if len(report.High) == 0 {
		t.Fatal("expected high entry")
	}
	if report.High[0].Path != "internal/foo/bar.go" {
		t.Errorf("expected full path preserved, got %q", report.High[0].Path)
	}
}

// ---------------------------------------------------------------------------
// Struct field checks (zero-value safety)
// ---------------------------------------------------------------------------

func TestSymbol_ZeroValue(t *testing.T) {
	var s Symbol
	// Zero-value Symbol should not panic and fields should be empty.
	_ = s.Name
	_ = s.Kind
	_ = s.Path
	_ = s.Line
	_ = s.Exported
}

func TestEdge_ZeroValue(t *testing.T) {
	var e Edge
	_ = e.From
	_ = e.To
	_ = e.Symbol
	_ = e.Confidence
	_ = e.Kind
}

func TestImpactReport_ZeroValue(t *testing.T) {
	var r ImpactReport
	if r.Symbol != "" {
		t.Error("expected empty symbol")
	}
	if r.High != nil || r.Medium != nil || r.Low != nil {
		t.Error("expected nil slices")
	}
}
