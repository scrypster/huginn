package catalog

import (
	"context"
	"fmt"
	"sync"
)

// ── Validator ─────────────────────────────────────────────────────────────────

// Validator is the interface implemented by every credential provider's
// connectivity check.  The fields map contains all values submitted by the
// user, keyed by the FieldDef.Key values from the catalog entry.
//
// Implementations are expected to map named keys to their positional
// validation function arguments explicitly — never use positional indexing
// on the fields map.
type Validator interface {
	Validate(ctx context.Context, fields map[string]string) error
}

// ValidatorFunc is a function adapter that implements Validator.
// It allows an anonymous function to satisfy the Validator interface without
// creating a named type per provider.
type ValidatorFunc func(ctx context.Context, fields map[string]string) error

// Validate implements Validator.
func (f ValidatorFunc) Validate(ctx context.Context, fields map[string]string) error {
	return f(ctx, fields)
}

// ── Registry ──────────────────────────────────────────────────────────────────

// Registry maps provider IDs (e.g., "datadog") to their Validator.
// All methods are safe for concurrent use.
type Registry struct {
	mu         sync.RWMutex
	validators map[string]Validator
}

// NewRegistry returns an empty, ready-to-use Registry.
func NewRegistry() *Registry {
	return &Registry{validators: make(map[string]Validator)}
}

// Register adds or replaces the Validator for the given provider ID.
// Panics if id or v is empty/nil — callers must register with valid arguments.
func (r *Registry) Register(id string, v Validator) {
	if id == "" {
		panic("catalog.Registry.Register: id must not be empty")
	}
	if v == nil {
		panic(fmt.Sprintf("catalog.Registry.Register: validator for %q must not be nil", id))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.validators[id] = v
}

// Get returns the Validator for the given provider ID and a boolean that
// reports whether a validator was found.  Returns nil, false for unknown IDs.
func (r *Registry) Get(id string) (Validator, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.validators[id]
	return v, ok
}
