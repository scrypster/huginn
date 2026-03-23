package backend

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
)

// BackendCache lazily instantiates and caches backends keyed by config.
// Agents that share provider+endpoint+apiKey reuse a single backend instance.
type BackendCache struct {
	mu           sync.Mutex
	backends     map[string]Backend
	fallback     Backend           // global backend from config.json
	providerKeys map[string]string // provider → raw API key ref (e.g. "keyring:huginn:anthropic")
}

// NewBackendCache creates a BackendCache with the given fallback backend.
// The fallback is returned when For() is called with an empty provider.
func NewBackendCache(fallback Backend) *BackendCache {
	return &BackendCache{
		backends:     make(map[string]Backend),
		fallback:     fallback,
		providerKeys: make(map[string]string),
	}
}

// SetProviderKey registers a provider-level API key fallback. When For() is
// called with a matching provider but no per-agent api_key, this ref is used
// instead of creating a backend with an empty key.
//
// Calling SetProviderKey evicts all cached backends for that provider so that
// subsequent For() calls create new backends with the updated key resolver.
// This enables live key rotation without restarting huginn: update the keychain
// entry, call SetProviderKey with the same keyring ref, and new requests pick
// up the new key immediately.
func (c *BackendCache) SetProviderKey(provider, rawRef string) {
	if provider == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	old := c.providerKeys[provider]
	c.providerKeys[provider] = rawRef
	// Evict cached backends that were built with the old provider-level key
	// so the next For() call creates a fresh backend with the new resolver.
	if old != rawRef {
		prefix := provider + ":"
		for k := range c.backends {
			if strings.HasPrefix(k, prefix) {
				delete(c.backends, k)
			}
		}
	}
}

// keyFingerprint returns a short hex fingerprint of a string.
// Used for cache keys to distinguish backends by their raw API key reference
// without storing either the reference or the resolved value.
func keyFingerprint(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h[:4]) // 8 hex chars
}

// For resolves the backend for the given provider/endpoint/apiKey/model.
// If provider is empty, the fallback global backend is returned.
// Cache key uses the raw apiKey reference string (e.g. "$ENV_VAR", "keyring:svc:user")
// — same reference = same backend. The resolved value is never computed here;
// a KeyResolver is built and each ChatCompletion call resolves on demand.
//
// When apiKey is empty and the provider has a registered fallback key (set via
// SetProviderKey), the fallback is used. This allows agents to specify a provider
// without needing their own api_key, inheriting the globally configured key.
//
// The entire check-create-store sequence is performed under the same lock
// acquisition to eliminate the check-then-act race condition.
func (c *BackendCache) For(provider, endpoint, apiKey, model string) (Backend, error) {
	if provider == "" {
		if c.fallback == nil {
			return nil, fmt.Errorf("backend cache: no provider specified and no fallback backend configured")
		}
		return c.fallback, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Inherit the provider-level key when no per-agent key is specified.
	// This must happen inside the lock to avoid a TOCTOU race with SetProviderKey.
	resolvedKeyRef := apiKey
	if resolvedKeyRef == "" {
		resolvedKeyRef = c.providerKeys[provider]
	}

	key := provider + ":" + endpoint + ":" + keyFingerprint(resolvedKeyRef) + ":" + model

	if b, ok := c.backends[key]; ok {
		return b, nil
	}

	resolver := NewKeyResolver(resolvedKeyRef)
	b, err := newFromResolvedConfig(provider, endpoint, resolver, model)
	if err != nil {
		return nil, fmt.Errorf("backend cache: %w", err)
	}

	c.backends[key] = b
	return b, nil
}

// WithFallbackAPIKey updates the raw API key reference on the fallback backend
// when the fallback implements KeyedBackend. Returns c for method chaining.
// Safe to call while running.
func (c *BackendCache) WithFallbackAPIKey(rawRef string) *BackendCache {
	c.mu.Lock()
	defer c.mu.Unlock()
	if kb, ok := c.fallback.(interface{ SetAPIKey(string) }); ok {
		kb.SetAPIKey(rawRef)
	}
	return c
}

// FallbackStatus reports the circuit-breaker state of the fallback backend.
// Returns "unknown" when the fallback does not implement StatusReporter.
func (c *BackendCache) FallbackStatus() string {
	c.mu.Lock()
	fb := c.fallback
	c.mu.Unlock()
	if sr, ok := fb.(StatusReporter); ok {
		return sr.BackendStatus()
	}
	return "unknown"
}

// InvalidateCache evicts all cached backend instances so subsequent For() calls
// create new backends (e.g. after an API key change).
func (c *BackendCache) InvalidateCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.backends = make(map[string]Backend)
}

// Shutdown calls Shutdown on all cached backends and the fallback.
func (c *BackendCache) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var firstErr error
	for key, b := range c.backends {
		if err := b.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("shutdown backend %q: %w", key, err)
		}
	}
	if c.fallback != nil {
		if err := c.fallback.Shutdown(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("shutdown fallback backend: %w", err)
		}
	}
	return firstErr
}
