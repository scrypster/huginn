package backend_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
)

// TestChatCompletion_StalledStream_ContextDeadline verifies that a stalled server
// (200 OK with no data) is interrupted by the request context deadline.
func TestChatCompletion_StalledStream_ContextDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 200 but never send any data — simulates a stalled SSE stream.
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Block until the client disconnects.
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	b := backend.NewAnthropicBackendWithEndpoint(func() (string, error) { return "key", nil }, "claude-3-5-haiku-20241022", srv.URL)
	start := time.Now()
	_, err := b.ChatCompletion(ctx, backend.ChatRequest{
		Model:    "claude-3-5-haiku-20241022",
		Messages: []backend.Message{{Role: "user", Content: "hello"}},
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from context deadline on stalled stream")
	}
	// Should have returned within ~500ms (not blocked indefinitely).
	if elapsed > 2*time.Second {
		t.Errorf("expected prompt return on deadline, took %v", elapsed)
	}
}
