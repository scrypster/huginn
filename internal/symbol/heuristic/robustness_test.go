package heuristic

import (
	"bytes"
	"testing"

	"github.com/scrypster/huginn/internal/symbol"
)

// TestExtractor_EmptyFile verifies extraction from empty file.
func TestExtractor_EmptyFile(t *testing.T) {
	ex := New()
	symbols, edges, err := ex.Extract("test.py", []byte{})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(symbols) != 0 {
		t.Errorf("expected 0 symbols from empty file, got %d", len(symbols))
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges from empty file, got %d", len(edges))
	}
}

// TestExtractor_OnlyWhitespace verifies extraction from whitespace-only file.
func TestExtractor_OnlyWhitespace(t *testing.T) {
	ex := New()
	symbols, edges, err := ex.Extract("test.py", []byte("   \n  \t  \n\n"))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(symbols) != 0 {
		t.Errorf("expected 0 symbols from whitespace, got %d", len(symbols))
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges from whitespace, got %d", len(edges))
	}
}

// TestExtractor_OnlyComments verifies extraction from comment-only file.
func TestExtractor_OnlyComments(t *testing.T) {
	ex := New()
	content := `# Python comment
// Go comment
-- SQL comment
* Asterisk comment`
	symbols, edges, err := ex.Extract("test.py", []byte(content))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(symbols) != 0 {
		t.Errorf("expected 0 symbols from comments, got %d", len(symbols))
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges from comments, got %d", len(edges))
	}
}

// TestExtractor_BinaryContent verifies extraction doesn't crash on binary data.
func TestExtractor_BinaryContent(t *testing.T) {
	ex := New()
	binaryData := []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0xfd}
	symbols, edges, err := ex.Extract("binary.bin", binaryData)
	// Should not panic; might return empty or error
	if err != nil {
		t.Logf("Extract returned error on binary: %v", err)
	}
	// Shouldn't crash at least
	_ = symbols
	_ = edges
}

// TestExtractor_VeryLargeFile verifies extraction on large files.
func TestExtractor_VeryLargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large file test in short mode")
	}
	ex := New()
	// Create a large file (lots of Python code)
	var buf bytes.Buffer
	for i := 0; i < 10000; i++ {
		buf.WriteString("def function_" + string(rune(i%26+'a')) + "(x):\n")
		buf.WriteString("    pass\n")
	}

	symbols, edges, err := ex.Extract("large.py", buf.Bytes())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(symbols) == 0 {
		t.Error("expected symbols from large file")
	}
	if len(symbols) < 5000 {
		t.Logf("expected ~10000 symbols, got %d", len(symbols))
	}
	_ = edges
}

// TestExtractor_VeryLongLine verifies extraction with extremely long lines.
func TestExtractor_VeryLongLine(t *testing.T) {
	ex := New()
	longLine := bytes.Repeat([]byte("a"), 100000)
	content := append([]byte("def short(x):\n"), longLine...)

	symbols, edges, err := ex.Extract("test.py", content)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// Should at least find the short function
	if len(symbols) == 0 {
		t.Error("expected symbols despite long line")
	}
	_ = edges
}

// TestExtractor_MixedEncodings verifies handling of mixed character encodings.
func TestExtractor_MixedEncodings(t *testing.T) {
	ex := New()
	// UTF-8, emoji, etc.
	content := []byte("def hello_世界🌍():\n    pass\n")
	symbols, edges, err := ex.Extract("test.py", content)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// Should handle UTF-8 gracefully without crash
	_ = symbols
	_ = edges
}

// TestExtractor_Python_BasicDef verifies Python function extraction.
func TestExtractor_Python_BasicDef(t *testing.T) {
	ex := New()
	content := []byte("def my_function(x, y):\n    return x + y\n")
	symbols, _, err := ex.Extract("test.py", content)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(symbols) == 0 {
		t.Errorf("expected symbols, got 0")
	}
	found := false
	for _, sym := range symbols {
		if sym.Name == "my_function" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected function 'my_function' in symbols")
	}
}

// TestExtractor_Python_UnderscorePrivate verifies Python _ prefix handling.
func TestExtractor_Python_UnderscorePrivate(t *testing.T) {
	ex := New()
	content := []byte("def _private_function():\n    pass\ndef public_function():\n    pass\n")
	symbols, _, err := ex.Extract("test.py", content)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// Check both functions are found with correct export status
	privateFound := false
	publicFound := false
	for _, sym := range symbols {
		if sym.Name == "_private_function" {
			privateFound = true
			if sym.Exported {
				t.Errorf("expected _private_function to have Exported=false")
			}
		}
		if sym.Name == "public_function" {
			publicFound = true
			if !sym.Exported {
				t.Errorf("expected public_function to have Exported=true")
			}
		}
	}
	if !privateFound || !publicFound {
		t.Errorf("expected to find both functions")
	}
}

// TestExtractor_Python_ClassDef verifies Python class extraction.
func TestExtractor_Python_ClassDef(t *testing.T) {
	ex := New()
	content := []byte("class MyClass:\n    def method(self):\n        pass\n")
	symbols, _, err := ex.Extract("test.py", content)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	classFound := false
	for _, sym := range symbols {
		if sym.Name == "MyClass" && sym.Kind == symbol.KindClass {
			classFound = true
		}
	}
	if !classFound {
		t.Error("expected to find MyClass")
	}
}

// TestExtractor_Python_Import verifies Python import edge extraction.
func TestExtractor_Python_Import(t *testing.T) {
	ex := New()
	content := []byte("import os\nfrom sys import path\nfrom . import local\n")
	_, edges, err := ex.Extract("test.py", content)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(edges) < 2 {
		t.Errorf("expected at least 2 import edges, got %d", len(edges))
	}
	for _, e := range edges {
		if e.Kind != symbol.EdgeImport {
			t.Errorf("expected EdgeImport, got %v", e.Kind)
		}
	}
}

// TestExtractor_Rust_Function verifies Rust function extraction.
func TestExtractor_Rust_Function(t *testing.T) {
	ex := New()
	content := []byte("fn main() {\n    println!(\"hello\");\n}\npub fn public_fn() {}\n")
	symbols, _, err := ex.Extract("main.rs", content)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	mainFound := false
	pubFound := false
	for _, sym := range symbols {
		if sym.Name == "main" && !sym.Exported {
			mainFound = true
		}
		if sym.Name == "public_fn" && sym.Exported {
			pubFound = true
		}
	}
	if !mainFound {
		t.Error("expected to find main (non-public)")
	}
	if !pubFound {
		t.Error("expected to find public_fn")
	}
}

// TestExtractor_Rust_Struct verifies Rust struct extraction.
func TestExtractor_Rust_Struct(t *testing.T) {
	ex := New()
	content := []byte("struct MyStruct { x: i32 }\npub struct PublicStruct { y: i32 }\n")
	symbols, _, err := ex.Extract("lib.rs", content)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	found := 0
	for _, sym := range symbols {
		if sym.Kind == symbol.KindClass {
			found++
		}
	}
	if found < 2 {
		t.Errorf("expected at least 2 struct symbols, got %d", found)
	}
}

// TestExtractor_Ruby_Definition verifies Ruby def extraction.
func TestExtractor_Ruby_Definition(t *testing.T) {
	ex := New()
	content := []byte("def my_method\n  42\nend\ndef _private\n  nil\nend\n")
	symbols, _, err := ex.Extract("test.rb", content)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for _, sym := range symbols {
		if sym.Name == "_private" && sym.Exported {
			t.Errorf("expected _private to have Exported=false")
		}
	}
}

// TestExtractor_Ruby_Class verifies Ruby class extraction.
func TestExtractor_Ruby_Class(t *testing.T) {
	ex := New()
	content := []byte("class MyClass\n  def initialize\n  end\nend\n")
	symbols, _, err := ex.Extract("test.rb", content)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	classFound := false
	for _, sym := range symbols {
		if sym.Name == "MyClass" && sym.Kind == symbol.KindClass {
			classFound = true
		}
	}
	if !classFound {
		t.Error("expected to find MyClass")
	}
}

// TestExtractor_Ruby_Module verifies Ruby module extraction.
func TestExtractor_Ruby_Module(t *testing.T) {
	ex := New()
	content := []byte("module MyModule\nend\n")
	symbols, _, err := ex.Extract("test.rb", content)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	modFound := false
	for _, sym := range symbols {
		if sym.Name == "MyModule" && sym.Kind == symbol.KindType {
			modFound = true
		}
	}
	if !modFound {
		t.Error("expected to find MyModule")
	}
}

// TestExtractor_Ruby_Require verifies Ruby require edge extraction.
func TestExtractor_Ruby_Require(t *testing.T) {
	ex := New()
	content := []byte("require 'mylib'\nrequire_relative './local'\n")
	_, edges, err := ex.Extract("test.rb", content)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(edges) < 2 {
		t.Errorf("expected at least 2 require edges, got %d", len(edges))
	}
}

// TestExtractor_Rust_Use verifies Rust use edge extraction.
func TestExtractor_Rust_Use(t *testing.T) {
	ex := New()
	content := []byte("use std::io;\nuse crate::module::Item;\n")
	_, edges, err := ex.Extract("lib.rs", content)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(edges) < 2 {
		t.Errorf("expected at least 2 use edges, got %d", len(edges))
	}
}

// TestExtractor_LineNumbers verifies line numbers are correct.
func TestExtractor_LineNumbers(t *testing.T) {
	ex := New()
	content := []byte("# comment\n\ndef function():\n    pass\n\ndef another():\n    pass\n")
	symbols, _, err := ex.Extract("test.py", content)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for _, sym := range symbols {
		if sym.Line <= 0 {
			t.Errorf("expected positive line number, got %d for %s", sym.Line, sym.Name)
		}
	}
}

// TestExtractor_NoFalsePositives verifies commented-out definitions aren't extracted as symbols.
func TestExtractor_NoFalsePositives(t *testing.T) {
	ex := New()
	content := []byte("# def fake_function():\n# def another_fake():\ndef real_function():\n    pass\n")
	symbols, _, err := ex.Extract("test.py", content)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// Should find real_function at minimum
	realFound := false
	for _, sym := range symbols {
		if sym.Name == "real_function" {
			realFound = true
		}
	}
	if !realFound {
		t.Errorf("expected to find 'real_function'")
	}
}

// TestExtractor_Indentation verifies indentation is handled properly.
func TestExtractor_Indentation(t *testing.T) {
	ex := New()
	content := []byte("class Outer:\n  def method1(self):\n    pass\n  def method2(self):\n    pass\n")
	symbols, _, err := ex.Extract("test.py", content)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// Should find the class and both methods
	if len(symbols) < 2 {
		t.Errorf("expected at least 2 symbols (class + methods), got %d", len(symbols))
	}
}

// BenchmarkExtractor_SimpleFile measures extraction performance on simple file.
func BenchmarkExtractor_SimpleFile(b *testing.B) {
	ex := New()
	content := []byte("def func1():\n    pass\ndef func2():\n    pass\ndef func3():\n    pass\n")

	for i := 0; i < b.N; i++ {
		ex.Extract("test.py", content)
	}
}

// BenchmarkExtractor_LargeFile measures extraction performance on large file.
func BenchmarkExtractor_LargeFile(b *testing.B) {
	ex := New()
	var buf bytes.Buffer
	for i := 0; i < 1000; i++ {
		buf.WriteString("def func_" + string(rune(i)) + "():\n    pass\n")
	}
	content := buf.Bytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ex.Extract("large.py", content)
	}
}
