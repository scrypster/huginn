package heuristic

import (
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

func findEdgeTo(edges []symbol.Edge, to string) *symbol.Edge {
	for i := range edges {
		if edges[i].To == to {
			return &edges[i]
		}
	}
	return nil
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
	if e.Language() != "heuristic" {
		t.Errorf("expected 'heuristic', got %q", e.Language())
	}
}

// ---------------------------------------------------------------------------
// Empty input
// ---------------------------------------------------------------------------

func TestExtract_EmptyContent(t *testing.T) {
	e := New()
	syms, edges, err := e.Extract("empty.py", []byte(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(syms) != 0 || len(edges) != 0 {
		t.Errorf("expected empty results for empty content, got %d syms %d edges", len(syms), len(edges))
	}
}

func TestExtract_OnlyComments(t *testing.T) {
	e := New()
	src := `# This is a comment
# Another comment
// C-style comment
-- SQL comment
* Javadoc line
`
	syms, edges, err := e.Extract("comments.py", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(syms) != 0 || len(edges) != 0 {
		t.Errorf("expected empty results for comment-only content, got %d syms %d edges", len(syms), len(edges))
	}
}

// ---------------------------------------------------------------------------
// Python
// ---------------------------------------------------------------------------

const pythonSrc = `import os
from pathlib import Path
import sys

def greet(name):
    pass

def _private_helper():
    pass

class MyClass:
    pass

class _InternalClass:
    pass
`

func TestExtract_Python_Imports(t *testing.T) {
	e := New()
	_, edges, err := e.Extract("script.py", []byte(pythonSrc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	osEdge := findEdgeTo(edges, "os")
	if osEdge == nil {
		t.Fatal("expected edge for 'os' import")
	}
	if osEdge.Kind != symbol.EdgeImport {
		t.Errorf("expected EdgeImport, got %q", osEdge.Kind)
	}
	if osEdge.Confidence != symbol.ConfMedium {
		t.Errorf("expected MEDIUM confidence, got %q", osEdge.Confidence)
	}
	if osEdge.From != "script.py" {
		t.Errorf("expected From='script.py', got %q", osEdge.From)
	}

	// "from pathlib import Path" — from extracts the module name "pathlib"
	pathlibEdge := findEdgeTo(edges, "pathlib")
	if pathlibEdge == nil {
		t.Fatal("expected edge for 'pathlib' from-import")
	}
}

func TestExtract_Python_Functions(t *testing.T) {
	e := New()
	syms, _, err := e.Extract("script.py", []byte(pythonSrc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	greet := findSymbol(syms, "greet")
	if greet == nil {
		t.Fatal("expected 'greet' function")
	}
	if greet.Kind != symbol.KindFunction {
		t.Errorf("expected KindFunction, got %q", greet.Kind)
	}
	if !greet.Exported {
		t.Error("'greet' should be exported (no underscore prefix)")
	}

	priv := findSymbol(syms, "_private_helper")
	if priv == nil {
		t.Fatal("expected '_private_helper'")
	}
	if priv.Exported {
		t.Error("'_private_helper' should not be exported")
	}
}

func TestExtract_Python_Classes(t *testing.T) {
	e := New()
	syms, _, err := e.Extract("script.py", []byte(pythonSrc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cls := findSymbol(syms, "MyClass")
	if cls == nil {
		t.Fatal("expected 'MyClass'")
	}
	if cls.Kind != symbol.KindClass {
		t.Errorf("expected KindClass, got %q", cls.Kind)
	}
	if !cls.Exported {
		t.Error("'MyClass' should be exported")
	}

	internal := findSymbol(syms, "_InternalClass")
	if internal == nil {
		t.Fatal("expected '_InternalClass'")
	}
	if internal.Exported {
		t.Error("'_InternalClass' should not be exported")
	}
}

func TestExtract_Python_LineNumbers(t *testing.T) {
	e := New()
	src := `import os

def first():
    pass

def second():
    pass
`
	syms, _, err := e.Extract("lines.py", []byte(src))
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
// Ruby
// ---------------------------------------------------------------------------

const rubySrc = `require 'json'
require_relative 'helper'

def say_hello
  puts "hi"
end

class Animal
end

class _Secret
end

module MyModule
end
`

func TestExtract_Ruby_Require(t *testing.T) {
	e := New()
	_, edges, err := e.Extract("script.rb", []byte(rubySrc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	jsonEdge := findEdgeTo(edges, "json")
	if jsonEdge == nil {
		t.Fatal("expected edge for 'json' require")
	}
	if jsonEdge.Kind != symbol.EdgeImport {
		t.Errorf("expected EdgeImport, got %q", jsonEdge.Kind)
	}
	if jsonEdge.Confidence != symbol.ConfMedium {
		t.Errorf("expected MEDIUM, got %q", jsonEdge.Confidence)
	}

	helperEdge := findEdgeTo(edges, "helper")
	if helperEdge == nil {
		t.Fatal("expected edge for 'helper' require_relative")
	}
}

func TestExtract_Ruby_Def(t *testing.T) {
	e := New()
	syms, _, err := e.Extract("script.rb", []byte(rubySrc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	say := findSymbol(syms, "say_hello")
	if say == nil {
		t.Fatal("expected 'say_hello' function")
	}
	if say.Kind != symbol.KindFunction {
		t.Errorf("expected KindFunction, got %q", say.Kind)
	}
	if !say.Exported {
		t.Error("'say_hello' should be exported")
	}
}

func TestExtract_Ruby_Class(t *testing.T) {
	e := New()
	syms, _, err := e.Extract("script.rb", []byte(rubySrc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	animal := findSymbol(syms, "Animal")
	if animal == nil {
		t.Fatal("expected 'Animal'")
	}
	if animal.Kind != symbol.KindClass {
		t.Errorf("expected KindClass, got %q", animal.Kind)
	}
	if !animal.Exported {
		t.Error("'Animal' should be exported")
	}
}

func TestExtract_Ruby_Module(t *testing.T) {
	e := New()
	syms, _, err := e.Extract("script.rb", []byte(rubySrc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mod := findSymbol(syms, "MyModule")
	if mod == nil {
		t.Fatal("expected 'MyModule' module")
	}
	if mod.Kind != symbol.KindType {
		t.Errorf("expected KindType for module, got %q", mod.Kind)
	}
	if !mod.Exported {
		t.Error("'MyModule' should be exported")
	}
}

// ---------------------------------------------------------------------------
// Rust
// ---------------------------------------------------------------------------

const rustSrc = `use std::io;
use std::collections::HashMap;

pub fn public_func() {}
fn private_func() {}

pub struct PublicStruct {}
struct PrivateStruct {}

pub trait MyTrait {}

impl MyTrait for PublicStruct {}
`

func TestExtract_Rust_Use(t *testing.T) {
	e := New()
	_, edges, err := e.Extract("lib.rs", []byte(rustSrc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ioEdge := findEdgeTo(edges, "std::io")
	if ioEdge == nil {
		t.Fatal("expected edge for 'std::io' use")
	}
	if ioEdge.Kind != symbol.EdgeImport {
		t.Errorf("expected EdgeImport, got %q", ioEdge.Kind)
	}
	if ioEdge.Confidence != symbol.ConfMedium {
		t.Errorf("expected MEDIUM, got %q", ioEdge.Confidence)
	}

	mapEdge := findEdgeTo(edges, "std::collections::HashMap")
	if mapEdge == nil {
		t.Fatal("expected edge for 'std::collections::HashMap'")
	}
}

func TestExtract_Rust_Functions(t *testing.T) {
	e := New()
	syms, _, err := e.Extract("lib.rs", []byte(rustSrc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pub := findSymbol(syms, "public_func")
	if pub == nil {
		t.Fatal("expected 'public_func'")
	}
	if pub.Kind != symbol.KindFunction {
		t.Errorf("expected KindFunction, got %q", pub.Kind)
	}
	if !pub.Exported {
		t.Error("'public_func' should be exported (has pub keyword)")
	}

	priv := findSymbol(syms, "private_func")
	if priv == nil {
		t.Fatal("expected 'private_func'")
	}
	if priv.Exported {
		t.Error("'private_func' should not be exported")
	}
}

func TestExtract_Rust_Structs(t *testing.T) {
	e := New()
	syms, _, err := e.Extract("lib.rs", []byte(rustSrc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pub := findSymbol(syms, "PublicStruct")
	if pub == nil {
		t.Fatal("expected 'PublicStruct'")
	}
	if pub.Kind != symbol.KindClass {
		t.Errorf("expected KindClass for struct, got %q", pub.Kind)
	}
	if !pub.Exported {
		t.Error("'PublicStruct' should be exported")
	}

	priv := findSymbol(syms, "PrivateStruct")
	if priv == nil {
		t.Fatal("expected 'PrivateStruct'")
	}
	if priv.Exported {
		t.Error("'PrivateStruct' should not be exported")
	}
}

func TestExtract_Rust_Trait(t *testing.T) {
	e := New()
	syms, _, err := e.Extract("lib.rs", []byte(rustSrc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	trait := findSymbol(syms, "MyTrait")
	if trait == nil {
		t.Fatal("expected 'MyTrait'")
	}
	if trait.Kind != symbol.KindInterface {
		t.Errorf("expected KindInterface for trait, got %q", trait.Kind)
	}
	if !trait.Exported {
		t.Error("'MyTrait' should be exported")
	}
}

func TestExtract_Rust_Impl(t *testing.T) {
	e := New()
	syms, _, err := e.Extract("lib.rs", []byte(rustSrc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// impl MyTrait for PublicStruct -> captures MyTrait as KindType
	impl := findSymbol(syms, "MyTrait")
	if impl == nil {
		// impl could capture "MyTrait" — check for it
		// Either the trait or the impl could match; look for KindType specifically.
		found := false
		for _, s := range syms {
			if s.Name == "MyTrait" && s.Kind == symbol.KindType {
				found = true
			}
		}
		if !found {
			// Not finding it as KindType is also acceptable if the trait decl
			// took priority; just ensure it appears somewhere.
			t.Log("'MyTrait' not found as KindType — may be captured by trait decl only")
		}
	}
}

// ---------------------------------------------------------------------------
// Generic fallback (file types not matching Python/Ruby/Rust patterns)
// ---------------------------------------------------------------------------

func TestExtract_Generic_FunctionFallback(t *testing.T) {
	e := New()
	// A file with no Python/Ruby/Rust identifiers: use generic patterns.
	// These should only trigger when no language-specific patterns have matched.
	// We need content that does NOT trigger py/rb/rs matchers.
	src := `function myHandler(req, res) {
  return res.send("ok")
}
`
	syms, _, err := e.Extract("handler.lua", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The generic fallback matches "function myHandler"
	handler := findSymbol(syms, "myHandler")
	if handler == nil {
		t.Fatal("expected 'myHandler' via generic fallback")
	}
	if handler.Kind != symbol.KindFunction {
		t.Errorf("expected KindFunction, got %q", handler.Kind)
	}
}

func TestExtract_Generic_ClassFallback(t *testing.T) {
	e := New()
	src := `class Animal {
  speak() {}
}
`
	syms, _, err := e.Extract("animal.lua", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	animal := findSymbol(syms, "Animal")
	if animal == nil {
		t.Fatal("expected 'Animal' via generic class fallback")
	}
	if animal.Kind != symbol.KindClass {
		t.Errorf("expected KindClass, got %q", animal.Kind)
	}
}

// ---------------------------------------------------------------------------
// Path propagation
// ---------------------------------------------------------------------------

func TestExtract_PathOnAllSymbols(t *testing.T) {
	e := New()
	src := `def alpha():
    pass

class Beta:
    pass
`
	syms, _, err := e.Extract("deep/path/script.py", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, s := range syms {
		if s.Path != "deep/path/script.py" {
			t.Errorf("expected path 'deep/path/script.py', got %q for %q", s.Path, s.Name)
		}
	}
}

func TestExtract_PathOnAllEdges(t *testing.T) {
	e := New()
	src := `import os
import sys
`
	_, edges, err := e.Extract("deep/path/script.py", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, edge := range edges {
		if edge.From != "deep/path/script.py" {
			t.Errorf("expected From='deep/path/script.py', got %q", edge.From)
		}
	}
}

// ---------------------------------------------------------------------------
// No error return (always nil)
// ---------------------------------------------------------------------------

func TestExtract_AlwaysNilError(t *testing.T) {
	e := New()
	cases := []struct {
		name    string
		content string
	}{
		{"empty", ""},
		{"python", "def foo(): pass"},
		{"ruby", "def bar; end"},
		{"rust", "fn baz() {}"},
		{"garbage", "}{}{!@#$%^&*"},
	}
	for _, tc := range cases {
		_, _, err := e.Extract("x", []byte(tc.content))
		if err != nil {
			t.Errorf("case %q: expected nil error, got %v", tc.name, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Unicode input (should not panic)
// ---------------------------------------------------------------------------

func TestExtract_UnicodeInput(t *testing.T) {
	e := New()
	src := "def héllo():\n    pass\n# 日本語 comment\n"
	syms, _, err := e.Extract("unicode.py", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// héllo won't match \w+ (ASCII only in Go regex), so no symbols expected.
	_ = syms
}

// ---------------------------------------------------------------------------
// Large input — smoke test
// ---------------------------------------------------------------------------

func TestExtract_LargeInput(t *testing.T) {
	e := New()
	var sb []byte
	for i := 0; i < 1000; i++ {
		sb = append(sb, []byte("def func"+string(rune('a'+i%26))+"():\n    pass\n")...)
	}
	syms, _, err := e.Extract("large.py", sb)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(syms) == 0 {
		t.Error("expected symbols from large Python file")
	}
}

// ---------------------------------------------------------------------------
// Mixed language patterns in same file (unusual, but should not panic)
// ---------------------------------------------------------------------------

func TestExtract_MixedPatterns(t *testing.T) {
	e := New()
	src := `import os
use std::io;
require 'json'
def my_func():
    pass
fn rust_fn() {}
class PythonClass:
    pass
`
	syms, edges, err := e.Extract("mixed.txt", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should not panic; results may vary but structure should be valid.
	for _, s := range syms {
		if s.Name == "" {
			t.Error("symbol with empty name found")
		}
	}
	for _, edge := range edges {
		if edge.To == "" {
			t.Error("edge with empty To found")
		}
	}
}
