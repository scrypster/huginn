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

// extToLanguage maps file extensions to the language key used in the
// managers map passed to RegisterLSPTools (mirrors init_tools.go's
// langExtensions — duplicated here because internal/tools cannot import
// package main). Used to route a call to the LSP server that actually
// speaks the target file's language, instead of an arbitrary one.
var extToLanguage = map[string]string{
	".go":  "go",
	".ts":  "typescript",
	".tsx": "typescript",
	".js":  "javascript",
	".jsx": "javascript",
	".mjs": "javascript",
	".cjs": "javascript",
	".rs":  "rust",
	".py":  "python",
	".rb":  "ruby",
	".php": "php",
}

// noManagerMessage is returned (as a normal, non-error Output — same shape
// as notConfiguredMessage) when path's extension either isn't a recognized
// language or has no LSP manager registered for it. This is the "clean
// unavailable" response a caller should get instead of a wrong-language
// server silently returning garbage.
func noManagerMessage(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		ext = "(no extension)"
	}
	return fmt.Sprintf("No language server configured for %s files.\n\n%s", ext, notConfiguredMessage)
}

// --- find_definition ---

type FindDefinitionTool struct {
	root string
	mgr  LSPManager
	// managers, when non-empty, enables extension-based routing: the
	// manager used for a call is picked by the target file's language, not
	// a single fixed default. Set only via RegisterLSPTools; direct
	// construction (NewFindDefinitionTool) keeps the single-manager
	// behavior for backward compatibility.
	managers map[string]LSPManager
}

func NewFindDefinitionTool(root string, mgr LSPManager) *FindDefinitionTool {
	return &FindDefinitionTool{root: root, mgr: mgr}
}

// resolveManager picks the LSPManager for path's language. With no
// managers map (direct construction / a single registered language), it
// falls back to the tool's default mgr unconditionally — unchanged
// behavior. With a managers map, it routes strictly by extension and
// returns ok=false (with a clean message) when nothing matches, rather
// than silently falling back to some other language's server.
func resolveManagerForPath(managers map[string]LSPManager, fallback LSPManager, path string) (mgr LSPManager, unavailableMsg string) {
	if len(managers) == 0 {
		return fallback, ""
	}
	lang, ok := extToLanguage[strings.ToLower(filepath.Ext(path))]
	if !ok {
		return nil, noManagerMessage(path)
	}
	mgr, ok = managers[lang]
	if !ok {
		return nil, noManagerMessage(path)
	}
	return mgr, ""
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
	mgr, unavailable := resolveManagerForPath(t.managers, t.mgr, path)
	if unavailable != "" {
		return ToolResult{Output: unavailable}
	}
	uri := pathToFileURI(t.root, path)
	locs, err := mgr.Definition(uri, line, column)
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
	// managers, when non-empty, makes list_symbols query every registered
	// language server and merge the results, instead of one arbitrarily
	// chosen manager returning results for only one language. Set only via
	// RegisterLSPTools; see FindDefinitionTool.managers.
	managers map[string]LSPManager
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
	syms, err := t.symbols(query)
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

// symbols queries every registered language server for query and merges
// the results. list_symbols has no file path to route by (unlike
// find_definition), so instead of picking one arbitrary manager (which
// would silently hide every other language's symbols) it fans out to all
// of them. With no managers map (direct construction) it falls back to the
// single default manager, unchanged from before. If every manager reports
// ErrNotConfigured, that error is returned so the caller gets the usual
// helpful "LSP not available" message rather than an empty result.
func (t *ListSymbolsTool) symbols(query string) ([]lsp.SymbolInformation, error) {
	if len(t.managers) == 0 {
		return t.mgr.Symbols(query)
	}
	var out []lsp.SymbolInformation
	sawConfigured := false
	var firstErr error
	for _, mgr := range t.managers {
		syms, err := mgr.Symbols(query)
		if err != nil {
			if errors.Is(err, lsp.ErrNotConfigured) {
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		sawConfigured = true
		out = append(out, syms...)
	}
	if !sawConfigured {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, lsp.ErrNotConfigured
	}
	return out, nil
}

// --- noopLSPManager ---

type noopLSPManager struct{}

func (n *noopLSPManager) Definition(string, int, int) ([]lsp.Location, error) {
	return nil, lsp.ErrNotConfigured
}

func (n *noopLSPManager) Symbols(string) ([]lsp.SymbolInformation, error) {
	return nil, lsp.ErrNotConfigured
}

// RegisterLSPTools registers symbol tools. Pass nil/empty managers to use
// noop (graceful not-configured) everywhere. With two or more managers,
// find_definition routes each call by the target file's extension (see
// resolveManagerForPath) instead of collapsing every language onto one
// arbitrarily-chosen server; list_symbols fans a query out to all of them
// (see ListSymbolsTool.symbols).
func RegisterLSPTools(reg *Registry, sandboxRoot string, managers map[string]LSPManager) {
	var mgr LSPManager = &noopLSPManager{}
	if len(managers) == 1 {
		for _, m := range managers {
			mgr = m
		}
	}
	reg.Register(&FindDefinitionTool{root: sandboxRoot, mgr: mgr, managers: managers})
	reg.Register(&ListSymbolsTool{root: sandboxRoot, mgr: mgr, managers: managers})
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
