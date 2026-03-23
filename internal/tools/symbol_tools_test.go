package tools_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/symbol/lsp"
	"github.com/scrypster/huginn/internal/tools"
)

type mockLSPMgr struct {
	defs []lsp.Location
	syms []lsp.SymbolInformation
	err  error
}

func (m *mockLSPMgr) Definition(uri string, line, col int) ([]lsp.Location, error) {
	return m.defs, m.err
}

func (m *mockLSPMgr) Symbols(query string) ([]lsp.SymbolInformation, error) {
	return m.syms, m.err
}

func TestFindDefinitionTool_NotConfigured_HelpfulMessage(t *testing.T) {
	mock := &mockLSPMgr{err: lsp.ErrNotConfigured}
	tool := tools.NewFindDefinitionTool("/project", mock)
	result := tool.Execute(context.Background(), map[string]any{
		"path":   "main.go",
		"line":   float64(1),
		"column": float64(1),
	})
	if result.IsError {
		t.Errorf("expected helpful message, not IsError")
	}
	if !strings.Contains(result.Output, "LSP") {
		t.Errorf("expected LSP in message, got: %s", result.Output)
	}
}

func TestFindDefinitionTool_Success(t *testing.T) {
	mock := &mockLSPMgr{
		defs: []lsp.Location{
			{
				URI: "file:///project/main.go",
				Range: lsp.Range{
					Start: lsp.Position{Line: 9},
				},
			},
		},
	}
	tool := tools.NewFindDefinitionTool("/project", mock)
	result := tool.Execute(context.Background(), map[string]any{
		"path":   "main.go",
		"line":   float64(42),
		"column": float64(15),
	})
	if result.IsError {
		t.Fatalf("error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "main.go") {
		t.Errorf("expected main.go in output")
	}
}

func TestListSymbolsTool_Success(t *testing.T) {
	mock := &mockLSPMgr{
		syms: []lsp.SymbolInformation{
			{
				Name: "Handler",
				Kind: 12,
				Location: lsp.Location{
					URI: "file:///project/server.go",
					Range: lsp.Range{
						Start: lsp.Position{Line: 20},
					},
				},
			},
		},
	}
	tool := tools.NewListSymbolsTool("/project", mock)
	result := tool.Execute(context.Background(), map[string]any{
		"query": "Handler",
	})
	if result.IsError {
		t.Fatalf("error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "Handler") {
		t.Errorf("expected Handler in output")
	}
}

func TestFindDefinitionTool_MissingArgs(t *testing.T) {
	mock := &mockLSPMgr{}
	tool := tools.NewFindDefinitionTool("/project", mock)
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for missing args")
	}
}

func TestFindDefinitionTool_Schema(t *testing.T) {
	mock := &mockLSPMgr{}
	tool := tools.NewFindDefinitionTool("/project", mock)
	if tool.Name() != "find_definition" {
		t.Errorf("expected find_definition, got %s", tool.Name())
	}
	if tool.Permission() != tools.PermRead {
		t.Error("expected PermRead")
	}
}

func TestListSymbolsTool_Schema(t *testing.T) {
	mock := &mockLSPMgr{}
	tool := tools.NewListSymbolsTool("/project", mock)
	if tool.Name() != "list_symbols" {
		t.Errorf("expected list_symbols, got %s", tool.Name())
	}
	if tool.Permission() != tools.PermRead {
		t.Error("expected PermRead")
	}
}

func TestListSymbolsTool_MissingQuery(t *testing.T) {
	mock := &mockLSPMgr{}
	tool := tools.NewListSymbolsTool("/project", mock)
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for missing query")
	}
}

func TestListSymbolsTool_NoResults(t *testing.T) {
	mock := &mockLSPMgr{syms: []lsp.SymbolInformation{}}
	tool := tools.NewListSymbolsTool("/project", mock)
	result := tool.Execute(context.Background(), map[string]any{"query": "NotFound"})
	if result.IsError {
		t.Errorf("expected success message, got error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "No symbols found") {
		t.Errorf("expected 'No symbols found' message, got: %s", result.Output)
	}
}

func TestFindDefinitionTool_InvalidLine(t *testing.T) {
	mock := &mockLSPMgr{}
	tool := tools.NewFindDefinitionTool("/project", mock)
	result := tool.Execute(context.Background(), map[string]any{
		"path":   "main.go",
		"line":   float64(-1),
		"column": float64(1),
	})
	if !result.IsError {
		t.Error("expected error for negative line")
	}
}

func TestFindDefinitionTool_NoResults(t *testing.T) {
	mock := &mockLSPMgr{defs: []lsp.Location{}}
	tool := tools.NewFindDefinitionTool("/project", mock)
	result := tool.Execute(context.Background(), map[string]any{
		"path":   "main.go",
		"line":   float64(1),
		"column": float64(1),
	})
	if result.IsError {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "No definition found") {
		t.Errorf("expected 'No definition found' message, got: %s", result.Output)
	}
}

func TestFindDefinitionTool_Error(t *testing.T) {
	mock := &mockLSPMgr{err: errors.New("LSP error")}
	tool := tools.NewFindDefinitionTool("/project", mock)
	result := tool.Execute(context.Background(), map[string]any{
		"path":   "main.go",
		"line":   float64(1),
		"column": float64(1),
	})
	if !result.IsError {
		t.Error("expected error")
	}
	if !strings.Contains(result.Error, "LSP error") {
		t.Errorf("error should mention LSP error, got %q", result.Error)
	}
}

func TestFindDefinitionTool_MissingPath(t *testing.T) {
	mock := &mockLSPMgr{}
	tool := tools.NewFindDefinitionTool("/project", mock)
	result := tool.Execute(context.Background(), map[string]any{
		"line":   float64(1),
		"column": float64(1),
	})
	if !result.IsError {
		t.Error("expected error for missing path")
	}
}

func TestFindDefinitionTool_MissingColumn(t *testing.T) {
	mock := &mockLSPMgr{}
	tool := tools.NewFindDefinitionTool("/project", mock)
	result := tool.Execute(context.Background(), map[string]any{
		"path": "main.go",
		"line": float64(1),
	})
	if !result.IsError {
		t.Error("expected error for missing column")
	}
}

func TestFindDefinitionTool_InvalidLineType(t *testing.T) {
	mock := &mockLSPMgr{}
	tool := tools.NewFindDefinitionTool("/project", mock)
	result := tool.Execute(context.Background(), map[string]any{
		"path":   "main.go",
		"line":   "not_a_number",
		"column": float64(1),
	})
	if !result.IsError {
		t.Error("expected error for invalid line type")
	}
}

func TestFindDefinitionTool_InvalidColumn(t *testing.T) {
	mock := &mockLSPMgr{}
	tool := tools.NewFindDefinitionTool("/project", mock)
	result := tool.Execute(context.Background(), map[string]any{
		"path":   "main.go",
		"line":   float64(1),
		"column": "not_a_number",
	})
	if !result.IsError {
		t.Error("expected error for invalid column type")
	}
}

func TestListSymbolsTool_NotConfigured(t *testing.T) {
	mock := &mockLSPMgr{err: lsp.ErrNotConfigured}
	tool := tools.NewListSymbolsTool("/project", mock)
	result := tool.Execute(context.Background(), map[string]any{"query": "Foo"})
	if result.IsError {
		t.Errorf("expected helpful message, got error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "LSP") {
		t.Errorf("expected LSP in message")
	}
}

func TestListSymbolsTool_Error(t *testing.T) {
	mock := &mockLSPMgr{err: errors.New("symbol error")}
	tool := tools.NewListSymbolsTool("/project", mock)
	result := tool.Execute(context.Background(), map[string]any{"query": "Foo"})
	if !result.IsError {
		t.Error("expected error")
	}
	if !strings.Contains(result.Error, "symbol error") {
		t.Errorf("expected 'symbol error', got %q", result.Error)
	}
}

func TestListSymbolsTool_EmptyQuery(t *testing.T) {
	mock := &mockLSPMgr{}
	tool := tools.NewListSymbolsTool("/project", mock)
	result := tool.Execute(context.Background(), map[string]any{"query": ""})
	if !result.IsError {
		t.Error("expected error for empty query")
	}
}

func TestRegisterLSPTools_NoManagers(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterLSPTools(reg, "/tmp", nil)

	if _, ok := reg.Get("find_definition"); !ok {
		t.Error("find_definition not registered")
	}
	if _, ok := reg.Get("list_symbols"); !ok {
		t.Error("list_symbols not registered")
	}
}

func TestRegisterLSPTools_WithGoManager(t *testing.T) {
	reg := tools.NewRegistry()
	mgr := &mockLSPMgr{}
	tools.RegisterLSPTools(reg, "/tmp", map[string]tools.LSPManager{"go": mgr})

	if _, ok := reg.Get("find_definition"); !ok {
		t.Error("find_definition not registered")
	}
}

func TestRegisterLSPTools_WithNonGoManager(t *testing.T) {
	reg := tools.NewRegistry()
	mgr := &mockLSPMgr{}
	tools.RegisterLSPTools(reg, "/tmp", map[string]tools.LSPManager{"python": mgr})

	if _, ok := reg.Get("find_definition"); !ok {
		t.Error("find_definition not registered")
	}
}
