package backend_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

// TestChatCompletion_Immediate401_ReturnsError verifies that an HTTP 401 response
// is surfaced as an error (not a nil-error + empty response).
func TestChatCompletion_Immediate401_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"invalid_api_key"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(func() (string, error) { return "bad-key", nil }, "claude-3-5-haiku-20241022", srv.URL)
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-3-5-haiku-20241022",
		Messages: []backend.Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error for HTTP 401, got nil")
	}
}

// TestChatCompletion_403_ReturnsError verifies that HTTP 403 is surfaced as an error.
func TestChatCompletion_403_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(func() (string, error) { return "key", nil }, "claude-3-5-haiku-20241022", srv.URL)
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-3-5-haiku-20241022",
		Messages: []backend.Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error for HTTP 403, got nil")
	}
}

// TestChatCompletion_500_ReturnsError verifies that HTTP 500 is surfaced as an error.
func TestChatCompletion_500_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"internal_server_error"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(func() (string, error) { return "key", nil }, "claude-3-5-haiku-20241022", srv.URL)
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-3-5-haiku-20241022",
		Messages: []backend.Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}
