package backend_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

// TestErrRateLimited_IsTypedError verifies that ErrRateLimited is a sentinel error.
func TestErrRateLimited_IsTypedError(t *testing.T) {
	if backend.ErrRateLimited == nil {
		t.Fatal("ErrRateLimited must not be nil")
	}
	if backend.ErrRateLimited.Error() == "" {
		t.Error("ErrRateLimited must have a non-empty message")
	}
}

// TestRateLimitError_Is verifies that *RateLimitError satisfies errors.Is(ErrRateLimited).
func TestRateLimitError_Is(t *testing.T) {
	rle := &backend.RateLimitError{Body: "rate limit exceeded"}
	if !errors.Is(rle, backend.ErrRateLimited) {
		t.Error("*RateLimitError should satisfy errors.Is(ErrRateLimited)")
	}
}

// TestRateLimitError_Error_WithBody verifies the error message includes the body.
func TestRateLimitError_Error_WithBody(t *testing.T) {
	rle := &backend.RateLimitError{Body: "please slow down"}
	msg := rle.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
	if msg == backend.ErrRateLimited.Error() {
		t.Error("expected body to enrich the error message")
	}
}

// TestRateLimitError_Error_WithoutBody verifies the fallback message.
func TestRateLimitError_Error_WithoutBody(t *testing.T) {
	rle := &backend.RateLimitError{}
	if rle.Error() != backend.ErrRateLimited.Error() {
		t.Errorf("expected fallback to ErrRateLimited message, got %q", rle.Error())
	}
}

// TestIsRateLimited_True verifies IsRateLimited returns true for *RateLimitError.
func TestIsRateLimited_True(t *testing.T) {
	err := &backend.RateLimitError{Body: "rate limit"}
	if !backend.IsRateLimited(err) {
		t.Error("expected IsRateLimited to return true for *RateLimitError")
	}
}

// TestIsRateLimited_False verifies IsRateLimited returns false for other errors.
func TestIsRateLimited_False(t *testing.T) {
	err := errors.New("some other error")
	if backend.IsRateLimited(err) {
		t.Error("expected IsRateLimited to return false for generic error")
	}
}

// TestIsRateLimited_Nil verifies IsRateLimited returns false for nil.
func TestIsRateLimited_Nil(t *testing.T) {
	if backend.IsRateLimited(nil) {
		t.Error("expected IsRateLimited to return false for nil")
	}
}

// TestAnthropicBackend_429_ReturnsRateLimitError verifies that the Anthropic backend
// surfaces HTTP 429 responses as *RateLimitError.
func TestAnthropicBackend_429_ReturnsRateLimitError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`))
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("test-key"), "claude-3-haiku-20240307", srv.URL)
	_, err := b.ChatCompletion(t.Context(), backend.ChatRequest{
		Model:    "claude-3-haiku-20240307",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error from 429 response")
	}
	if !backend.IsRateLimited(err) {
		t.Errorf("expected *RateLimitError, got: %T %v", err, err)
	}
}

// TestExternalBackend_429_ReturnsRateLimitError verifies that the ExternalBackend (OpenAI)
// surfaces HTTP 429 responses as *RateLimitError.
func TestExternalBackend_429_ReturnsRateLimitError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"Rate limit exceeded"}}`))
	}))
	defer srv.Close()

	b := backend.NewExternalBackendWithAPIKey(srv.URL+"/v1", backend.NewKeyResolver("test-key"))
	b.SetModel("test-model")
	_, err := b.ChatCompletion(t.Context(), backend.ChatRequest{
		Model:    "test-model",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error from 429 response")
	}
	if !backend.IsRateLimited(err) {
		t.Errorf("expected *RateLimitError, got: %T %v", err, err)
	}
}

// TestAnthropicBackend_401_ReturnsGenericError verifies that 401 does not return ErrRateLimited.
func TestAnthropicBackend_401_ReturnsGenericError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid api key"}`))
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("bad-key"), "claude-3-haiku-20240307", srv.URL)
	_, err := b.ChatCompletion(t.Context(), backend.ChatRequest{
		Model:    "claude-3-haiku-20240307",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error from 401 response")
	}
	if backend.IsRateLimited(err) {
		t.Error("401 should not be treated as rate limit")
	}
}
