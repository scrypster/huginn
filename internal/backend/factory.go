package backend

import (
	"context"
	"fmt"
	"os"
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
	case "ollama", "":
		// Genuinely ollama (or the unset-provider default, which config
		// migration also resolves to "ollama" — see migrateV5toV6): gets
		// DefaultOllamaKeepAlive so a model that was just loaded stays
		// resident. See D1: this must NOT live in NewExternalBackend itself,
		// since that constructor is reused by non-ollama callers.
		if endpoint == "" {
			endpoint = "http://localhost:11434"
		}
		b := NewExternalBackend(endpoint)
		b.SetKeepAlive(DefaultOllamaKeepAlive)
		b.SetModel(model)
		return b, nil

	case "external":
		// Generic OpenAI-compatible endpoint — not confirmed to be ollama,
		// so keep_alive stays omitted unless the caller explicitly opts in
		// via SetKeepAlive.
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

	case "deepseek":
		if endpoint == "" {
			endpoint = "https://api.deepseek.com"
		}
		b := NewExternalBackendWithAPIKey(endpoint, resolver)
		b.SetModel(model)
		return b, nil

	case "zai":
		if endpoint == "" {
			endpoint = "https://api.z.ai/api/paas/v4"
		}
		b := NewExternalBackendWithAPIKey(endpoint, resolver)
		b.SetModel(model)
		return b, nil

	case "custom":
		if endpoint == "" {
			return nil, fmt.Errorf("backend: provider %q requires an endpoint to be configured", "custom")
		}
		b := NewExternalBackendWithAPIKey(endpoint, resolver)
		b.SetModel(model)
		return b, nil

	case "anthropic":
		return NewAnthropicBackend(resolver, model), nil

	case "openrouter":
		return NewOpenRouterBackend(resolver, model), nil

	case "google":
		// Google AI Studio (Generative Language API). Distinct from Vertex AI
		// — uses a single API key, no project / location / OAuth.
		return NewGoogleAIBackend(resolver, model), nil

	case "vertex":
		return NewVertexBackend(context.Background(), VertexConfig{
			Project:         os.Getenv("GOOGLE_CLOUD_PROJECT"),
			Location:        os.Getenv("GOOGLE_CLOUD_LOCATION"),
			CredentialsPath: os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"),
			Model:           model,
		})

	default:
		return nil, fmt.Errorf("backend: unknown provider %q", provider)
	}
}
