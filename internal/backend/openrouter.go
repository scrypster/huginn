package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/modelconfig"
)

const (
	openRouterDefaultEndpoint = "https://openrouter.ai/api/v1"
	openRouterReferer         = "https://huginn.dev"
	openRouterTitle           = "Huginn"
)

// OpenRouterBackend calls OpenRouter's OpenAI-compatible /chat/completions endpoint.
// It reuses ExternalBackend's SSE parsing and adds OpenRouter-specific headers and routing.
// It is safe for concurrent use.
type OpenRouterBackend struct {
	*ExternalBackend
	keyResolver   KeyResolver
	modelID       string
	providerOrder []string
	allowFallback bool
}

// NewOpenRouterBackend creates an OpenRouterBackend with the default endpoint.
func NewOpenRouterBackend(resolver KeyResolver, modelID string) *OpenRouterBackend {
	return NewOpenRouterBackendWithEndpoint(resolver, modelID, openRouterDefaultEndpoint)
}

// NewOpenRouterBackendWithEndpoint creates an OpenRouterBackend with a custom endpoint.
func NewOpenRouterBackendWithEndpoint(resolver KeyResolver, modelID, endpoint string) *OpenRouterBackend {
	external := NewExternalBackend(endpoint)
	// Set the model on the embedded ExternalBackend so ContextWindow() works
	external.SetModel(modelID)

	return &OpenRouterBackend{
		ExternalBackend: external,
		keyResolver:     resolver,
		modelID:         modelID,
	}
}

// SetProviderOrder sets the provider routing order for OpenRouter.
// This adds a "provider" field to the request body with the specified order and allow_fallbacks flag.
// Call this before ChatCompletion to enable provider routing.
func (b *OpenRouterBackend) SetProviderOrder(order []string, allowFallbacks bool) {
	b.providerOrder = order
	b.allowFallback = allowFallbacks
}

// ChatCompletion sends a chat request to OpenRouter and returns the response.
// It adds OpenRouter-specific headers (Authorization, HTTP-Referer, X-Title) and
// optionally includes provider routing in the request body.
func (b *OpenRouterBackend) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	apiKey, err := b.keyResolver()
	if err != nil {
		return nil, fmt.Errorf("chat completion: resolve api key: %w", err)
	}

	// Build the base request body using ExternalBackend's buildRequest
	baseBody, err := b.ExternalBackend.buildRequest(req)
	if err != nil {
		return nil, err
	}

	// Parse the base request and add provider field if set
	var bodyMap map[string]any
	if err := json.Unmarshal(baseBody, &bodyMap); err != nil {
		return nil, fmt.Errorf("unmarshal request body: %w", err)
	}

	// Add provider routing if configured
	if len(b.providerOrder) > 0 {
		bodyMap["provider"] = map[string]any{
			"order":           b.providerOrder,
			"allow_fallbacks": b.allowFallback,
		}
	}

	// Re-marshal the modified body
	modifiedBody, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("marshal modified request body: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		b.ExternalBackend.endpoint+"/chat/completions", bytes.NewReader(modifiedBody))
	if err != nil {
		return nil, err
	}

	// Set OpenRouter-specific headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("HTTP-Referer", openRouterReferer)
	httpReq.Header.Set("X-Title", openRouterTitle)

	// Make the request
	resp, err := b.ExternalBackend.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("chat completion: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("chat completion: HTTP %d", resp.StatusCode)
	}

	// Parse SSE response using ExternalBackend's parseSSE method
	return b.ExternalBackend.parseSSE(ctx, resp, req)
}

// ContextWindow returns the context window for the model.
// It strips the "provider/" prefix from the model ID (e.g., "anthropic/claude-sonnet-4-6" → "claude-sonnet-4-6")
// and looks up the context window size from modelconfig.
func (b *OpenRouterBackend) ContextWindow() int {
	// Strip provider prefix if present
	modelID := b.modelID
	if idx := strings.Index(modelID, "/"); idx != -1 {
		modelID = modelID[idx+1:]
	}
	return modelconfig.ContextWindowForModel(modelID)
}

// Health checks connectivity to OpenRouter with authentication.
func (b *OpenRouterBackend) Health(ctx context.Context) error {
	apiKey, err := b.keyResolver()
	if err != nil {
		return fmt.Errorf("openrouter health: resolve api key: %w", err)
	}
	hc := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.ExternalBackend.endpoint+"/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("openrouter health: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("openrouter health: HTTP %d", resp.StatusCode)
	}
	return nil
}

// compile-time interface check
var _ Backend = (*OpenRouterBackend)(nil)
