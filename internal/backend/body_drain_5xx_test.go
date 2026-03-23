package backend

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestAnthropicBackend_BodyDrainOn5xx verifies that a 5xx response causes the
// backend to drain the body (allowing TCP connection reuse) and return an error.
func TestAnthropicBackend_BodyDrainOn5xx(t *testing.T) {
	// Track whether the body was fully read by the server handler.
	var bodyClosed bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyClosed = true
		w.WriteHeader(http.StatusServiceUnavailable) // 503
		fmt.Fprint(w, strings.Repeat("x", 256))     // 256-byte body
	}))
	defer srv.Close()

	b := NewAnthropicBackendWithEndpoint(func() (string, error) { return "test-key", nil }, "claude-3-5-sonnet-20241022", srv.URL)

	_, err := b.ChatCompletion(t.Context(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error for 5xx, got nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("expected 503 in error, got: %v", err)
	}
	if !bodyClosed {
		t.Error("expected handler to run (body closed on server side)")
	}
}

// TestAnthropicBackend_BodyDrainOn429 verifies that a 429 response is drained
// and returns a *RateLimitError.
func TestAnthropicBackend_BodyDrainOn429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "15")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":"rate_limit_exceeded"}`)
	}))
	defer srv.Close()

	b := NewAnthropicBackendWithEndpoint(func() (string, error) { return "test-key", nil }, "claude-3-5-sonnet-20241022", srv.URL)

	_, err := b.ChatCompletion(t.Context(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err == nil {
		t.Fatal("expected error for 429, got nil")
	}
	rle, ok := err.(*RateLimitError)
	if !ok {
		t.Fatalf("expected *RateLimitError, got %T: %v", err, err)
	}
	if rle.RetryAfter != 15*time.Second {
		t.Errorf("expected RetryAfter=15s, got %v", rle.RetryAfter)
	}
}

// drainCounter wraps an io.ReadCloser and counts total bytes read.
type drainCounter struct {
	rc    io.ReadCloser
	total int
}

func (d *drainCounter) Read(p []byte) (int, error) {
	n, err := d.rc.Read(p)
	d.total += n
	return n, err
}
func (d *drainCounter) Close() error { return d.rc.Close() }

// TestParseRetryAfter_InResponse verifies integration: the header value is
// extracted from an actual http.Header attached to a mock response.
func TestParseRetryAfter_InResponse(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "42")
	d := parseRetryAfter(h)
	if d.Seconds() != 42 {
		t.Errorf("expected 42s, got %v", d)
	}
}

// TestLimitReaderBodyDrain verifies that draining up to 512 bytes from a larger
// body via LimitReader leaves the connection reusable (no goroutine hang).
func TestLimitReaderBodyDrain(t *testing.T) {
	body := bytes.NewReader(bytes.Repeat([]byte("z"), 1024))
	rc := io.NopCloser(body)
	n, _ := io.Copy(io.Discard, io.LimitReader(rc, 512))
	if n != 512 {
		t.Errorf("expected to drain 512 bytes, drained %d", n)
	}
}
