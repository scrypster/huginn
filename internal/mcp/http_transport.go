package mcp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// HTTPTransport implements Transport for MuninnDB MCP HTTP endpoint.
// Each Send POSTs the message body and buffers the response for the
// subsequent Receive call. Calls must be serialized (Send → Receive → Send → …).
// 204/empty responses (e.g. notification ACKs) do NOT populate pending.
type HTTPTransport struct {
	endpoint string
	token    string
	client   *http.Client
	mu       sync.Mutex
	pending  []byte
}

// NewHTTPTransport creates an HTTPTransport for the given MCP HTTP endpoint and bearer token.
func NewHTTPTransport(endpoint, token string) *HTTPTransport {
	return &HTTPTransport{
		endpoint: endpoint,
		token:    token,
		client:   &http.Client{},
	}
}

func (t *HTTPTransport) Send(ctx context.Context, msg []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(msg))
	if err != nil {
		return fmt.Errorf("mcp http: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("mcp http: post: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("mcp http: read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("mcp http: server returned %d: %s", resp.StatusCode, string(body))
	}
	// 204/empty = notification ACK — do not populate pending
	if resp.StatusCode == http.StatusNoContent || len(body) == 0 {
		return nil
	}
	t.mu.Lock()
	t.pending = body
	t.mu.Unlock()
	return nil
}

func (t *HTTPTransport) Receive(_ context.Context) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pending == nil {
		return nil, fmt.Errorf("mcp http: no pending response (call Send first)")
	}
	b := t.pending
	t.pending = nil
	return b, nil
}

func (t *HTTPTransport) Close() error { return nil }

var _ Transport = (*HTTPTransport)(nil)
