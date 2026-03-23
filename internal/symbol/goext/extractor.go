package goext

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"

	"github.com/scrypster/huginn/internal/symbol"
)

// GoExtractor extracts symbols and edges from Go source files using go/ast.
// Confidence: HIGH for static import edges (direct imports), MEDIUM for call edges.
// Graceful degradation: if parsing fails, returns empty slices (no error).
type GoExtractor struct{}

func New() *GoExtractor { return &GoExtractor{} }

func (e *GoExtractor) Language() string { return "go" }

func (e *GoExtractor) Extract(path string, content []byte) ([]symbol.Symbol, []symbol.Edge, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, content, parser.SkipObjectResolution)
	if err != nil {
		// Graceful degradation: return empty on parse failure
		return nil, nil, nil
	}

	var symbols []symbol.Symbol
	var edges []symbol.Edge

	pkgName := f.Name.Name
	_ = pkgName

	// 1. Extract imports as HIGH-confidence edges
	for _, imp := range f.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		edges = append(edges, symbol.Edge{
			From:       path,
			To:         importPath, // package path (not file path, but still useful for graph)
			Symbol:     importPath,
			Confidence: symbol.ConfHigh,
			Kind:       symbol.EdgeImport,
		})
		// Also record as a symbol
		localName := importPath
		if imp.Name != nil {
			localName = imp.Name.Name
		} else {
			// Use last segment as local name
			parts := strings.Split(importPath, "/")
			localName = parts[len(parts)-1]
		}
		symbols = append(symbols, symbol.Symbol{
			Name:     localName,
			Kind:     symbol.KindImport,
			Path:     path,
			Line:     fset.Position(imp.Pos()).Line,
			Exported: false,
		})
	}

	// 2. Extract top-level declarations
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name := d.Name.Name
			symbols = append(symbols, symbol.Symbol{
				Name:     name,
				Kind:     symbol.KindFunction,
				Path:     path,
				Line:     fset.Position(d.Pos()).Line,
				Exported: ast.IsExported(name),
			})
			// Scan function body for calls
			if d.Body != nil {
				callEdges := extractCallEdges(path, d.Body, fset)
				edges = append(edges, callEdges...)
			}

		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					kind := symbol.KindType
					switch s.Type.(type) {
					case *ast.InterfaceType:
						kind = symbol.KindInterface
					case *ast.StructType:
						kind = symbol.KindClass
					}
					symbols = append(symbols, symbol.Symbol{
						Name:     s.Name.Name,
						Kind:     kind,
						Path:     path,
						Line:     fset.Position(s.Pos()).Line,
						Exported: ast.IsExported(s.Name.Name),
					})

				case *ast.ValueSpec:
					for _, name := range s.Names {
						symbols = append(symbols, symbol.Symbol{
							Name:     name.Name,
							Kind:     symbol.KindVariable,
							Path:     path,
							Line:     fset.Position(name.Pos()).Line,
							Exported: ast.IsExported(name.Name),
						})
					}
				}
			}
		}
	}

	return symbols, edges, nil
}

// extractCallEdges walks a function body looking for selector expressions (pkg.Func calls).
// Returns MEDIUM-confidence Call edges for each selector call found.
func extractCallEdges(path string, body *ast.BlockStmt, fset *token.FileSet) []symbol.Edge {
	var edges []symbol.Edge
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// sel.X is the receiver/package, sel.Sel is the method/function name
		if ident, ok := sel.X.(*ast.Ident); ok {
			edges = append(edges, symbol.Edge{
				From:       path,
				To:         ident.Name, // local name — will be resolved to import path by caller
				Symbol:     sel.Sel.Name,
				Confidence: symbol.ConfMedium,
				Kind:       symbol.EdgeCall,
			})
		}
		return true
	})
	return edges
}

// IsGoFile returns true if path is a Go source file (not a test file).
func IsGoFile(path string) bool {
	return filepath.Ext(path) == ".go" && !strings.HasSuffix(path, "_test.go")
}
