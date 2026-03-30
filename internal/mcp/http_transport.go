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
// Each Send POSTs the message body and makes the response available via Receive.
// Receive blocks until Send provides a response, supporting the recvLoop pattern
// where recvLoop may call Receive before transport.Send has completed.
// 204/empty responses (e.g. notification ACKs) do NOT unblock a pending Receive.
type HTTPTransport struct {
	endpoint string
	token    string
	client   *http.Client
	// ch is a buffered channel (size 1) that carries the response body from Send
	// to Receive. Buffered so Send never blocks when no Receive is pending yet.
	ch   chan []byte
	done chan struct{}
	once sync.Once
}

// NewHTTPTransport creates an HTTPTransport for the given MCP HTTP endpoint and bearer token.
func NewHTTPTransport(endpoint, token string) *HTTPTransport {
	return &HTTPTransport{
		endpoint: endpoint,
		token:    token,
		client:   &http.Client{},
		ch:       make(chan []byte, 1),
		done:     make(chan struct{}),
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
	// 204/empty = notification ACK — no response body to deliver.
	if resp.StatusCode == http.StatusNoContent || len(body) == 0 {
		return nil
	}
	// Non-blocking send: channel is buffered(1). If a previous response was not
	// consumed before another Send, drain the stale entry first to avoid blocking.
	select {
	case t.ch <- body:
	default:
		// Drain stale response and replace with fresh one.
		select {
		case <-t.ch:
		default:
		}
		t.ch <- body
	}
	return nil
}

// Receive waits for a response body posted by Send. It blocks until one is
// available or the context is cancelled. This allows recvLoop to call Receive
// before transport.Send has been dispatched (the HTTP response arrives after Send).
func (t *HTTPTransport) Receive(ctx context.Context) ([]byte, error) {
	select {
	case b := <-t.ch:
		return b, nil
	case <-t.done:
		return nil, fmt.Errorf("mcp http: transport closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close shuts down the transport, unblocking any goroutine blocked in Receive.
// Safe to call multiple times.
func (t *HTTPTransport) Close() error {
	t.once.Do(func() { close(t.done) })
	return nil
}

var _ Transport = (*HTTPTransport)(nil)
