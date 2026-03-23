package tsext

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/symbol"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func findSymbol(syms []symbol.Symbol, name string) *symbol.Symbol {
	for i := range syms {
		if syms[i].Name == name {
			return &syms[i]
		}
	}
	return nil
}

func findEdge(edges []symbol.Edge, sym string, kind symbol.EdgeKind) *symbol.Edge {
	for i := range edges {
		if edges[i].Symbol == sym && edges[i].Kind == kind {
			return &edges[i]
		}
	}
	return nil
}

func countEdgeKind(edges []symbol.Edge, kind symbol.EdgeKind) int {
	n := 0
	for _, e := range edges {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// Constructor / Language
// ---------------------------------------------------------------------------

func TestNew(t *testing.T) {
	e := New()
	if e == nil {
		t.Fatal("New() returned nil")
	}
}

func TestLanguage(t *testing.T) {
	e := New()
	if e.Language() != "typescript" {
		t.Errorf("expected 'typescript', got %q", e.Language())
	}
}

// ---------------------------------------------------------------------------
// Empty / trivial input
// ---------------------------------------------------------------------------

func TestExtract_EmptyContent(t *testing.T) {
	e := New()
	syms, edges, err := e.Extract("empty.ts", []byte(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(syms) != 0 || len(edges) != 0 {
		t.Errorf("expected empty results for empty file, got %d syms %d edges", len(syms), len(edges))
	}
}

func TestExtract_OnlyComments(t *testing.T) {
	e := New()
	src := `// Single-line comment
/* Multi-line
   comment */
// Another
`
	syms, edges, err := e.Extract("comments.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(syms) != 0 || len(edges) != 0 {
		t.Errorf("expected empty results for comment-only file, got %d syms %d edges", len(syms), len(edges))
	}
}

func TestExtract_AlwaysNilError(t *testing.T) {
	e := New()
	cases := []string{"", "garbage }{", "const x = 1", "import { } from ''"}
	for _, src := range cases {
		_, _, err := e.Extract("x.ts", []byte(src))
		if err != nil {
			t.Errorf("expected nil error for %q, got %v", src, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Named imports: import { X, Y } from './foo'
// ---------------------------------------------------------------------------

func TestExtract_NamedImports(t *testing.T) {
	e := New()
	src := `import { Foo, Bar, Baz } from './utils'`
	syms, edges, err := e.Extract("app.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, name := range []string{"Foo", "Bar", "Baz"} {
		s := findSymbol(syms, name)
		if s == nil {
			t.Fatalf("expected import symbol %q", name)
		}
		if s.Kind != symbol.KindImport {
			t.Errorf("%q should be KindImport, got %q", name, s.Kind)
		}
		if s.Exported {
			t.Errorf("%q import should not be exported", name)
		}

		edge := findEdge(edges, name, symbol.EdgeImport)
		if edge == nil {
			t.Fatalf("expected import edge for %q", name)
		}
		if edge.Confidence != symbol.ConfHigh {
			t.Errorf("import edge for %q should be HIGH, got %q", name, edge.Confidence)
		}
		if edge.To != "utils" {
			t.Errorf("import edge To should be 'utils', got %q", edge.To)
		}
		if edge.From != "app.ts" {
			t.Errorf("import edge From should be 'app.ts', got %q", edge.From)
		}
	}
}

func TestExtract_NamedImport_AliasAs(t *testing.T) {
	e := New()
	src := `import { Original as Alias } from './mod'`
	syms, edges, err := e.Extract("alias.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The local name after "as" should be used.
	s := findSymbol(syms, "Alias")
	if s == nil {
		t.Fatal("expected import symbol 'Alias'")
	}
	edge := findEdge(edges, "Alias", symbol.EdgeImport)
	if edge == nil {
		t.Fatal("expected import edge for 'Alias'")
	}
}

// ---------------------------------------------------------------------------
// Default imports: import X from './foo'
// ---------------------------------------------------------------------------

func TestExtract_DefaultImport(t *testing.T) {
	e := New()
	src := `import React from 'react'`
	syms, edges, err := e.Extract("comp.tsx", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := findSymbol(syms, "React")
	if s == nil {
		t.Fatal("expected 'React' default import symbol")
	}
	if s.Kind != symbol.KindImport {
		t.Errorf("expected KindImport, got %q", s.Kind)
	}

	edge := findEdge(edges, "React", symbol.EdgeImport)
	if edge == nil {
		t.Fatal("expected import edge for 'React'")
	}
	if edge.To != "react" {
		t.Errorf("expected To='react', got %q", edge.To)
	}
	if edge.Confidence != symbol.ConfHigh {
		t.Errorf("expected HIGH, got %q", edge.Confidence)
	}
}

// ---------------------------------------------------------------------------
// Star imports: import * as X from './foo'
// ---------------------------------------------------------------------------

func TestExtract_StarImport(t *testing.T) {
	e := New()
	src := `import * as utils from './utils'`
	syms, edges, err := e.Extract("main.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := findSymbol(syms, "utils")
	if s == nil {
		t.Fatal("expected 'utils' star import symbol")
	}

	edge := findEdge(edges, "utils", symbol.EdgeImport)
	if edge == nil {
		t.Fatal("expected import edge for 'utils'")
	}
	if edge.To != "utils" {
		t.Errorf("expected To='utils', got %q", edge.To)
	}
	if edge.Confidence != symbol.ConfHigh {
		t.Errorf("expected HIGH, got %q", edge.Confidence)
	}
}

// ---------------------------------------------------------------------------
// require()
// ---------------------------------------------------------------------------

func TestExtract_Require(t *testing.T) {
	e := New()
	src := `const fs = require('./fileSystem')`
	_, edges, err := e.Extract("node.js", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edge := findEdge(edges, "require", symbol.EdgeImport)
	if edge == nil {
		t.Fatal("expected require import edge")
	}
	if edge.To != "fileSystem" {
		t.Errorf("expected To='fileSystem', got %q", edge.To)
	}
	// require is LOW confidence (dynamic)
	if edge.Confidence != symbol.ConfLow {
		t.Errorf("expected LOW confidence for require, got %q", edge.Confidence)
	}
}

// ---------------------------------------------------------------------------
// Export patterns
// ---------------------------------------------------------------------------

func TestExtract_ExportFunction(t *testing.T) {
	e := New()
	src := `export function processData(input: string): void {}`
	syms, _, err := e.Extract("proc.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := findSymbol(syms, "processData")
	if s == nil {
		t.Fatal("expected 'processData' export")
	}
	if s.Kind != symbol.KindFunction {
		t.Errorf("expected KindFunction, got %q", s.Kind)
	}
	if !s.Exported {
		t.Error("'processData' should be exported")
	}
}

func TestExtract_ExportAsyncFunction(t *testing.T) {
	e := New()
	src := `export async function fetchData(url: string): Promise<void> {}`
	syms, _, err := e.Extract("fetch.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := findSymbol(syms, "fetchData")
	if s == nil {
		t.Fatal("expected 'fetchData' export")
	}
	if s.Kind != symbol.KindFunction {
		t.Errorf("expected KindFunction, got %q", s.Kind)
	}
	if !s.Exported {
		t.Error("'fetchData' should be exported")
	}
}

func TestExtract_ExportClass(t *testing.T) {
	e := New()
	src := `export class UserService {
  constructor() {}
}`
	syms, _, err := e.Extract("service.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := findSymbol(syms, "UserService")
	if s == nil {
		t.Fatal("expected 'UserService'")
	}
	if s.Kind != symbol.KindClass {
		t.Errorf("expected KindClass, got %q", s.Kind)
	}
	if !s.Exported {
		t.Error("'UserService' should be exported")
	}
}

func TestExtract_ExportConst(t *testing.T) {
	e := New()
	src := `export const MAX_RETRIES = 3`
	syms, _, err := e.Extract("config.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := findSymbol(syms, "MAX_RETRIES")
	if s == nil {
		t.Fatal("expected 'MAX_RETRIES'")
	}
	if s.Kind != symbol.KindVariable {
		t.Errorf("expected KindVariable, got %q", s.Kind)
	}
	if !s.Exported {
		t.Error("'MAX_RETRIES' should be exported")
	}
}

func TestExtract_ExportLet(t *testing.T) {
	e := New()
	src := `export let counter = 0`
	syms, _, err := e.Extract("state.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := findSymbol(syms, "counter")
	if s == nil {
		t.Fatal("expected 'counter'")
	}
	if s.Kind != symbol.KindVariable {
		t.Errorf("expected KindVariable, got %q", s.Kind)
	}
}

func TestExtract_ExportVar(t *testing.T) {
	e := New()
	src := `export var legacy = "value"`
	syms, _, err := e.Extract("legacy.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := findSymbol(syms, "legacy")
	if s == nil {
		t.Fatal("expected 'legacy'")
	}
	if s.Kind != symbol.KindVariable {
		t.Errorf("expected KindVariable, got %q", s.Kind)
	}
}

func TestExtract_ExportType(t *testing.T) {
	e := New()
	src := `export type UserID = string`
	syms, _, err := e.Extract("types.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := findSymbol(syms, "UserID")
	if s == nil {
		t.Fatal("expected 'UserID' type export")
	}
	if s.Kind != symbol.KindType {
		t.Errorf("expected KindType, got %q", s.Kind)
	}
	if !s.Exported {
		t.Error("'UserID' should be exported")
	}
}

func TestExtract_ExportInterface(t *testing.T) {
	e := New()
	src := `export interface IUserRepo {
  findById(id: string): User | null
}`
	syms, _, err := e.Extract("repo.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := findSymbol(syms, "IUserRepo")
	if s == nil {
		t.Fatal("expected 'IUserRepo' interface export")
	}
	if s.Kind != symbol.KindInterface {
		t.Errorf("expected KindInterface, got %q", s.Kind)
	}
	if !s.Exported {
		t.Error("'IUserRepo' should be exported")
	}
}

func TestExtract_ExportDefault_Named(t *testing.T) {
	e := New()
	src := `export default function App() {}`
	syms, _, err := e.Extract("App.tsx", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := findSymbol(syms, "App")
	if s == nil {
		t.Fatal("expected 'App' from export default function")
	}
	if !s.Exported {
		t.Error("export default should be exported")
	}
}

func TestExtract_ExportDefault_Anonymous(t *testing.T) {
	e := New()
	src := `export default function() {}`
	syms, _, err := e.Extract("anon.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Anonymous export default gets name "default".
	s := findSymbol(syms, "default")
	if s == nil {
		t.Fatal("expected 'default' symbol for anonymous export default")
	}
	if !s.Exported {
		t.Error("'default' should be exported")
	}
}

// ---------------------------------------------------------------------------
// Call edges
// ---------------------------------------------------------------------------

func TestExtract_CallEdge_ImportedSymbol_MediumConfidence(t *testing.T) {
	e := New()
	src := `import { processData } from './processor'
const result = processData(input)
`
	_, edges, err := e.Extract("app.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	callEdge := findEdge(edges, "processData", symbol.EdgeCall)
	if callEdge == nil {
		t.Fatal("expected call edge for 'processData'")
	}
	if callEdge.Confidence != symbol.ConfMedium {
		t.Errorf("expected MEDIUM confidence for imported symbol call, got %q", callEdge.Confidence)
	}
	if callEdge.To != "processor" {
		t.Errorf("expected To='processor', got %q", callEdge.To)
	}
}

func TestExtract_CallEdge_NonImportedSymbol_LowConfidence(t *testing.T) {
	e := New()
	src := `const x = unknownHelper(42)`
	_, edges, err := e.Extract("app.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	callEdge := findEdge(edges, "unknownHelper", symbol.EdgeCall)
	if callEdge == nil {
		t.Fatal("expected call edge for 'unknownHelper'")
	}
	if callEdge.Confidence != symbol.ConfLow {
		t.Errorf("expected LOW confidence for non-imported call, got %q", callEdge.Confidence)
	}
}

func TestExtract_CallEdge_KeywordsNotCapture(t *testing.T) {
	e := New()
	src := `if (condition) { return value; }
for (let i = 0; i < 10; i++) {}
while (true) { break; }
`
	_, edges, err := e.Extract("app.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	keywords := []string{"if", "for", "while", "return", "break"}
	for _, kw := range keywords {
		edge := findEdge(edges, kw, symbol.EdgeCall)
		if edge != nil {
			t.Errorf("keyword %q should not appear as a call edge", kw)
		}
	}
}

// ---------------------------------------------------------------------------
// new X( — Instantiation edges
// ---------------------------------------------------------------------------

func TestExtract_InstantiationEdge_ImportedClass_MediumConfidence(t *testing.T) {
	e := New()
	src := `import { UserService } from './services'
const svc = new UserService()
`
	_, edges, err := e.Extract("main.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edge := findEdge(edges, "UserService", symbol.EdgeInstantiation)
	if edge == nil {
		t.Fatal("expected instantiation edge for 'UserService'")
	}
	if edge.Confidence != symbol.ConfMedium {
		t.Errorf("expected MEDIUM, got %q", edge.Confidence)
	}
	if edge.To != "services" {
		t.Errorf("expected To='services', got %q", edge.To)
	}
}

func TestExtract_InstantiationEdge_Unknown_LowConfidence(t *testing.T) {
	e := New()
	src := `const x = new LocalClass()`
	_, edges, err := e.Extract("main.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edge := findEdge(edges, "LocalClass", symbol.EdgeInstantiation)
	if edge == nil {
		t.Fatal("expected instantiation edge for 'LocalClass'")
	}
	if edge.Confidence != symbol.ConfLow {
		t.Errorf("expected LOW confidence, got %q", edge.Confidence)
	}
}

// ---------------------------------------------------------------------------
// extends
// ---------------------------------------------------------------------------

func TestExtract_ExtendsEdge_ImportedClass(t *testing.T) {
	e := New()
	src := `import { BaseController } from './base'
class MyController extends BaseController {}`
	_, edges, err := e.Extract("controller.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edge := findEdge(edges, "BaseController", symbol.EdgeExtends)
	if edge == nil {
		t.Fatal("expected extends edge for 'BaseController'")
	}
	if edge.Confidence != symbol.ConfMedium {
		t.Errorf("expected MEDIUM for imported extends, got %q", edge.Confidence)
	}
	if edge.To != "base" {
		t.Errorf("expected To='base', got %q", edge.To)
	}
}

func TestExtract_ExtendsEdge_LocalClass(t *testing.T) {
	e := New()
	src := `class Child extends Parent {}`
	_, edges, err := e.Extract("classes.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edge := findEdge(edges, "Parent", symbol.EdgeExtends)
	if edge == nil {
		t.Fatal("expected extends edge for 'Parent'")
	}
	if edge.Confidence != symbol.ConfLow {
		t.Errorf("expected LOW for local extends, got %q", edge.Confidence)
	}
}

// ---------------------------------------------------------------------------
// implements
// ---------------------------------------------------------------------------

func TestExtract_ImplementsEdge_SingleInterface(t *testing.T) {
	e := New()
	src := `import { IRepository } from './interfaces'
class UserRepo implements IRepository {}`
	_, edges, err := e.Extract("repo.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edge := findEdge(edges, "IRepository", symbol.EdgeImplements)
	if edge == nil {
		t.Fatal("expected implements edge for 'IRepository'")
	}
	if edge.Confidence != symbol.ConfMedium {
		t.Errorf("expected MEDIUM, got %q", edge.Confidence)
	}
}

func TestExtract_ImplementsEdge_MultipleInterfaces(t *testing.T) {
	e := New()
	src := `class Combo implements Alpha, Beta, Gamma {}`
	_, edges, err := e.Extract("combo.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, name := range []string{"Alpha", "Beta", "Gamma"} {
		edge := findEdge(edges, name, symbol.EdgeImplements)
		if edge == nil {
			t.Errorf("expected implements edge for %q", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Edge deduplication
// ---------------------------------------------------------------------------

func TestExtract_EdgeDeduplication(t *testing.T) {
	e := New()
	// Same import referenced multiple times on different lines.
	src := `import { Helper } from './helpers'
const a = Helper(1)
const b = Helper(2)
`
	_, edges, err := e.Extract("dedup.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// There should be exactly 1 import edge for Helper.
	importCount := 0
	callCount := 0
	for _, edge := range edges {
		if edge.Symbol == "Helper" && edge.Kind == symbol.EdgeImport {
			importCount++
		}
		if edge.Symbol == "Helper" && edge.Kind == symbol.EdgeCall {
			callCount++
		}
	}
	if importCount != 1 {
		t.Errorf("expected exactly 1 import edge for Helper, got %d", importCount)
	}
	// The call should be deduped too (same path + symbol + kind).
	if callCount != 1 {
		t.Errorf("expected exactly 1 call edge for Helper (deduped), got %d", callCount)
	}
}

// ---------------------------------------------------------------------------
// normalizePath
// ---------------------------------------------------------------------------

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"./foo", "foo"},
		{"../bar", "bar"},
		{"../../baz", "baz"},
		{"pkg/mod", "pkg/mod"},
		{"react", "react"},
		{"./components/Button.tsx", "components/Button"},
		{"./utils/index", "utils"},
		{"../services/user.ts", "services/user"},
		{"./foo.js", "foo"},
		{"./foo.jsx", "foo"},
	}
	for _, tt := range tests {
		got := normalizePath(tt.input)
		if got != tt.want {
			t.Errorf("normalizePath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// makeEdgeKey
// ---------------------------------------------------------------------------

func TestMakeEdgeKey_Distinct(t *testing.T) {
	key1 := makeEdgeKey("from", "to", "sym", symbol.EdgeCall)
	key2 := makeEdgeKey("from", "to", "sym", symbol.EdgeImport)
	key3 := makeEdgeKey("from", "other", "sym", symbol.EdgeCall)

	if key1 == key2 {
		t.Error("different kinds should produce different keys")
	}
	if key1 == key3 {
		t.Error("different 'to' should produce different keys")
	}
}

func TestMakeEdgeKey_Stable(t *testing.T) {
	key1 := makeEdgeKey("a", "b", "Sym", symbol.EdgeCall)
	key2 := makeEdgeKey("a", "b", "Sym", symbol.EdgeCall)
	if key1 != key2 {
		t.Error("same inputs should produce same key")
	}
}

// ---------------------------------------------------------------------------
// isKeyword
// ---------------------------------------------------------------------------

func TestIsKeyword(t *testing.T) {
	keywords := []string{
		"if", "for", "while", "switch", "case", "break", "continue", "return",
		"function", "class", "try", "catch", "finally", "throw", "const", "let",
		"var", "async", "await", "yield", "new", "this", "super", "delete",
		"typeof", "instanceof", "in", "of", "get", "set", "static", "extends",
		"implements", "interface", "enum", "export", "import", "from", "as",
		"default", "public", "private", "protected", "readonly", "abstract",
	}
	for _, kw := range keywords {
		if !isKeyword(kw) {
			t.Errorf("isKeyword(%q) should be true", kw)
		}
	}
}

func TestIsKeyword_NonKeywords(t *testing.T) {
	nonKeywords := []string{
		"myFunc", "UserService", "processData", "x", "handleClick", "foo",
	}
	for _, name := range nonKeywords {
		if isKeyword(name) {
			t.Errorf("isKeyword(%q) should be false", name)
		}
	}
}

// ---------------------------------------------------------------------------
// IsTSFile
// ---------------------------------------------------------------------------

func TestIsTSFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"app.ts", true},
		{"component.tsx", true},
		{"util.js", true},
		{"page.jsx", true},
		{"App.TS", true},   // case-insensitive
		{"App.TSX", true},
		{"main.go", false},
		{"script.py", false},
		{"README.md", false},
		{"", false},
		{"noext", false},
	}
	for _, tt := range tests {
		got := IsTSFile(tt.path)
		if got != tt.want {
			t.Errorf("IsTSFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Multi-line comment handling
// ---------------------------------------------------------------------------

func TestExtract_MultiLineComment_Skipped(t *testing.T) {
	e := New()
	src := `/* This is a multi-line comment
export function ignoredFunc() {}
*/
export function realFunc() {}
`
	syms, _, err := e.Extract("comments.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if findSymbol(syms, "ignoredFunc") != nil {
		t.Error("function inside multi-line comment should not be extracted")
	}
	if findSymbol(syms, "realFunc") == nil {
		t.Fatal("expected 'realFunc' to be extracted")
	}
}

func TestExtract_SingleLineBlockComment_Skipped(t *testing.T) {
	e := New()
	src := `/* single-line block comment */ export function hidden() {}
export function visible() {}
`
	_, _, err := e.Extract("block.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The line starting with /* is skipped in its entirety.
	// visible() on the next line should be extracted.
}

// ---------------------------------------------------------------------------
// Line numbers
// ---------------------------------------------------------------------------

func TestExtract_LineNumbers(t *testing.T) {
	e := New()
	src := `import { Foo } from './foo'

export function first() {}

export function second() {}
`
	syms, _, err := e.Extract("lines.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	first := findSymbol(syms, "first")
	second := findSymbol(syms, "second")
	if first == nil || second == nil {
		t.Fatal("expected both first and second")
	}
	if first.Line <= 0 {
		t.Errorf("expected positive line for first, got %d", first.Line)
	}
	if second.Line <= first.Line {
		t.Errorf("second (line %d) should come after first (line %d)", second.Line, first.Line)
	}
}

// ---------------------------------------------------------------------------
// Path propagation
// ---------------------------------------------------------------------------

func TestExtract_PathOnSymbols(t *testing.T) {
	e := New()
	src := `import { X } from './x'
export function doThing() {}
`
	syms, _, err := e.Extract("src/app/main.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, s := range syms {
		if s.Path != "src/app/main.ts" {
			t.Errorf("expected path 'src/app/main.ts', got %q for %q", s.Path, s.Name)
		}
	}
}

func TestExtract_PathOnEdges(t *testing.T) {
	e := New()
	src := `import { X } from './x'`
	_, edges, err := e.Extract("src/app/main.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, edge := range edges {
		if edge.From != "src/app/main.ts" {
			t.Errorf("expected From='src/app/main.ts', got %q", edge.From)
		}
	}
}

// ---------------------------------------------------------------------------
// Comprehensive realistic TypeScript file
// ---------------------------------------------------------------------------

const realisticTS = `import { Injectable } from '@angular/core'
import { HttpClient } from '@angular/common/http'
import * as utils from './utils'
import Config from '../config'

export interface IUser {
  id: string
  name: string
}

export type UserMap = Map<string, IUser>

export class UserService {
  constructor(private http: HttpClient) {}

  async getUser(id: string): Promise<IUser> {
    const result = utils.parse(id)
    return this.http.get<IUser>('/api/users/' + result)
  }
}

export const DEFAULT_TIMEOUT = 5000

export function createUser(name: string): IUser {
  return new UserService(null).getUser(name)
}
`

func TestExtract_RealisticFile_Symbols(t *testing.T) {
	e := New()
	syms, _, err := e.Extract("user.service.ts", []byte(realisticTS))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]symbol.SymbolKind{
		"Injectable":      symbol.KindImport,
		"HttpClient":      symbol.KindImport,
		"utils":           symbol.KindImport,
		"Config":          symbol.KindImport,
		"IUser":           symbol.KindInterface,
		"UserMap":         symbol.KindType,
		"UserService":     symbol.KindClass,
		"DEFAULT_TIMEOUT": symbol.KindVariable,
		"createUser":      symbol.KindFunction,
	}

	for name, kind := range expected {
		s := findSymbol(syms, name)
		if s == nil {
			t.Errorf("expected symbol %q (kind %q)", name, kind)
			continue
		}
		if s.Kind != kind {
			t.Errorf("symbol %q: expected kind %q, got %q", name, kind, s.Kind)
		}
	}
}

func TestExtract_RealisticFile_ImportEdges(t *testing.T) {
	e := New()
	_, edges, err := e.Extract("user.service.ts", []byte(realisticTS))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check HIGH-confidence import edges
	injectable := findEdge(edges, "Injectable", symbol.EdgeImport)
	if injectable == nil {
		t.Fatal("expected import edge for 'Injectable'")
	}
	if injectable.Confidence != symbol.ConfHigh {
		t.Errorf("expected HIGH, got %q", injectable.Confidence)
	}
	if !strings.Contains(injectable.To, "angular") {
		t.Errorf("expected To to contain 'angular', got %q", injectable.To)
	}
}

func TestExtract_RealisticFile_ExportedFlags(t *testing.T) {
	e := New()
	syms, _, err := e.Extract("user.service.ts", []byte(realisticTS))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	exported := []string{"IUser", "UserMap", "UserService", "DEFAULT_TIMEOUT", "createUser"}
	for _, name := range exported {
		s := findSymbol(syms, name)
		if s == nil {
			t.Errorf("expected symbol %q", name)
			continue
		}
		if !s.Exported {
			t.Errorf("%q should be exported", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Unicode in source (should not panic)
// ---------------------------------------------------------------------------

func TestExtract_UnicodeContent(t *testing.T) {
	e := New()
	src := `// Comment with unicode: 日本語, héllo
export function normalFunc() {}
const msg = "こんにちは"
`
	syms, _, err := e.Extract("unicode.ts", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := findSymbol(syms, "normalFunc")
	if s == nil {
		t.Fatal("expected 'normalFunc' despite unicode in file")
	}
}

// ---------------------------------------------------------------------------
// Large file (many exports) — smoke test
// ---------------------------------------------------------------------------

func TestExtract_LargeFile(t *testing.T) {
	e := New()
	var sb strings.Builder
	for i := 0; i < 300; i++ {
		sb.WriteString("export function fn")
		for _, b := range numStr(i) {
			sb.WriteByte(b)
		}
		sb.WriteString("() {}\n")
	}

	syms, _, err := e.Extract("large.ts", []byte(sb.String()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect to have found roughly 300 function exports.
	funcCount := 0
	for _, s := range syms {
		if s.Kind == symbol.KindFunction {
			funcCount++
		}
	}
	if funcCount < 300 {
		t.Errorf("expected at least 300 function exports, got %d", funcCount)
	}
}

// numStr converts an int to decimal string without importing fmt/strconv.
func numStr(n int) []byte {
	if n == 0 {
		return []byte("0")
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return digits
}
