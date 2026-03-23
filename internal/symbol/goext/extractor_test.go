package goext

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

func findEdge(edges []symbol.Edge, sym string) *symbol.Edge {
	for i := range edges {
		if edges[i].Symbol == sym {
			return &edges[i]
		}
	}
	return nil
}

func countKind(syms []symbol.Symbol, kind symbol.SymbolKind) int {
	n := 0
	for _, s := range syms {
		if s.Kind == kind {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// New / Language
// ---------------------------------------------------------------------------

func TestNew(t *testing.T) {
	e := New()
	if e == nil {
		t.Fatal("New() returned nil")
	}
}

func TestLanguage(t *testing.T) {
	e := New()
	if e.Language() != "go" {
		t.Errorf("expected language 'go', got %q", e.Language())
	}
}

// ---------------------------------------------------------------------------
// Extract — basic happy path
// ---------------------------------------------------------------------------

const simpleGo = `package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("hello")
}

func helper() {}
`

func TestExtract_BasicFunctions(t *testing.T) {
	e := New()
	syms, edges, err := e.Extract("main.go", []byte(simpleGo))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mainSym := findSymbol(syms, "main")
	if mainSym == nil {
		t.Fatal("expected to find 'main' function")
	}
	if mainSym.Kind != symbol.KindFunction {
		t.Errorf("expected KindFunction, got %q", mainSym.Kind)
	}
	if mainSym.Exported {
		t.Error("'main' should not be considered exported (lowercase in Go)")
	}

	helperSym := findSymbol(syms, "helper")
	if helperSym == nil {
		t.Fatal("expected to find 'helper' function")
	}

	// Two import edges: fmt and os
	importEdges := 0
	for _, edge := range edges {
		if edge.Kind == symbol.EdgeImport {
			importEdges++
		}
	}
	if importEdges != 2 {
		t.Errorf("expected 2 import edges, got %d", importEdges)
	}
}

func TestExtract_ImportSymbolsAndEdges(t *testing.T) {
	e := New()
	src := `package foo

import (
	"fmt"
	myfmt "github.com/pkg/foo"
)

func Run() {}
`
	syms, edges, err := e.Extract("foo.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// fmt import — local name derived from last path segment
	fmtSym := findSymbol(syms, "fmt")
	if fmtSym == nil {
		t.Fatal("expected 'fmt' import symbol")
	}
	if fmtSym.Kind != symbol.KindImport {
		t.Errorf("expected KindImport, got %q", fmtSym.Kind)
	}
	if fmtSym.Exported {
		t.Error("imports should not be exported")
	}

	// Named import: myfmt
	myfmtSym := findSymbol(syms, "myfmt")
	if myfmtSym == nil {
		t.Fatal("expected 'myfmt' named import symbol")
	}

	// Import edge for fmt
	fmtEdge := findEdge(edges, "fmt")
	if fmtEdge == nil {
		t.Fatal("expected edge for 'fmt'")
	}
	if fmtEdge.Confidence != symbol.ConfHigh {
		t.Errorf("import edges should be HIGH confidence, got %q", fmtEdge.Confidence)
	}
	if fmtEdge.Kind != symbol.EdgeImport {
		t.Errorf("expected EdgeImport kind, got %q", fmtEdge.Kind)
	}
	if fmtEdge.From != "foo.go" {
		t.Errorf("expected From='foo.go', got %q", fmtEdge.From)
	}
}

// ---------------------------------------------------------------------------
// Types: struct, interface, type alias, variable
// ---------------------------------------------------------------------------

const typesGo = `package types

type MyStruct struct {
	Field string
}

type MyInterface interface {
	DoThing() error
}

type MyType = string

var globalVar = "hello"
var ExportedVar = 42

const unexportedConst = "x"
const ExportedConst = "y"
`

func TestExtract_StructBecomesClass(t *testing.T) {
	e := New()
	syms, _, err := e.Extract("types.go", []byte(typesGo))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := findSymbol(syms, "MyStruct")
	if s == nil {
		t.Fatal("expected 'MyStruct'")
	}
	if s.Kind != symbol.KindClass {
		t.Errorf("struct should map to KindClass, got %q", s.Kind)
	}
	if !s.Exported {
		t.Error("MyStruct should be exported")
	}
}

func TestExtract_InterfaceKind(t *testing.T) {
	e := New()
	syms, _, err := e.Extract("types.go", []byte(typesGo))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := findSymbol(syms, "MyInterface")
	if s == nil {
		t.Fatal("expected 'MyInterface'")
	}
	if s.Kind != symbol.KindInterface {
		t.Errorf("interface should map to KindInterface, got %q", s.Kind)
	}
	if !s.Exported {
		t.Error("MyInterface should be exported")
	}
}

func TestExtract_TypeAlias(t *testing.T) {
	e := New()
	syms, _, err := e.Extract("types.go", []byte(typesGo))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := findSymbol(syms, "MyType")
	if s == nil {
		t.Fatal("expected 'MyType'")
	}
	if s.Kind != symbol.KindType {
		t.Errorf("type alias should map to KindType, got %q", s.Kind)
	}
}

func TestExtract_Variables(t *testing.T) {
	e := New()
	syms, _, err := e.Extract("types.go", []byte(typesGo))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gv := findSymbol(syms, "globalVar")
	if gv == nil {
		t.Fatal("expected 'globalVar'")
	}
	if gv.Kind != symbol.KindVariable {
		t.Errorf("expected KindVariable, got %q", gv.Kind)
	}
	if gv.Exported {
		t.Error("globalVar should not be exported")
	}

	ev := findSymbol(syms, "ExportedVar")
	if ev == nil {
		t.Fatal("expected 'ExportedVar'")
	}
	if !ev.Exported {
		t.Error("ExportedVar should be exported")
	}
}

// ---------------------------------------------------------------------------
// Export detection (uppercase = exported)
// ---------------------------------------------------------------------------

func TestExtract_ExportedFunction(t *testing.T) {
	e := New()
	src := `package pkg

func PublicFunc() {}
func privateFunc() {}
`
	syms, _, err := e.Extract("pkg.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pub := findSymbol(syms, "PublicFunc")
	if pub == nil {
		t.Fatal("expected 'PublicFunc'")
	}
	if !pub.Exported {
		t.Error("PublicFunc should be exported")
	}

	priv := findSymbol(syms, "privateFunc")
	if priv == nil {
		t.Fatal("expected 'privateFunc'")
	}
	if priv.Exported {
		t.Error("privateFunc should not be exported")
	}
}

// ---------------------------------------------------------------------------
// Call edges (MEDIUM confidence selector calls)
// ---------------------------------------------------------------------------

func TestExtract_CallEdgesMediumConfidence(t *testing.T) {
	e := New()
	src := `package main

import "fmt"

func main() {
	fmt.Println("hello")
	fmt.Fprintf(nil, "x")
}
`
	_, edges, err := e.Extract("main.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var callEdges []symbol.Edge
	for _, edge := range edges {
		if edge.Kind == symbol.EdgeCall {
			callEdges = append(callEdges, edge)
		}
	}
	if len(callEdges) < 2 {
		t.Fatalf("expected at least 2 call edges (Println + Fprintf), got %d", len(callEdges))
	}
	for _, ce := range callEdges {
		if ce.Confidence != symbol.ConfMedium {
			t.Errorf("call edges should be MEDIUM confidence, got %q for %q", ce.Confidence, ce.Symbol)
		}
		if ce.To != "fmt" {
			t.Errorf("expected To='fmt', got %q", ce.To)
		}
	}

	// Specific symbols
	println := findEdge(callEdges, "Println")
	if println == nil {
		// Search differently since findEdge searches by Symbol
		found := false
		for _, ce := range callEdges {
			if ce.Symbol == "Println" {
				found = true
			}
		}
		if !found {
			t.Error("expected call edge for 'Println'")
		}
	}
}

func TestExtract_NoCallEdgesWithoutBody(t *testing.T) {
	e := New()
	src := `package main

type Foo struct{}
`
	_, edges, err := e.Extract("main.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, edge := range edges {
		if edge.Kind == symbol.EdgeCall {
			t.Errorf("unexpected call edge: %+v", edge)
		}
	}
}

// ---------------------------------------------------------------------------
// Line numbers
// ---------------------------------------------------------------------------

func TestExtract_LineNumbers(t *testing.T) {
	e := New()
	src := `package main

func First() {}

func Second() {}
`
	syms, _, err := e.Extract("main.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	first := findSymbol(syms, "First")
	second := findSymbol(syms, "Second")
	if first == nil || second == nil {
		t.Fatal("expected both First and Second")
	}
	if first.Line <= 0 {
		t.Errorf("expected positive line number for First, got %d", first.Line)
	}
	if second.Line <= first.Line {
		t.Errorf("Second (line %d) should come after First (line %d)", second.Line, first.Line)
	}
}

// ---------------------------------------------------------------------------
// Path propagation
// ---------------------------------------------------------------------------

func TestExtract_PathSetOnAllSymbols(t *testing.T) {
	e := New()
	src := `package main

import "fmt"

func Run() {}

type Config struct{}
`
	syms, _, err := e.Extract("internal/app/main.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, s := range syms {
		if s.Path != "internal/app/main.go" {
			t.Errorf("expected path 'internal/app/main.go', got %q for symbol %q", s.Path, s.Name)
		}
	}
}

func TestExtract_PathSetOnEdges(t *testing.T) {
	e := New()
	src := `package main

import "fmt"

func main() {
	fmt.Println("hi")
}
`
	_, edges, err := e.Extract("cmd/main.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, edge := range edges {
		if edge.From != "cmd/main.go" {
			t.Errorf("expected From='cmd/main.go', got %q", edge.From)
		}
	}
}

// ---------------------------------------------------------------------------
// Import path — last segment becomes local name
// ---------------------------------------------------------------------------

func TestExtract_ImportLastSegmentLocalName(t *testing.T) {
	e := New()
	src := `package main

import (
	"github.com/scrypster/huginn/internal/symbol"
)

func main() {}
`
	syms, _, err := e.Extract("main.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := findSymbol(syms, "symbol")
	if s == nil {
		t.Error("expected local name 'symbol' from path 'github.com/scrypster/huginn/internal/symbol'")
	}
}

// ---------------------------------------------------------------------------
// Malformed / invalid Go — graceful degradation
// ---------------------------------------------------------------------------

func TestExtract_MalformedGo_ReturnsEmpty(t *testing.T) {
	e := New()
	src := `this is not valid go code }{}{`
	syms, edges, err := e.Extract("bad.go", []byte(src))
	if err != nil {
		t.Errorf("expected no error on parse failure (graceful degradation), got: %v", err)
	}
	if len(syms) != 0 || len(edges) != 0 {
		t.Errorf("expected empty results on parse failure, got %d syms %d edges", len(syms), len(edges))
	}
}

func TestExtract_EmptyContent_ReturnsEmpty(t *testing.T) {
	e := New()
	syms, edges, err := e.Extract("empty.go", []byte(""))
	if err != nil {
		t.Errorf("unexpected error for empty content: %v", err)
	}
	// Empty content is not valid Go, so graceful degradation should apply.
	if len(syms) != 0 || len(edges) != 0 {
		t.Errorf("expected empty results for empty file, got %d syms %d edges", len(syms), len(edges))
	}
}

func TestExtract_PackageOnlyNoDecls(t *testing.T) {
	e := New()
	src := `package main`
	syms, edges, err := e.Extract("pkg.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(syms) != 0 {
		t.Errorf("expected 0 symbols for package-only file, got %d", len(syms))
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges for package-only file, got %d", len(edges))
	}
}

// ---------------------------------------------------------------------------
// Multiple declarations in one var/const block
// ---------------------------------------------------------------------------

func TestExtract_MultipleVarsInBlock(t *testing.T) {
	e := New()
	src := `package main

var (
	Alpha = 1
	Beta  = 2
	gamma = 3
)
`
	syms, _, err := e.Extract("vars.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	alpha := findSymbol(syms, "Alpha")
	beta := findSymbol(syms, "Beta")
	gamma := findSymbol(syms, "gamma")

	if alpha == nil || beta == nil || gamma == nil {
		t.Fatalf("expected Alpha, Beta, gamma; got: %v", syms)
	}
	if !alpha.Exported || !beta.Exported {
		t.Error("Alpha and Beta should be exported")
	}
	if gamma.Exported {
		t.Error("gamma should not be exported")
	}
}

// ---------------------------------------------------------------------------
// Method receivers (receiver methods are still FuncDecl)
// ---------------------------------------------------------------------------

func TestExtract_MethodOnReceiver(t *testing.T) {
	e := New()
	src := `package main

type Server struct{}

func (s *Server) Start() error { return nil }
func (s Server) Stop() {}
`
	syms, _, err := e.Extract("server.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	start := findSymbol(syms, "Start")
	stop := findSymbol(syms, "Stop")
	if start == nil {
		t.Fatal("expected 'Start' method")
	}
	if stop == nil {
		t.Fatal("expected 'Stop' method")
	}
	if !start.Exported || !stop.Exported {
		t.Error("Start and Stop should be exported")
	}
}

// ---------------------------------------------------------------------------
// IsGoFile
// ---------------------------------------------------------------------------

func TestIsGoFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"main.go", true},
		{"internal/foo/bar.go", true},
		{"main_test.go", false},
		{"internal/foo/bar_test.go", false},
		{"main.ts", false},
		{"main.py", false},
		{"", false},
		{"noext", false},
		{".go", true}, // edge case: file literally named ".go"
	}

	for _, tt := range tests {
		got := IsGoFile(tt.path)
		if got != tt.want {
			t.Errorf("IsGoFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Unicode in string literals (should not panic)
// ---------------------------------------------------------------------------

func TestExtract_UnicodeStringLiterals(t *testing.T) {
	e := New()
	src := `package main

import "fmt"

func Greet() {
	msg := "héllo wörld 日本語"
	fmt.Println(msg)
}
`
	syms, _, err := e.Extract("unicode.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	g := findSymbol(syms, "Greet")
	if g == nil {
		t.Fatal("expected 'Greet' function")
	}
}

// ---------------------------------------------------------------------------
// Large file (many declarations) — smoke test
// ---------------------------------------------------------------------------

func TestExtract_LargeFile(t *testing.T) {
	e := New()
	var sb strings.Builder
	sb.WriteString("package large\n\n")
	for i := 0; i < 500; i++ {
		sb.WriteString("func ")
		sb.WriteString("Func")
		// write the number without fmt dependency
		for _, digit := range numStr(i) {
			sb.WriteByte(digit)
		}
		sb.WriteString("() {}\n")
	}

	syms, _, err := e.Extract("large.go", []byte(sb.String()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(syms) != 500 {
		t.Errorf("expected 500 symbols, got %d", len(syms))
	}
}

// numStr converts an int to its decimal string without importing fmt/strconv.
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

// ---------------------------------------------------------------------------
// Nested calls inside anonymous functions
// ---------------------------------------------------------------------------

func TestExtract_NestedCallsInsideAnonymousFunc(t *testing.T) {
	e := New()
	src := `package main

import "fmt"

func Run() {
	go func() {
		fmt.Println("goroutine")
	}()
}
`
	_, edges, err := e.Extract("run.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// There should be a call edge for Println inside the anonymous func.
	found := false
	for _, edge := range edges {
		if edge.Kind == symbol.EdgeCall && edge.Symbol == "Println" {
			found = true
		}
	}
	if !found {
		t.Error("expected call edge for Println inside goroutine")
	}
}
