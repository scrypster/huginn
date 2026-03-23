package tsext

import (
	"bufio"
	"bytes"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/scrypster/huginn/internal/symbol"
)

// TSExtractor extracts symbols and edges from TypeScript/JavaScript source files
// using pure-Go regex and line-pattern heuristics. No CGO, no external parser libs.
//
// Confidence model:
// - Import edges: HIGH (static, well-defined)
// - Calls where symbol was imported: MEDIUM
// - Calls where symbol was NOT imported: LOW
// - Extends/Implements: MEDIUM (class hierarchy)
type TSExtractor struct{}

func New() *TSExtractor {
	return &TSExtractor{}
}

func (e *TSExtractor) Language() string {
	return "typescript"
}

// Extract parses TypeScript/JavaScript source and returns symbols and edges.
// Graceful degradation: if regex fails or syntax is malformed, returns partial results.
func (e *TSExtractor) Extract(path string, content []byte) ([]symbol.Symbol, []symbol.Edge, error) {
	var symbols []symbol.Symbol
	var edges []symbol.Edge

	// Track imported symbols (localName -> fromPath)
	importedSymbols := make(map[string]string)

	// Track edges to deduplicate (From|To|Symbol|Kind -> true)
	seenEdges := make(map[string]bool)

	// Track line number
	lineNum := 0

	// Scan file line by line
	scanner := bufio.NewScanner(bytes.NewReader(content))
	var inMultilineComment bool

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Handle multi-line comments
		if inMultilineComment {
			if strings.Contains(line, "*/") {
				inMultilineComment = false
			}
			continue
		}
		if strings.Contains(line, "/*") {
			inMultilineComment = true
			if strings.Contains(line, "*/") {
				inMultilineComment = false
			}
			continue
		}

		// Skip empty lines and single-line comments
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}

		// Skip export default comments and certain patterns
		if strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
			continue
		}

		// 1. Parse imports
		if matches := reImportNamed.FindStringSubmatch(line); matches != nil {
			// import { X, Y, Z } from './foo'
			fromPath := normalizePath(matches[2])
			imports := strings.Split(matches[1], ",")
			for _, imp := range imports {
				localName := strings.TrimSpace(imp)
				// Handle "import X as Y" syntax
				if strings.Contains(localName, " as ") {
					parts := strings.Split(localName, " as ")
					localName = strings.TrimSpace(parts[1])
				}
				if localName == "" {
					continue
				}

				importedSymbols[localName] = fromPath
				symbols = append(symbols, symbol.Symbol{
					Name:     localName,
					Kind:     symbol.KindImport,
					Path:     path,
					Line:     lineNum,
					Exported: false,
				})
				edgeKey := makeEdgeKey(path, fromPath, localName, symbol.EdgeImport)
				if !seenEdges[edgeKey] {
					edges = append(edges, symbol.Edge{
						From:       path,
						To:         fromPath,
						Symbol:     localName,
						Confidence: symbol.ConfHigh,
						Kind:       symbol.EdgeImport,
					})
					seenEdges[edgeKey] = true
				}
			}
			continue
		}

		if matches := reImportDefault.FindStringSubmatch(line); matches != nil {
			// import X from './foo'
			localName := strings.TrimSpace(matches[1])
			fromPath := normalizePath(matches[2])

			if localName != "" {
				importedSymbols[localName] = fromPath
				symbols = append(symbols, symbol.Symbol{
					Name:     localName,
					Kind:     symbol.KindImport,
					Path:     path,
					Line:     lineNum,
					Exported: false,
				})
				edgeKey := makeEdgeKey(path, fromPath, localName, symbol.EdgeImport)
				if !seenEdges[edgeKey] {
					edges = append(edges, symbol.Edge{
						From:       path,
						To:         fromPath,
						Symbol:     localName,
						Confidence: symbol.ConfHigh,
						Kind:       symbol.EdgeImport,
					})
					seenEdges[edgeKey] = true
				}
			}
			continue
		}

		if matches := reImportStar.FindStringSubmatch(line); matches != nil {
			// import * as X from './foo'
			localName := strings.TrimSpace(matches[1])
			fromPath := normalizePath(matches[2])

			if localName != "" {
				importedSymbols[localName] = fromPath
				symbols = append(symbols, symbol.Symbol{
					Name:     localName,
					Kind:     symbol.KindImport,
					Path:     path,
					Line:     lineNum,
					Exported: false,
				})
				edgeKey := makeEdgeKey(path, fromPath, localName, symbol.EdgeImport)
				if !seenEdges[edgeKey] {
					edges = append(edges, symbol.Edge{
						From:       path,
						To:         fromPath,
						Symbol:     localName,
						Confidence: symbol.ConfHigh,
						Kind:       symbol.EdgeImport,
					})
					seenEdges[edgeKey] = true
				}
			}
			continue
		}

		if matches := reRequire.FindStringSubmatch(line); matches != nil {
			// require('./foo')
			fromPath := normalizePath(matches[1])
			// require returns the module, so we use a generic symbol name
			edgeKey := makeEdgeKey(path, fromPath, "require", symbol.EdgeImport)
			if !seenEdges[edgeKey] {
				edges = append(edges, symbol.Edge{
					From:       path,
					To:         fromPath,
					Symbol:     "require",
					Confidence: symbol.ConfLow, // dynamic require is less certain
					Kind:       symbol.EdgeImport,
				})
				seenEdges[edgeKey] = true
			}
			continue
		}

		// 2. Parse exports
		if matches := reExportFunc.FindStringSubmatch(line); matches != nil {
			// export function foo(
			name := strings.TrimSpace(matches[1])
			if name != "" {
				symbols = append(symbols, symbol.Symbol{
					Name:     name,
					Kind:     symbol.KindFunction,
					Path:     path,
					Line:     lineNum,
					Exported: true,
				})
			}
			continue
		}

		if matches := reExportClass.FindStringSubmatch(line); matches != nil {
			// export class Foo {
			name := strings.TrimSpace(matches[1])
			if name != "" {
				symbols = append(symbols, symbol.Symbol{
					Name:     name,
					Kind:     symbol.KindClass,
					Path:     path,
					Line:     lineNum,
					Exported: true,
				})
			}
			continue
		}

		if matches := reExportConst.FindStringSubmatch(line); matches != nil {
			// export const|let|var foo =
			name := strings.TrimSpace(matches[1])
			if name != "" {
				symbols = append(symbols, symbol.Symbol{
					Name:     name,
					Kind:     symbol.KindVariable,
					Path:     path,
					Line:     lineNum,
					Exported: true,
				})
			}
			continue
		}

		if matches := reExportType.FindStringSubmatch(line); matches != nil {
			// export type Foo =
			name := strings.TrimSpace(matches[1])
			if name != "" {
				symbols = append(symbols, symbol.Symbol{
					Name:     name,
					Kind:     symbol.KindType,
					Path:     path,
					Line:     lineNum,
					Exported: true,
				})
			}
			continue
		}

		if matches := reExportIface.FindStringSubmatch(line); matches != nil {
			// export interface Foo {
			name := strings.TrimSpace(matches[1])
			if name != "" {
				symbols = append(symbols, symbol.Symbol{
					Name:     name,
					Kind:     symbol.KindInterface,
					Path:     path,
					Line:     lineNum,
					Exported: true,
				})
			}
			continue
		}

		if matches := reExportDefault.FindStringSubmatch(line); matches != nil {
			// export default function|class [name]
			// If there's a name, use it; otherwise use "default"
			name := strings.TrimSpace(matches[1])
			if name == "" {
				name = "default"
			}
			symbols = append(symbols, symbol.Symbol{
				Name:     name,
				Kind:     symbol.KindFunction,
				Path:     path,
				Line:     lineNum,
				Exported: true,
			})
			continue
		}

		// 3. Parse calls, new, extends, implements (lower confidence)
		// Look for calls like X(
		if matches := reCall.FindAllStringSubmatch(line, -1); matches != nil {
			for _, match := range matches {
				symbolName := strings.TrimSpace(match[1])
				if symbolName == "" || isKeyword(symbolName) {
					continue
				}

				// Determine confidence based on whether symbol was imported
				conf := symbol.ConfLow
				var toPath string
				if importedPath, imported := importedSymbols[symbolName]; imported {
					conf = symbol.ConfMedium
					toPath = importedPath
				} else {
					toPath = symbolName // best effort
				}

				edgeKey := makeEdgeKey(path, toPath, symbolName, symbol.EdgeCall)
				if !seenEdges[edgeKey] {
					edges = append(edges, symbol.Edge{
						From:       path,
						To:         toPath,
						Symbol:     symbolName,
						Confidence: conf,
						Kind:       symbol.EdgeCall,
					})
					seenEdges[edgeKey] = true
				}
			}
		}

		// Look for new X(
		if matches := reNew.FindAllStringSubmatch(line, -1); matches != nil {
			for _, match := range matches {
				symbolName := strings.TrimSpace(match[1])
				if symbolName == "" || isKeyword(symbolName) {
					continue
				}

				conf := symbol.ConfLow
				var toPath string
				if importedPath, imported := importedSymbols[symbolName]; imported {
					conf = symbol.ConfMedium
					toPath = importedPath
				} else {
					toPath = symbolName
				}

				edgeKey := makeEdgeKey(path, toPath, symbolName, symbol.EdgeInstantiation)
				if !seenEdges[edgeKey] {
					edges = append(edges, symbol.Edge{
						From:       path,
						To:         toPath,
						Symbol:     symbolName,
						Confidence: conf,
						Kind:       symbol.EdgeInstantiation,
					})
					seenEdges[edgeKey] = true
				}
			}
		}

		// Look for extends X
		if matches := reExtends.FindStringSubmatch(line); matches != nil {
			symbolName := strings.TrimSpace(matches[1])
			if symbolName != "" && !isKeyword(symbolName) {
				conf := symbol.ConfLow
				var toPath string
				if importedPath, imported := importedSymbols[symbolName]; imported {
					conf = symbol.ConfMedium
					toPath = importedPath
				} else {
					toPath = symbolName
				}

				edgeKey := makeEdgeKey(path, toPath, symbolName, symbol.EdgeExtends)
				if !seenEdges[edgeKey] {
					edges = append(edges, symbol.Edge{
						From:       path,
						To:         toPath,
						Symbol:     symbolName,
						Confidence: conf,
						Kind:       symbol.EdgeExtends,
					})
					seenEdges[edgeKey] = true
				}
			}
		}

		// Look for implements X, Y, Z
		if matches := reImpl.FindStringSubmatch(line); matches != nil {
			implList := matches[1]
			// Split on comma and process each interface
			interfaces := strings.Split(implList, ",")
			for _, iface := range interfaces {
				symbolName := strings.TrimSpace(iface)
				if symbolName == "" || isKeyword(symbolName) {
					continue
				}

				conf := symbol.ConfLow
				var toPath string
				if importedPath, imported := importedSymbols[symbolName]; imported {
					conf = symbol.ConfMedium
					toPath = importedPath
				} else {
					toPath = symbolName
				}

				edgeKey := makeEdgeKey(path, toPath, symbolName, symbol.EdgeImplements)
				if !seenEdges[edgeKey] {
					edges = append(edges, symbol.Edge{
						From:       path,
						To:         toPath,
						Symbol:     symbolName,
						Confidence: conf,
						Kind:       symbol.EdgeImplements,
					})
					seenEdges[edgeKey] = true
				}
			}
		}
	}

	return symbols, edges, nil
}

// Regex patterns for TypeScript/JavaScript (compiled at package level)
var (
	// Import patterns
	reImportNamed   = regexp.MustCompile(`import\s*\{([^}]+)\}\s*from\s*['"]([^'"]+)['"]`)
	reImportDefault = regexp.MustCompile(`import\s+(\w+)\s+from\s*['"]([^'"]+)['"]`)
	reImportStar    = regexp.MustCompile(`import\s*\*\s*as\s+(\w+)\s+from\s*['"]([^'"]+)['"]`)
	reRequire       = regexp.MustCompile(`require\s*\(\s*['"]([^'"]+)['"]\s*\)`)

	// Export patterns
	reExportFunc    = regexp.MustCompile(`export\s+(?:async\s+)?function\s+(\w+)`)
	reExportClass   = regexp.MustCompile(`export\s+class\s+(\w+)`)
	reExportConst   = regexp.MustCompile(`export\s+(?:const|let|var)\s+(\w+)`)
	reExportType    = regexp.MustCompile(`export\s+type\s+(\w+)`)
	reExportIface   = regexp.MustCompile(`export\s+interface\s+(\w+)`)
	reExportDefault = regexp.MustCompile(`export\s+default\s+(?:function|class)?\s*(\w*)`)

	// Call and inheritance patterns
	reCall    = regexp.MustCompile(`\b(\w+)\s*\(`)
	reNew     = regexp.MustCompile(`new\s+(\w+)\s*[(<]`)
	reExtends = regexp.MustCompile(`extends\s+(\w+)`)
	reImpl     = regexp.MustCompile(`implements\s+([\w,\s]+)`)
)

// normalizePath converts relative import paths to a canonical form.
// E.g., './foo' -> 'foo', '../bar' -> 'bar', 'pkg/mod' stays as is.
func normalizePath(p string) string {
	// Remove leading ./ or ../
	p = strings.TrimPrefix(p, "./")
	for strings.HasPrefix(p, "../") {
		p = strings.TrimPrefix(p, "../")
	}
	// Remove trailing /index or .js/.ts/.tsx/.jsx
	p = strings.TrimSuffix(p, "/index")
	p = strings.TrimSuffix(p, ".js")
	p = strings.TrimSuffix(p, ".ts")
	p = strings.TrimSuffix(p, ".tsx")
	p = strings.TrimSuffix(p, ".jsx")
	return p
}

// makeEdgeKey creates a dedupe key for edges.
func makeEdgeKey(from, to, symbol string, kind symbol.EdgeKind) string {
	return from + "|" + to + "|" + symbol + "|" + string(kind)
}

// isKeyword checks if a name is a JavaScript/TypeScript keyword.
// Prevents false positives in call detection.
func isKeyword(name string) bool {
	keywords := map[string]bool{
		"if":       true,
		"for":      true,
		"while":    true,
		"switch":   true,
		"case":     true,
		"break":    true,
		"continue": true,
		"return":   true,
		"function": true,
		"class":    true,
		"try":      true,
		"catch":    true,
		"finally":  true,
		"throw":    true,
		"const":    true,
		"let":      true,
		"var":      true,
		"async":    true,
		"await":    true,
		"yield":    true,
		"new":      true,
		"this":     true,
		"super":    true,
		"delete":   true,
		"typeof":   true,
		"instanceof": true,
		"in":       true,
		"of":       true,
		"get":      true,
		"set":      true,
		"static":   true,
		"extends":  true,
		"implements": true,
		"interface": true,
		"enum":     true,
		"export":   true,
		"import":   true,
		"from":     true,
		"as":       true,
		"default":  true,
		"public":   true,
		"private":  true,
		"protected": true,
		"readonly": true,
		"abstract": true,
	}
	return keywords[name]
}

// IsTSFile returns true if path is a TypeScript/JavaScript source file.
func IsTSFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".ts" || ext == ".tsx" || ext == ".js" || ext == ".jsx"
}
