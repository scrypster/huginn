package heuristic

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"

	"github.com/scrypster/huginn/internal/symbol"
)

// HeuristicExtractor extracts symbols using simple line-pattern matching.
// Used as fallback for languages without a dedicated extractor.
// All call edges get LOW confidence. Import edges get MEDIUM confidence.
type HeuristicExtractor struct{}

func New() *HeuristicExtractor { return &HeuristicExtractor{} }

func (e *HeuristicExtractor) Language() string { return "heuristic" }

// Patterns (compiled once at package level)
var (
	// Python
	rePyImport = regexp.MustCompile(`^(?:from\s+(\S+)\s+import|import\s+(\S+))`)
	rePyDef    = regexp.MustCompile(`^def\s+(\w+)\s*\(`)
	rePyClass  = regexp.MustCompile(`^class\s+(\w+)`)

	// Ruby
	reRbRequire = regexp.MustCompile(`require(?:_relative)?\s+['"]([^'"]+)['"]`)
	reRbDef     = regexp.MustCompile(`^\s*def\s+(\w+)`)
	reRbClass   = regexp.MustCompile(`^\s*class\s+(\w+)`)
	reRbModule  = regexp.MustCompile(`^\s*module\s+(\w+)`)

	// Rust
	reRsUse    = regexp.MustCompile(`^use\s+([\w:]+)`)
	reRsFn     = regexp.MustCompile(`^\s*(?:pub\s+)?fn\s+(\w+)`)
	reRsStruct = regexp.MustCompile(`^\s*(?:pub\s+)?struct\s+(\w+)`)
	reRsTrait  = regexp.MustCompile(`^\s*(?:pub\s+)?trait\s+(\w+)`)
	reRsImpl    = regexp.MustCompile(`^\s*impl(?:<[^>]+>)?\s+(\w+)`)

	// Generic function/class patterns (last resort)
	reGenFunc  = regexp.MustCompile(`(?:function|func|def|fn|sub|procedure)\s+(\w+)`)
	reGenClass = regexp.MustCompile(`(?:class|struct|type|interface|trait)\s+(\w+)`)
)

func (e *HeuristicExtractor) Extract(path string, content []byte) ([]symbol.Symbol, []symbol.Edge, error) {
	var symbols []symbol.Symbol
	var edges []symbol.Edge

	// Recover from any panic in regex matching
	defer func() {
		if r := recover(); r != nil {
			symbols = nil
			edges = nil
		}
	}()

	scanner := bufio.NewScanner(bytes.NewReader(content))
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Skip comments and blank lines
		if trimmed == "" || strings.HasPrefix(trimmed, "#") ||
			strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") ||
			strings.HasPrefix(trimmed, "--") {
			continue
		}

		// Python imports
		if m := rePyImport.FindStringSubmatch(trimmed); m != nil {
			mod := m[1]
			if mod == "" {
				mod = m[2]
			}
			if mod != "" {
				edges = append(edges, symbol.Edge{
					From: path, To: mod, Symbol: mod,
					Confidence: symbol.ConfMedium, Kind: symbol.EdgeImport,
				})
			}
		}

		// Python def
		if m := rePyDef.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, symbol.Symbol{
				Name: m[1], Kind: symbol.KindFunction, Path: path, Line: lineNum,
				Exported: !strings.HasPrefix(m[1], "_"),
			})
		}

		// Python class
		if m := rePyClass.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, symbol.Symbol{
				Name: m[1], Kind: symbol.KindClass, Path: path, Line: lineNum,
				Exported: !strings.HasPrefix(m[1], "_"),
			})
		}

		// Ruby require
		if m := reRbRequire.FindStringSubmatch(trimmed); m != nil {
			edges = append(edges, symbol.Edge{
				From: path, To: m[1], Symbol: m[1],
				Confidence: symbol.ConfMedium, Kind: symbol.EdgeImport,
			})
		}

		// Ruby def/class/module
		if m := reRbDef.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, symbol.Symbol{
				Name: m[1], Kind: symbol.KindFunction, Path: path, Line: lineNum,
				Exported: !strings.HasPrefix(m[1], "_"),
			})
		}
		if m := reRbClass.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, symbol.Symbol{
				Name: m[1], Kind: symbol.KindClass, Path: path, Line: lineNum, Exported: true,
			})
		}
		if m := reRbModule.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, symbol.Symbol{
				Name: m[1], Kind: symbol.KindType, Path: path, Line: lineNum, Exported: true,
			})
		}

		// Rust use
		if m := reRsUse.FindStringSubmatch(trimmed); m != nil {
			edges = append(edges, symbol.Edge{
				From: path, To: m[1], Symbol: m[1],
				Confidence: symbol.ConfMedium, Kind: symbol.EdgeImport,
			})
		}

		// Rust fn/struct/trait/impl
		if m := reRsFn.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, symbol.Symbol{
				Name: m[1], Kind: symbol.KindFunction, Path: path, Line: lineNum,
				Exported: strings.Contains(line, "pub "),
			})
		}
		if m := reRsStruct.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, symbol.Symbol{
				Name: m[1], Kind: symbol.KindClass, Path: path, Line: lineNum,
				Exported: strings.Contains(line, "pub "),
			})
		}
		if m := reRsTrait.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, symbol.Symbol{
				Name: m[1], Kind: symbol.KindInterface, Path: path, Line: lineNum,
				Exported: strings.Contains(line, "pub "),
			})
		}
		if m := reRsImpl.FindStringSubmatch(trimmed); m != nil {
			symbols = append(symbols, symbol.Symbol{
				Name: m[1], Kind: symbol.KindType, Path: path, Line: lineNum, Exported: false,
			})
		}

		// Generic fallback (only if no language-specific match above triggered)
		if len(symbols) == 0 && len(edges) == 0 {
			if m := reGenFunc.FindStringSubmatch(trimmed); m != nil {
				symbols = append(symbols, symbol.Symbol{
					Name: m[1], Kind: symbol.KindFunction, Path: path, Line: lineNum, Exported: true,
				})
			}
			if m := reGenClass.FindStringSubmatch(trimmed); m != nil {
				symbols = append(symbols, symbol.Symbol{
					Name: m[1], Kind: symbol.KindClass, Path: path, Line: lineNum, Exported: true,
				})
			}
		}
	}

	return symbols, edges, nil
}
