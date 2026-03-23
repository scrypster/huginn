package symbol

import (
	"path/filepath"
	"strings"
)

// Extractor extracts symbols and edges from a single file.
type Extractor interface {
	// Language returns the language this extractor handles (e.g. "go", "typescript").
	Language() string
	// Extract parses path/content and returns symbols and edges.
	Extract(path string, content []byte) ([]Symbol, []Edge, error)
}

// Registry maps file extensions to Extractors.
type Registry struct {
	extractors map[string]Extractor // key: lowercase extension e.g. ".go"
	fallback   Extractor            // used when no specific extractor matches
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		extractors: make(map[string]Extractor),
	}
}

// Register associates an Extractor with one or more file extensions.
// Extensions should include the dot, e.g. ".go", ".ts", ".tsx".
func (r *Registry) Register(ext Extractor, extensions ...string) {
	for _, e := range extensions {
		r.extractors[strings.ToLower(e)] = ext
	}
}

// SetFallback sets the extractor used when no specific extractor matches.
func (r *Registry) SetFallback(ext Extractor) {
	r.fallback = ext
}

// ExtractorFor returns the Extractor for the given file path, or the fallback.
// Returns nil if no extractor and no fallback is set.
func (r *Registry) ExtractorFor(path string) Extractor {
	ext := strings.ToLower(filepath.Ext(path))
	if e, ok := r.extractors[ext]; ok {
		return e
	}
	return r.fallback
}

// Extract extracts symbols and edges from path/content using the registered extractor.
// Returns empty slices (not error) if no extractor is registered for the file type.
func (r *Registry) Extract(path string, content []byte) ([]Symbol, []Edge, error) {
	ext := r.ExtractorFor(path)
	if ext == nil {
		return nil, nil, nil
	}
	return ext.Extract(path, content)
}

// Languages returns all registered language names.
func (r *Registry) Languages() []string {
	seen := make(map[string]bool)
	var langs []string
	for _, e := range r.extractors {
		lang := e.Language()
		if !seen[lang] {
			seen[lang] = true
			langs = append(langs, lang)
		}
	}
	return langs
}

// SymbolIndex is a pre-built index mapping symbol names to their edges,
// allowing O(1) impact lookups instead of O(n) linear scans per query.
type SymbolIndex map[string][]Edge

// BuildSymbolIndex constructs a SymbolIndex from a flat edge list.
// Subsequent ImpactQueryIndexed calls use this index for efficient lookups.
func BuildSymbolIndex(edges []Edge) SymbolIndex {
	idx := make(SymbolIndex, len(edges)/2+1)
	for _, e := range edges {
		idx[e.Symbol] = append(idx[e.Symbol], e)
	}
	return idx
}

// ImpactQueryIndexed is the indexed variant of ImpactQuery.
// It uses a pre-built SymbolIndex for O(1) symbol lookup rather than O(n) scan.
func ImpactQueryIndexed(symbolName string, idx SymbolIndex) ImpactReport {
	report := ImpactReport{Symbol: symbolName}
	for _, e := range idx[symbolName] {
		entry := ImpactEntry{Path: e.From, Confidence: e.Confidence}
		switch e.Confidence {
		case ConfHigh:
			report.High = append(report.High, entry)
		case ConfMedium:
			report.Medium = append(report.Medium, entry)
		default:
			report.Low = append(report.Low, entry)
		}
	}
	return report
}

// ImpactQuery finds all files referencing symbolName, grouped by confidence.
// edges is the full edge list from the store.
func ImpactQuery(symbolName string, edges []Edge) ImpactReport {
	report := ImpactReport{Symbol: symbolName}
	for _, e := range edges {
		if e.Symbol != symbolName {
			continue
		}
		entry := ImpactEntry{Path: e.From, Confidence: e.Confidence}
		switch e.Confidence {
		case ConfHigh:
			report.High = append(report.High, entry)
		case ConfMedium:
			report.Medium = append(report.Medium, entry)
		default:
			report.Low = append(report.Low, entry)
		}
	}
	return report
}
