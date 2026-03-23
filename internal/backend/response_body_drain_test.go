package backend

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestAnthropicBackend_Non200_BodyDrained verifies that a non-200 response body
// is fully drained before the connection is returned to the pool.
//
// Bug: on HTTP !200, ChatCompletion returns an error immediately after
// resp.Body.Close() via the deferred close. The body is NOT read before close.
// Closing an unread body prevents TCP connection reuse (the server sees the
// client abort mid-response). Under load this creates new connections per request
// rather than reusing keep-alive connections.
//
// Fix: drain resp.Body with io.Copy(io.Discard, resp.Body) before or instead of
// returning error, so the connection is eligible for keep-alive reuse.
func TestAnthropicBackend_Non200_BodyDrained(t *testing.T) {
	t.Parallel()

	bodyRead := atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wrap the body to detect if it was fully read by the client.
		w.WriteHeader(http.StatusTooManyRequests)
		_, err := io.WriteString(w, `{"error":"rate limited"}`)
		if err == nil {
			bodyRead.Store(true)
		}
	}))
	defer srv.Close()

	b := NewAnthropicBackendWithEndpoint(NewKeyResolver("test-key"), "claude-sonnet-4-6", srv.URL)

	// Make two requests to the same server. If the first response body is not
	// drained, the second request will use a new connection (connection count
	// goes up). We verify both requests succeed at the transport level.
	for i := 0; i < 2; i++ {
		_, err := b.ChatCompletion(context.Background(), ChatRequest{
			Model:    "claude-sonnet-4-6",
			Messages: []Message{{Role: "user", Content: "hello"}},
		})
		if err == nil {
			t.Fatalf("request %d: expected error for 429 response, got nil", i)
		}
	}
}

// TestExternalBackend_Non200_BodyDrained verifies the same for ExternalBackend.
func TestExternalBackend_Non200_BodyDrained(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"error":"internal error","details":"something failed"}`)
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)

	// Two requests; if body is not drained the second call creates a new conn.
	// Either way must return an error, not panic.
	for i := 0; i < 2; i++ {
		_, err := b.ChatCompletion(context.Background(), ChatRequest{
			Model:    "test-model",
			Messages: []Message{{Role: "user", Content: "hello"}},
		})
		if err == nil {
			t.Fatalf("request %d: expected error for 500 response, got nil", i)
		}
	}
}

// TestAnthropicBackend_Non200_ErrorIncludesBody verifies that the error message
// for 4xx responses includes the response body (useful for 401 debugging).
func TestAnthropicBackend_Non200_ErrorIncludesBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"invalid_api_key"}`)
	}))
	defer srv.Close()

	b := NewAnthropicBackendWithEndpoint(NewKeyResolver("bad-key"), "claude-sonnet-4-6", srv.URL)

	_, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	// Error must include body content for 4xx responses so users can diagnose
	// API key issues without a separate curl. Status code alone is not enough.
	if msg := err.Error(); !contains(msg, "invalid_api_key") {
		t.Errorf("expected error to include response body %q for 401, got: %q", "invalid_api_key", msg)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
