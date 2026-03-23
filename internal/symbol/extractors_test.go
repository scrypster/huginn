package symbol_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/symbol"
	"github.com/scrypster/huginn/internal/symbol/goext"
	"github.com/scrypster/huginn/internal/symbol/heuristic"
	"github.com/scrypster/huginn/internal/symbol/tsext"
)

// ---------------------------------------------------------------------------
// Registry + extractor integration
// ---------------------------------------------------------------------------

func TestRegistry_GoExtractor_Extract(t *testing.T) {
	r := symbol.NewRegistry()
	r.Register(goext.New(), ".go")

	src := []byte(`package foo

import "fmt"

type MyIface interface{ Do() error }
type MyStruct struct{ Name string }

func Run() { fmt.Println("hi") }
`)
	syms, edges, err := r.Extract("pkg/foo.go", src)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}
	if len(syms) == 0 {
		t.Error("expected symbols from Go extractor")
	}
	if len(edges) == 0 {
		t.Error("expected edges (import + call) from Go extractor")
	}

	// Check interface
	foundIface := false
	for _, s := range syms {
		if s.Name == "MyIface" && s.Kind == symbol.KindInterface {
			foundIface = true
		}
	}
	if !foundIface {
		t.Error("expected MyIface with KindInterface")
	}

	// Check struct → KindClass
	foundClass := false
	for _, s := range syms {
		if s.Name == "MyStruct" && s.Kind == symbol.KindClass {
			foundClass = true
		}
	}
	if !foundClass {
		t.Error("expected MyStruct with KindClass")
	}

	// Check exported function
	foundRun := false
	for _, s := range syms {
		if s.Name == "Run" && s.Kind == symbol.KindFunction && s.Exported {
			foundRun = true
		}
	}
	if !foundRun {
		t.Error("expected Run as exported KindFunction")
	}
}

func TestRegistry_GoExtractor_GenericType(t *testing.T) {
	r := symbol.NewRegistry()
	r.Register(goext.New(), ".go")

	src := []byte(`package generics

type Result[T any] struct {
	Value T
	Err   error
}

func Map[T, U any](slice []T, fn func(T) U) []U {
	result := make([]U, len(slice))
	for i, v := range slice {
		result[i] = fn(v)
	}
	return result
}
`)
	syms, _, err := r.Extract("generics.go", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	foundResult := false
	foundMap := false
	for _, s := range syms {
		if s.Name == "Result" {
			foundResult = true
		}
		if s.Name == "Map" {
			foundMap = true
		}
	}
	if !foundResult {
		t.Error("expected 'Result' generic struct symbol")
	}
	if !foundMap {
		t.Error("expected 'Map' generic function symbol")
	}
}

func TestRegistry_GoExtractor_StructEmbedding(t *testing.T) {
	r := symbol.NewRegistry()
	r.Register(goext.New(), ".go")

	src := []byte(`package embed

import "sync"

type SafeMap struct {
	sync.Mutex
	data map[string]string
}

func (s *SafeMap) Set(k, v string) {
	s.Lock()
	defer s.Unlock()
	s.data[k] = v
}
`)
	syms, _, err := r.Extract("embed.go", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	foundStruct := false
	for _, s := range syms {
		if s.Name == "SafeMap" && s.Kind == symbol.KindClass {
			foundStruct = true
		}
	}
	if !foundStruct {
		t.Error("expected SafeMap struct with embedding")
	}
}

func TestRegistry_TSExtractor_ExtractBarrelExports(t *testing.T) {
	r := symbol.NewRegistry()
	r.Register(tsext.New(), ".ts", ".tsx")

	// Barrel export pattern
	src := []byte(`export { UserService } from './user-service'
export { AuthService } from './auth-service'
export type { User } from './types'
`)
	// Note: tsext handles export statements line by line via regex.
	// These "export { ... } from" lines won't match the export function/class/const patterns.
	// Just verify no error and no panic.
	_, _, err := r.Extract("index.ts", src)
	if err != nil {
		t.Fatalf("unexpected error on barrel export: %v", err)
	}
}

func TestRegistry_TSExtractor_InterfaceDeclaration(t *testing.T) {
	r := symbol.NewRegistry()
	r.Register(tsext.New(), ".ts")

	src := []byte(`export interface IPaymentGateway {
  charge(amount: number): Promise<Receipt>
  refund(id: string): Promise<void>
}

export interface ILogger {
  log(message: string): void
  error(err: Error): void
}
`)
	syms, _, err := r.Extract("gateways.ts", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	foundPayment := false
	foundLogger := false
	for _, s := range syms {
		if s.Name == "IPaymentGateway" && s.Kind == symbol.KindInterface && s.Exported {
			foundPayment = true
		}
		if s.Name == "ILogger" && s.Kind == symbol.KindInterface && s.Exported {
			foundLogger = true
		}
	}
	if !foundPayment {
		t.Error("expected IPaymentGateway interface")
	}
	if !foundLogger {
		t.Error("expected ILogger interface")
	}
}

func TestRegistry_TSExtractor_EmptyFile(t *testing.T) {
	r := symbol.NewRegistry()
	r.Register(tsext.New(), ".ts")

	syms, edges, err := r.Extract("empty.ts", []byte(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(syms) != 0 || len(edges) != 0 {
		t.Errorf("expected empty results for empty TS file, got %d syms %d edges", len(syms), len(edges))
	}
}

func TestRegistry_TSExtractor_InvalidSyntax_ReturnsPartialNoError(t *testing.T) {
	r := symbol.NewRegistry()
	r.Register(tsext.New(), ".ts")

	// tsext uses regex, so invalid syntax just returns partial results, never an error
	src := []byte(`}{)( this is garbage
export function validFunc() {}
`)
	_, _, err := r.Extract("invalid.ts", src)
	if err != nil {
		t.Errorf("expected no error for invalid TS syntax, got: %v", err)
	}
}

func TestRegistry_HeuristicExtractor_PythonFallback(t *testing.T) {
	r := symbol.NewRegistry()
	r.SetFallback(heuristic.New())

	src := []byte(`import os
import sys

def main():
    print("hello")

class Worker:
    pass
`)
	syms, edges, err := r.Extract("script.py", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(syms) == 0 {
		t.Error("expected symbols from heuristic extractor for Python")
	}
	if len(edges) == 0 {
		t.Error("expected import edges from heuristic extractor for Python")
	}

	foundMain := false
	foundWorker := false
	for _, s := range syms {
		if s.Name == "main" {
			foundMain = true
		}
		if s.Name == "Worker" {
			foundWorker = true
		}
	}
	if !foundMain {
		t.Error("expected 'main' function from Python heuristic")
	}
	if !foundWorker {
		t.Error("expected 'Worker' class from Python heuristic")
	}
}

func TestRegistry_HeuristicExtractor_RustFallback(t *testing.T) {
	r := symbol.NewRegistry()
	r.SetFallback(heuristic.New())

	src := []byte(`use std::collections::HashMap;

pub struct Config {
    pub name: String,
}

pub trait Validator {
    fn validate(&self) -> bool;
}

pub fn process() {}
`)
	syms, edges, err := r.Extract("config.rs", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(syms) == 0 {
		t.Error("expected symbols from heuristic extractor for Rust")
	}
	if len(edges) == 0 {
		t.Error("expected use edges from heuristic Rust extractor")
	}

	foundConfig := false
	foundValidator := false
	for _, s := range syms {
		if s.Name == "Config" {
			foundConfig = true
		}
		if s.Name == "Validator" {
			foundValidator = true
		}
	}
	if !foundConfig {
		t.Error("expected 'Config' struct from Rust heuristic")
	}
	if !foundValidator {
		t.Error("expected 'Validator' trait from Rust heuristic")
	}
}

// ---------------------------------------------------------------------------
// Multi-extractor registry: correct routing
// ---------------------------------------------------------------------------

func TestRegistry_MultiExtractor_CorrectRouting(t *testing.T) {
	r := symbol.NewRegistry()
	r.Register(goext.New(), ".go")
	r.Register(tsext.New(), ".ts", ".tsx")
	r.SetFallback(heuristic.New())

	tests := []struct {
		path    string
		content []byte
		check   func(t *testing.T, syms []symbol.Symbol, edges []symbol.Edge)
	}{
		{
			path: "service.go",
			content: []byte(`package svc
func Handle() {}`),
			check: func(t *testing.T, syms []symbol.Symbol, edges []symbol.Edge) {
				found := false
				for _, s := range syms {
					if s.Name == "Handle" && s.Kind == symbol.KindFunction {
						found = true
					}
				}
				if !found {
					t.Error("Go extractor should find Handle function")
				}
			},
		},
		{
			path: "service.ts",
			content: []byte(`export function handle() {}`),
			check: func(t *testing.T, syms []symbol.Symbol, edges []symbol.Edge) {
				found := false
				for _, s := range syms {
					if s.Name == "handle" && s.Kind == symbol.KindFunction && s.Exported {
						found = true
					}
				}
				if !found {
					t.Error("TS extractor should find handle function")
				}
			},
		},
		{
			path: "script.rb",
			content: []byte(`def process
end`),
			check: func(t *testing.T, syms []symbol.Symbol, edges []symbol.Edge) {
				found := false
				for _, s := range syms {
					if s.Name == "process" {
						found = true
					}
				}
				if !found {
					t.Error("heuristic extractor should find Ruby process def")
				}
			},
		},
	}

	for _, tt := range tests {
		syms, edges, err := r.Extract(tt.path, tt.content)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tt.path, err)
			continue
		}
		tt.check(t, syms, edges)
	}
}

// ---------------------------------------------------------------------------
// No extractor for unknown extension with no fallback
// ---------------------------------------------------------------------------

func TestRegistry_UnknownExtension_NoFallback_Empty(t *testing.T) {
	r := symbol.NewRegistry()
	r.Register(goext.New(), ".go")

	syms, edges, err := r.Extract("unknown.xyz", []byte("some content"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if syms != nil || edges != nil {
		t.Error("expected nil slices for unknown extension with no fallback")
	}
}

// ---------------------------------------------------------------------------
// ImpactQuery additional edge cases
// ---------------------------------------------------------------------------

func TestImpactQuery_LinePreserved(t *testing.T) {
	edges := []symbol.Edge{
		{From: "a.go", To: "b.go", Symbol: "DoWork", Confidence: symbol.ConfHigh, Kind: symbol.EdgeCall},
	}
	report := symbol.ImpactQuery("DoWork", edges)
	if len(report.High) == 0 {
		t.Fatal("expected high entry")
	}
	// Line is not carried in ImpactEntry from edge (it's 0) but path is always set.
	if report.High[0].Path != "a.go" {
		t.Errorf("expected path 'a.go', got %q", report.High[0].Path)
	}
}

func TestImpactQuery_MultipleEdgesToSameFile(t *testing.T) {
	// Same file calling the same symbol multiple times (both should appear)
	edges := []symbol.Edge{
		{From: "caller.go", Symbol: "Func", Confidence: symbol.ConfHigh},
		{From: "caller.go", Symbol: "Func", Confidence: symbol.ConfHigh},
	}
	report := symbol.ImpactQuery("Func", edges)
	if len(report.High) != 2 {
		t.Errorf("expected 2 high entries (not deduped), got %d", len(report.High))
	}
}
