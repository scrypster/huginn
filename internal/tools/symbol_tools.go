package tools

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/symbol/lsp"
)

const notConfiguredMessage = `LSP not available for this language.

Huginn auto-detects LSP servers on startup when they are in your PATH:
  - Go:        gopls
  - TypeScript/JS: typescript-language-server
  - Rust:      rust-analyzer
  - Python:    pylsp or pyright-langserver

Install the appropriate server and restart Huginn. No manual configuration needed.`

// LSPManager is the interface for LSP-backed symbol resolution.
type LSPManager interface {
	Definition(uri string, line, column int) ([]lsp.Location, error)
	Symbols(query string) ([]lsp.SymbolInformation, error)
}

// --- find_definition ---

type FindDefinitionTool struct {
	root string
	mgr  LSPManager
}

func NewFindDefinitionTool(root string, mgr LSPManager) *FindDefinitionTool {
	return &FindDefinitionTool{root: root, mgr: mgr}
}

func (t *FindDefinitionTool) Name() string {
	return "find_definition"
}

func (t *FindDefinitionTool) Permission() PermissionLevel {
	return PermRead
}

func (t *FindDefinitionTool) Description() string {
	return "Find the definition of a symbol at the given file position using the LSP."
}

func (t *FindDefinitionTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "find_definition",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"path", "line", "column"},
				Properties: map[string]backend.ToolProperty{
					"path": {
						Type:        "string",
						Description: "File path relative to project root",
					},
					"line": {
						Type:        "integer",
						Description: "1-indexed line number",
					},
					"column": {
						Type:        "integer",
						Description: "1-indexed column",
					},
				},
			},
		},
	}
}

func (t *FindDefinitionTool) Execute(_ context.Context, args map[string]any) ToolResult {
	path, line, column, err := extractPositionArgs(args)
	if err != nil {
		return ToolResult{IsError: true, Error: "find_definition: " + err.Error()}
	}
	uri := pathToFileURI(t.root, path)
	locs, err := t.mgr.Definition(uri, line, column)
	if err != nil {
		if errors.Is(err, lsp.ErrNotConfigured) {
			return ToolResult{Output: notConfiguredMessage}
		}
		return ToolResult{IsError: true, Error: fmt.Sprintf("find_definition: %v", err)}
	}
	return ToolResult{Output: formatLocations("Definition", locs, t.root)}
}

// --- list_symbols ---

type ListSymbolsTool struct {
	root string
	mgr  LSPManager
}

func NewListSymbolsTool(root string, mgr LSPManager) *ListSymbolsTool {
	return &ListSymbolsTool{root: root, mgr: mgr}
}

func (t *ListSymbolsTool) Name() string {
	return "list_symbols"
}

func (t *ListSymbolsTool) Permission() PermissionLevel {
	return PermRead
}

func (t *ListSymbolsTool) Description() string {
	return "List workspace symbols matching a query using the LSP."
}

func (t *ListSymbolsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "list_symbols",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"query"},
				Properties: map[string]backend.ToolProperty{
					"query": {
						Type:        "string",
						Description: "Symbol name or prefix",
					},
				},
			},
		},
	}
}

func (t *ListSymbolsTool) Execute(_ context.Context, args map[string]any) ToolResult {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return ToolResult{IsError: true, Error: "list_symbols: 'query' required"}
	}
	syms, err := t.mgr.Symbols(query)
	if err != nil {
		if errors.Is(err, lsp.ErrNotConfigured) {
			return ToolResult{Output: notConfiguredMessage}
		}
		return ToolResult{IsError: true, Error: fmt.Sprintf("list_symbols: %v", err)}
	}
	if len(syms) == 0 {
		return ToolResult{Output: fmt.Sprintf("No symbols found matching %q.", query)}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Symbols matching %q (%d found):\n", query, len(syms))
	for _, s := range syms {
		rel := uriToRelPath(t.root, s.Location.URI)
		line := s.Location.Range.Start.Line + 1
		fmt.Fprintf(&sb, "  %s %s (%s:%d)\n", lspKindName(s.Kind), s.Name, rel, line)
	}
	return ToolResult{Output: sb.String()}
}

// --- noopLSPManager ---

type noopLSPManager struct{}

func (n *noopLSPManager) Definition(string, int, int) ([]lsp.Location, error) {
	return nil, lsp.ErrNotConfigured
}

func (n *noopLSPManager) Symbols(string) ([]lsp.SymbolInformation, error) {
	return nil, lsp.ErrNotConfigured
}

// RegisterLSPTools registers symbol tools. Pass nil managers to use noop (graceful not-configured).
func RegisterLSPTools(reg *Registry, sandboxRoot string, managers map[string]LSPManager) {
	var mgr LSPManager = &noopLSPManager{}
	if managers != nil {
		if m, ok := managers["go"]; ok {
			mgr = m
		} else {
			for _, m := range managers {
				mgr = m
				break
			}
		}
	}
	reg.Register(NewFindDefinitionTool(sandboxRoot, mgr))
	reg.Register(NewListSymbolsTool(sandboxRoot, mgr))
}

// helpers

func extractPositionArgs(args map[string]any) (path string, line, column int, err error) {
	p, ok := args["path"].(string)
	if !ok || p == "" {
		return "", 0, 0, fmt.Errorf("'path' required")
	}
	l, err2 := toInt(args["line"])
	if err2 != nil || l <= 0 {
		return "", 0, 0, fmt.Errorf("'line' must be positive integer")
	}
	c, err3 := toInt(args["column"])
	if err3 != nil || c <= 0 {
		return "", 0, 0, fmt.Errorf("'column' must be positive integer")
	}
	return p, l, c, nil
}

func toInt(v any) (int, error) {
	switch n := v.(type) {
	case float64:
		return int(n), nil
	case int:
		return n, nil
	case int64:
		return int(n), nil
	}
	return 0, fmt.Errorf("not an integer: %T", v)
}

func pathToFileURI(root, relPath string) string {
	abs := filepath.Join(root, relPath)
	return "file://" + filepath.ToSlash(abs)
}

func uriToRelPath(root, uri string) string {
	abs := strings.TrimPrefix(uri, "file://")
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return abs
	}
	return rel
}

func formatLocations(label string, locs []lsp.Location, root string) string {
	if len(locs) == 0 {
		return fmt.Sprintf("No %s found.", strings.ToLower(label))
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s (%d):\n", label, len(locs))
	for _, loc := range locs {
		rel := uriToRelPath(root, loc.URI)
		fmt.Fprintf(&sb, "  %s:%d:%d\n", rel, loc.Range.Start.Line+1, loc.Range.Start.Character+1)
	}
	return sb.String()
}

func lspKindName(kind int) string {
	names := map[int]string{
		5:  "Class",
		6:  "Method",
		12: "Function",
		13: "Variable",
		11: "Interface",
		23: "Struct",
		14: "Constant",
	}
	if n, ok := names[kind]; ok {
		return n
	}
	return "Symbol"
}
