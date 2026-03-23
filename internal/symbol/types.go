package symbol

// SymbolKind classifies the type of a symbol.
type SymbolKind string

const (
	KindFunction      SymbolKind = "function"
	KindClass         SymbolKind = "class"
	KindInterface     SymbolKind = "interface"
	KindType          SymbolKind = "type"
	KindVariable      SymbolKind = "variable"
	KindImport        SymbolKind = "import"
	KindExport        SymbolKind = "export"
)

// Confidence indicates how certain we are about an edge relationship.
type Confidence string

const (
	ConfHigh   Confidence = "HIGH"
	ConfMedium Confidence = "MEDIUM"
	ConfLow    Confidence = "LOW"
)

// EdgeKind classifies the relationship type between files.
type EdgeKind string

const (
	EdgeImport        EdgeKind = "Import"
	EdgeCall          EdgeKind = "Call"
	EdgeInstantiation EdgeKind = "Instantiation"
	EdgeExtends       EdgeKind = "Extends"
	EdgeImplements    EdgeKind = "Implements"
)

// Symbol represents a code symbol extracted from a file.
type Symbol struct {
	Name     string     `json:"name"`
	Kind     SymbolKind `json:"kind"`
	Path     string     `json:"path"`
	Line     int        `json:"line"`
	Exported bool       `json:"exported"`
}

// Edge represents a relationship between two files in the call/import graph.
type Edge struct {
	From       string     `json:"from"`
	To         string     `json:"to"`
	Symbol     string     `json:"symbol"`
	Confidence Confidence `json:"confidence"`
	Kind       EdgeKind   `json:"kind"`
}

// ImpactEntry is one file in the /impact command output.
type ImpactEntry struct {
	Path       string     `json:"path"`
	Line       int        `json:"line,omitempty"`
	Confidence Confidence `json:"confidence"`
}

// ImpactReport is the result of /impact <symbol>.
type ImpactReport struct {
	Symbol string                       `json:"symbol"`
	High   []ImpactEntry                `json:"high"`
	Medium []ImpactEntry                `json:"medium"`
	Low    []ImpactEntry                `json:"low"`
}
