package backend

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

// googleAIDefaultEndpoint is Google's public Generative Language API host,
// also known as "Google AI Studio". Distinct from Vertex AI, which is a
// separate Google Cloud product with its own host and auth model.
const googleAIDefaultEndpoint = "https://generativelanguage.googleapis.com"

// GoogleAIBackend talks to Google's Generative Language API (AI Studio).
// Authenticates via a single API key passed as a query parameter. Only
// gemini-* models are supported.
//
// Safe for concurrent use.
type GoogleAIBackend struct {
	endpoint    string
	client      *http.Client
	keyResolver KeyResolver
	model       string
	cb          *circuitBreaker
}

// NewGoogleAIBackend creates a GoogleAIBackend with the default endpoint.
func NewGoogleAIBackend(resolver KeyResolver, model string) *GoogleAIBackend {
	return NewGoogleAIBackendWithEndpoint(resolver, model, googleAIDefaultEndpoint)
}

// NewGoogleAIBackendWithEndpoint creates a GoogleAIBackend with a custom endpoint.
func NewGoogleAIBackendWithEndpoint(resolver KeyResolver, model, endpoint string) *GoogleAIBackend {
	if endpoint == "" {
		endpoint = googleAIDefaultEndpoint
	}
	return &GoogleAIBackend{
		endpoint:    strings.TrimRight(endpoint, "/"),
		client:      &http.Client{Timeout: 0, Transport: streamingTransport()},
		keyResolver: resolver,
		model:       model,
		cb:          newCircuitBreaker(),
	}
}

// BackendStatus implements StatusReporter.
func (b *GoogleAIBackend) BackendStatus() string { return b.cb.State() }

// ContextWindow reuses the Vertex Gemini lookup since the same models are
// served via both products and share context-window sizes.
func (b *GoogleAIBackend) ContextWindow() int {
	return ContextWindowForVertexModel(b.model)
}

// Shutdown is a no-op for GoogleAIBackend (we don't own the service).
func (b *GoogleAIBackend) Shutdown(_ context.Context) error { return nil }

// Health verifies the API key by listing models. A 2xx response means the
// key is valid and the API is reachable.
func (b *GoogleAIBackend) Health(ctx context.Context) error {
	apiKey, err := b.keyResolver()
	if err != nil {
		return fmt.Errorf("google-ai health: resolve key: %w", err)
	}
	if apiKey == "" {
		return fmt.Errorf("google-ai health: API key is required")
	}
	url := fmt.Sprintf("%s/v1beta/models?key=%s", b.endpoint, apiKey)
	hc := *b.client
	hc.Timeout = 5 * time.Second
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("google-ai health: %w", err)
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("google-ai health: invalid API key (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("google-ai health: HTTP %d", resp.StatusCode)
	}
	return nil
}

// ChatCompletion sends a chat request to streamGenerateContent and returns
// the assembled ChatResponse. Reuses the Vertex Gemini body builder and SSE
// parser — the wire schemas are byte-identical between Vertex Gemini and AI
// Studio.
func (b *GoogleAIBackend) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = b.model
	}
	if !strings.Contains(strings.ToLower(model), "gemini") {
		return nil, fmt.Errorf("google-ai: unsupported model %q; expected gemini-*", model)
	}

	body, err := buildGeminiBody(req)
	if err != nil {
		return nil, fmt.Errorf("google-ai: build request: %w", err)
	}

	apiKey, err := b.keyResolver()
	if err != nil {
		return nil, fmt.Errorf("google-ai: resolve key: %w", err)
	}
	if apiKey == "" {
		return nil, fmt.Errorf("google-ai: API key is required")
	}

	if !b.cb.Allow() {
		return nil, ErrCircuitOpen
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s", b.endpoint, model, apiKey)

	var lastErr error
	for attempt := 0; attempt <= chatMaxRetries; attempt++ {
		if attempt > 0 {
			delay := chatRetryBase * (1 << uint(attempt-1))
			jitter := time.Duration(rand.Int63n(int64(delay / 4)))
			if rand.Intn(2) == 0 {
				jitter = -jitter
			}
			select {
			case <-time.After(delay + jitter):
			case <-ctx.Done():
				return nil, fmt.Errorf("google-ai: %w", ctx.Err())
			}
			slog.Warn("google-ai: retrying after transient error", "attempt", attempt, "err", lastErr)
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := b.client.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("google-ai: %w", err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			if resp.StatusCode == http.StatusTooManyRequests {
				rl := &RateLimitError{Body: string(bytes.TrimSpace(errBody))}
				rl.RetryAfter = parseRetryAfter(resp.Header)
				return nil, rl
			}
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				if len(errBody) > 0 {
					return nil, fmt.Errorf("google-ai: HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(errBody))
				}
				return nil, fmt.Errorf("google-ai: HTTP %d", resp.StatusCode)
			}
			lastErr = fmt.Errorf("google-ai: HTTP %d", resp.StatusCode)
			continue
		}

		result, err := parseGeminiSSE(ctx, resp, req, streamStallTimeout)
		if err != nil {
			b.cb.RecordFailure()
			return nil, err
		}
		b.cb.RecordSuccess()
		return result, nil
	}
	b.cb.RecordFailure()
	return nil, lastErr
}

var (
	_ Backend        = (*GoogleAIBackend)(nil)
	_ StatusReporter = (*GoogleAIBackend)(nil)
)
