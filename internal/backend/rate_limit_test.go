package backend

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRateLimitError_ErrorMessage verifies that RateLimitError.Error() returns
// a human-readable message that includes the response body when present.
func TestRateLimitError_ErrorMessage(t *testing.T) {
	t.Parallel()

	withBody := &RateLimitError{Body: "Retry after 60 seconds"}
	if !strings.Contains(withBody.Error(), "rate limited") {
		t.Errorf("expected 'rate limited' in error: %q", withBody.Error())
	}
	if !strings.Contains(withBody.Error(), "Retry after 60 seconds") {
		t.Errorf("expected body in error: %q", withBody.Error())
	}

	noBody := &RateLimitError{}
	if !strings.Contains(noBody.Error(), "rate limited") {
		t.Errorf("expected 'rate limited' in no-body error: %q", noBody.Error())
	}
}

// TestRateLimitError_ErrorsIs verifies that errors.Is works correctly with
// ErrRateLimited sentinel and the *RateLimitError concrete type.
func TestRateLimitError_ErrorsIs(t *testing.T) {
	t.Parallel()

	rl := &RateLimitError{Body: "too many requests"}
	if !errors.Is(rl, ErrRateLimited) {
		t.Error("errors.Is(rl, ErrRateLimited) should return true")
	}

	other := fmt.Errorf("some other error")
	if errors.Is(other, ErrRateLimited) {
		t.Error("errors.Is(other, ErrRateLimited) should return false")
	}
}

// TestIsRateLimited verifies the IsRateLimited helper function.
func TestIsRateLimited(t *testing.T) {
	t.Parallel()

	rl := &RateLimitError{}
	if !IsRateLimited(rl) {
		t.Error("IsRateLimited(*RateLimitError) should return true")
	}

	wrapped := fmt.Errorf("outer: %w", rl)
	if !IsRateLimited(wrapped) {
		t.Error("IsRateLimited(wrapped *RateLimitError) should return true")
	}

	other := fmt.Errorf("not rate limited")
	if IsRateLimited(other) {
		t.Error("IsRateLimited(other) should return false")
	}
}

// TestExternalBackend_429_ReturnsRateLimitError verifies that an HTTP 429
// response from the mock server causes ExternalBackend.ChatCompletion to
// return a *RateLimitError.
func TestExternalBackend_429_ReturnsRateLimitError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":"rate_limit_exceeded","retry_after":60}`)
	}))
	t.Cleanup(srv.Close)

	b := NewExternalBackend(srv.URL)
	_, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})

	if err == nil {
		t.Fatal("expected error for 429, got nil")
	}
	if !IsRateLimited(err) {
		t.Errorf("expected IsRateLimited(err)=true, got false; err=%v", err)
	}
	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Errorf("expected *RateLimitError, got %T: %v", err, err)
	}
}

// TestExternalBackend_429_BodyIncludedInError verifies that the 429 response
// body (up to 512 bytes) is included in the RateLimitError.Body field.
func TestExternalBackend_429_BodyIncludedInError(t *testing.T) {
	t.Parallel()

	const rateLimitMsg = "Quota exceeded for this minute"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, rateLimitMsg)
	}))
	t.Cleanup(srv.Close)

	b := NewExternalBackend(srv.URL)
	_, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})

	if err == nil {
		t.Fatal("expected error for 429, got nil")
	}
	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("expected *RateLimitError, got %T", err)
	}
	if !strings.Contains(rl.Body, rateLimitMsg) {
		t.Errorf("expected body to contain %q, got %q", rateLimitMsg, rl.Body)
	}
}

// TestAnthropicBackend_429_ReturnsRateLimitError verifies that AnthropicBackend
// also returns a *RateLimitError on HTTP 429.
func TestAnthropicBackend_429_ReturnsRateLimitError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"type":"rate_limit_error","message":"too many requests"}}`)
	}))
	t.Cleanup(srv.Close)

	b := NewAnthropicBackendWithEndpoint(
		func() (string, error) { return "test-api-key", nil },
		"claude-3-5-sonnet-20241022",
		srv.URL,
	)
	_, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "claude-3-5-sonnet-20241022",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})

	if err == nil {
		t.Fatal("expected error for 429, got nil")
	}
	if !IsRateLimited(err) {
		t.Errorf("expected IsRateLimited(err)=true; err=%v", err)
	}
}

// TestExternalBackend_429_vs_500 verifies the distinction between 429 (rate
// limit, typed error) and 500 (generic error, not a RateLimitError).
func TestExternalBackend_429_vs_500(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		status       int
		wantRateLimit bool
	}{
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, false},
		{http.StatusBadGateway, false},
	} {
		tc := tc
		t.Run(fmt.Sprintf("HTTP%d", tc.status), func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			t.Cleanup(srv.Close)

			b := NewExternalBackend(srv.URL)
			_, err := b.ChatCompletion(context.Background(), ChatRequest{
				Model:    "test",
				Messages: []Message{{Role: "user", Content: "hi"}},
			})

			if err == nil {
				t.Fatalf("expected error for HTTP %d, got nil", tc.status)
			}
			got := IsRateLimited(err)
			if got != tc.wantRateLimit {
				t.Errorf("IsRateLimited = %v, want %v; err = %v", got, tc.wantRateLimit, err)
			}
		})
	}
}
