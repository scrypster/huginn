package backend

import (
	"fmt"
)

// NewKeyResolver creates a KeyResolver that calls ResolveAPIKey(raw) on each
// invocation. The raw reference string (e.g. "$ENV_VAR", "keyring:svc:user")
// is captured by the closure — the resolved secret is never stored.
func NewKeyResolver(raw string) KeyResolver {
	return func() (string, error) {
		return ResolveAPIKey(raw)
	}
}

// NewFromConfig creates a Backend based on provider, endpoint, and apiKey strings.
// The apiKey is resolved via a KeyResolver on each request.
// model is the model identifier to use (for ContextWindow lookup etc.).
func NewFromConfig(provider, endpoint, apiKey, model string) (Backend, error) {
	return newFromResolvedConfig(provider, endpoint, NewKeyResolver(apiKey), model)
}

// newFromResolvedConfig creates a Backend with a KeyResolver.
// This is used internally by BackendCache.For() and NewFromConfig.
func newFromResolvedConfig(provider, endpoint string, resolver KeyResolver, model string) (Backend, error) {
	switch provider {
	case "ollama", "external", "":
		if endpoint == "" {
			endpoint = "http://localhost:11434"
		}
		b := NewExternalBackend(endpoint)
		b.SetModel(model)
		return b, nil

	case "openai":
		if endpoint == "" {
			endpoint = "https://api.openai.com"
		}
		b := NewExternalBackendWithAPIKey(endpoint, resolver)
		b.SetModel(model)
		return b, nil

	case "anthropic":
		return NewAnthropicBackend(resolver, model), nil

	case "openrouter":
		return NewOpenRouterBackend(resolver, model), nil

	default:
		return nil, fmt.Errorf("backend: unknown provider %q", provider)
	}
}
