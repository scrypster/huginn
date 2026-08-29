package tools_test

// Real-gopls end-to-end coverage for find_definition/list_symbols. Skips
// when gopls isn't on PATH (e.g. CI without it installed) rather than
// failing — this is a "does the real thing work" check, not a mock unit
// test (see symbol_tools_test.go for those).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/symbol/lsp"
	"github.com/scrypster/huginn/internal/tools"
)

func TestFindDefinitionAndListSymbols_RealGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not on PATH; skipping real-LSP integration test")
	}

	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module scratch\n\ngo 1.21\n")
	// No leading indentation on the call site keeps the column offset
	// used below (column 1) trivially correct.
	mustWrite(t, filepath.Join(root, "main.go"), "package main\n\nfunc Greet() string { return \"hi\" }\n\nfunc main() {\nGreet()\n}\n")

	cfg := lsp.Detect("go")
	if cfg.Command == "" {
		t.Fatal("lsp.Detect(\"go\") found no gopls despite exec.LookPath succeeding")
	}
	mgr := lsp.NewManager("go", cfg)
	if err := mgr.Start(root); err != nil {
		t.Fatalf("gopls Start: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop() })

	reg := tools.NewRegistry()
	tools.RegisterLSPTools(reg, root, map[string]tools.LSPManager{"go": mgr})

	findDef, ok := reg.Get("find_definition")
	if !ok {
		t.Fatal("find_definition not registered")
	}
	// Line 6 ("Greet()"), column 1: the call site.
	result := findDef.Execute(context.Background(), map[string]any{
		"path":   "main.go",
		"line":   float64(6),
		"column": float64(1),
	})
	if result.IsError {
		t.Fatalf("find_definition errored: %s", result.Error)
	}
	if !strings.Contains(result.Output, "main.go:3") {
		t.Errorf("expected definition to point at main.go:3 (func Greet), got:\n%s", result.Output)
	}

	listSyms, ok := reg.Get("list_symbols")
	if !ok {
		t.Fatal("list_symbols not registered")
	}
	symResult := listSyms.Execute(context.Background(), map[string]any{"query": "Greet"})
	if symResult.IsError {
		t.Fatalf("list_symbols errored: %s", symResult.Error)
	}
	if !strings.Contains(symResult.Output, "Greet") {
		t.Errorf("expected Greet in list_symbols output, got:\n%s", symResult.Output)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
