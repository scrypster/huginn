package tools

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"sync"

	"github.com/scrypster/huginn/internal/backend"
)

// validToolName matches tool names that are safe for use with LLM function
// calling: 1–64 characters, only alphanumeric, underscores, and hyphens.
var validToolName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// validateToolSchema checks that a tool has a valid schema before registration.
// Returns a non-nil error when the schema is invalid so callers can decide
// whether to reject silently (Register) or surface the error (RegisterStrict).
func validateToolSchema(t Tool) error {
	schema := t.Schema()
	name := schema.Function.Name
	if name == "" {
		return fmt.Errorf("tools: tool schema missing function name for type %T", t)
	}
	if !validToolName.MatchString(name) {
		return fmt.Errorf("tools: tool name %q contains invalid characters (must match ^[a-zA-Z0-9_-]{1,64}$)", name)
	}
	return nil
}

// Registry holds all registered tools and provides lookup + filtering.
type Registry struct {
	mu             sync.RWMutex
	tools          map[string]Tool
	allowed        map[string]bool // nil = all allowed
	blocked        map[string]bool // nil = nothing blocked
	providerByTool map[string]string // tool name → provider (e.g. "github")
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		tools:          make(map[string]Tool),
		providerByTool: make(map[string]string),
	}
}

// Register adds a tool to the registry.
// If the tool's schema is invalid (missing/illegal function name), the
// registration is skipped and an error is logged via slog.Error so the
// operator can detect the problem without crashing the process.
func (r *Registry) Register(t Tool) {
	if err := validateToolSchema(t); err != nil {
		slog.Error("tools: skipping registration due to invalid schema", "error", err)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[t.Name()]; exists {
		slog.Warn("tools: overwriting existing tool registration", "name", t.Name())
	}
	r.tools[t.Name()] = t
}

// RegisterStrict registers a tool and returns an error for any of the
// following conditions:
//   - the tool schema is invalid (missing / illegal function name)
//   - a tool with that name is already registered
//
// Use this for MCP/connection tool injection where a name collision or
// malformed schema is unexpected and must be surfaced to the operator.
func (r *Registry) RegisterStrict(t Tool) error {
	if err := validateToolSchema(t); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[t.Name()]; exists {
		return fmt.Errorf("tools: tool %q is already registered; use Register to override", t.Name())
	}
	r.tools[t.Name()] = t
	return nil
}

// StrictRegister is an alias for RegisterStrict retained for backward
// compatibility with existing callers. Prefer RegisterStrict in new code.
func (r *Registry) StrictRegister(t Tool) error {
	return r.RegisterStrict(t)
}

// Unregister removes a tool by name. Safe to call even if the tool does not exist.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
}

// Get returns the tool with the given name, or false if not found.
// Returns false if the tool is disabled/blocked.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	if !ok {
		return nil, false
	}
	if !r.isEnabledLocked(name) {
		return nil, false
	}
	return t, ok
}

// All returns all enabled tools in deterministic order.
func (r *Registry) All() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	// Sort for determinism
	sort.Strings(names)
	var result []Tool
	for _, name := range names {
		if r.isEnabledLocked(name) {
			result = append(result, r.tools[name])
		}
	}
	return result
}

// AllSchemas returns tool schemas for all enabled tools, for sending to the model.
func (r *Registry) AllSchemas() []backend.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	schemas := make([]backend.Tool, 0, len(r.tools))
	for _, t := range r.tools {
		if r.isEnabledLocked(t.Name()) {
			schemas = append(schemas, t.Schema())
		}
	}
	return schemas
}

// SetAllowed configures a whitelist. Only the listed tools will be enabled.
// Pass nil or empty to clear the whitelist (all tools enabled).
func (r *Registry) SetAllowed(names []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(names) == 0 {
		r.allowed = nil
		return
	}
	r.allowed = make(map[string]bool, len(names))
	for _, n := range names {
		r.allowed[n] = true
	}
}

// SetBlocked configures a blacklist. The listed tools will be disabled.
func (r *Registry) SetBlocked(names []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(names) == 0 {
		r.blocked = nil
		return
	}
	r.blocked = make(map[string]bool, len(names))
	for _, n := range names {
		r.blocked[n] = true
	}
}

// IsEnabled returns true if the tool with the given name is available (not blocked/not in whitelist).
func (r *Registry) IsEnabled(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.isEnabledLocked(name)
}

func (r *Registry) isEnabledLocked(name string) bool {
	if r.blocked != nil && r.blocked[name] {
		return false
	}
	if r.allowed != nil {
		return r.allowed[name]
	}
	return true
}

// TagTools associates a list of tool names with a provider string.
// Call after Register. Tags are stored regardless of whether the tool name
// is currently registered (registration may happen in any order).
func (r *Registry) TagTools(names []string, provider string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, n := range names {
		r.providerByTool[n] = provider
	}
}

// ProviderFor returns the provider string associated with the given tool name,
// or "" if the tool is untagged.
func (r *Registry) ProviderFor(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.providerByTool[name]
}

// AllSchemasForProviders returns schemas for tools belonging to the given providers.
// A provider of "*" means all externally-tagged tools (non-builtin).
// Empty providers slice returns nothing (default-deny; caller must pass ["*"] for all).
func (r *Registry) AllSchemasForProviders(providers []string) []backend.Tool {
	if len(providers) == 0 {
		return nil
	}
	allowAll := false
	allowed := make(map[string]bool, len(providers))
	for _, p := range providers {
		if p == "*" {
			allowAll = true
		} else {
			allowed[p] = true
		}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	schemas := make([]backend.Tool, 0, len(providers)*5)
	for _, t := range r.tools {
		if !r.isEnabledLocked(t.Name()) {
			continue
		}
		provider := r.providerByTool[t.Name()]
		if provider == "builtin" {
			continue // builtin tools are resolved by applyToolbelt separately
		}
		if allowAll || (provider != "" && allowed[provider]) {
			schemas = append(schemas, t.Schema())
		}
	}
	return schemas
}

// SchemasByNames returns schemas for the specific named tools.
// Unknown names are silently skipped. Empty slice returns nothing.
func (r *Registry) SchemasByNames(names []string) []backend.Tool {
	if len(names) == 0 {
		return nil
	}
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	schemas := make([]backend.Tool, 0, len(names))
	for _, t := range r.tools {
		if want[t.Name()] && r.isEnabledLocked(t.Name()) {
			schemas = append(schemas, t.Schema())
		}
	}
	return schemas
}

// Execute runs the named tool with the given args.
// Returns the output string and an error if the tool is unknown or execution fails.
// Satisfies threadmgr.ToolRegistryIface.
func (r *Registry) Execute(ctx context.Context, name string, args map[string]any) (string, error) {
	t, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	result := t.Execute(ctx, args)
	if result.IsError {
		return result.Output, fmt.Errorf("%s", result.Error)
	}
	return result.Output, nil
}

// Fork returns a per-session shallow copy of the registry.
//
// Safety contract: This is safe because all tools registered in the shared
// (parent) registry are stateless — they hold no per-call mutable state.
// Per-session tools (e.g., MuninnDB MCP adapters) MUST only be registered
// into a forked copy, never the shared parent. Violating this invariant
// reintroduces the concurrent-session race this method is designed to prevent.
func (r *Registry) Fork() *Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f := &Registry{
		tools:          make(map[string]Tool, len(r.tools)),
		providerByTool: make(map[string]string, len(r.providerByTool)),
	}
	for k, v := range r.tools {
		f.tools[k] = v
	}
	for k, v := range r.providerByTool {
		f.providerByTool[k] = v
	}
	return f
}

// AllBuiltinSchemas returns schemas for all tools tagged with the "builtin" provider.
func (r *Registry) AllBuiltinSchemas() []backend.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	schemas := make([]backend.Tool, 0)
	for _, t := range r.tools {
		if !r.isEnabledLocked(t.Name()) {
			continue
		}
		if r.providerByTool[t.Name()] == "builtin" {
			schemas = append(schemas, t.Schema())
		}
	}
	return schemas
}
