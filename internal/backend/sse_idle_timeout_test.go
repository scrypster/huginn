package backend

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestParseSSE_IdleTimeout_AbortsStallMidStream verifies that the internal
// idle-stall watchdog fires and aborts a stream when no SSE data arrives within
// streamStallTimeout. This tests the path where the server sends the HTTP 200
// header and begins the SSE stream but then stops sending data mid-stream
// without closing the connection — distinct from the context-deadline test
// which uses an external cancellation.
//
// The test lowers streamStallTimeout to 100ms so the watchdog fires quickly.
func TestParseSSE_IdleTimeout_AbortsStallMidStream(t *testing.T) {
	// Override the idle timeout to something short so the test completes quickly.
	orig := streamStallTimeout
	streamStallTimeout = 150 * time.Millisecond
	t.Cleanup(func() { streamStallTimeout = orig })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		// Send one valid SSE event to exercise the activity-reset path.
		fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n")
		flusher.Flush()

		// Now stall: stop sending data but keep the connection open.
		// The client should time out via the idle watchdog.
		<-r.Context().Done()
	}))
	defer srv.Close()

	// Use a long context timeout — the stall should be caught by the idle watchdog,
	// not the external context deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	b := NewAnthropicBackendWithEndpoint(func() (string, error) { return "key", nil }, "claude-sonnet-4-6", srv.URL)
	start := time.Now()
	_, err := b.ChatCompletion(ctx, ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from idle stall timeout, got nil")
	}

	// The error should mention the idle timeout, not generic context cancellation.
	if !strings.Contains(err.Error(), "idle timeout") {
		t.Errorf("expected 'idle timeout' in error message, got: %v", err)
	}

	// Should complete well within the idle timeout + some margin, not the full 10s context.
	if elapsed > 3*time.Second {
		t.Errorf("expected stall to be detected quickly, but took %v", elapsed)
	}
}

// TestParseSSE_NormalStream_NotInterrupted verifies that a well-behaved SSE
// stream that completes normally is not aborted by the idle watchdog.
func TestParseSSE_NormalStream_NotInterrupted(t *testing.T) {
	// Use a slightly longer timeout than the stream takes so the watchdog doesn't
	// interfere with fast-completing streams.
	orig := streamStallTimeout
	streamStallTimeout = 2 * time.Second
	t.Cleanup(func() { streamStallTimeout = orig })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		// Send a complete, well-formed SSE stream without any stall.
		fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\", world\"}}\n\n")
		flusher.Flush()
		startData := `{"type":"message_start","message":{"usage":{"input_tokens":5,"output_tokens":0}}}`
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", startData)
		flusher.Flush()
		deltaData := `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", deltaData)
		flusher.Flush()
		fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		flusher.Flush()
		// Connection closes naturally when handler returns.
	}))
	defer srv.Close()

	ctx := context.Background()
	b := NewAnthropicBackendWithEndpoint(func() (string, error) { return "key", nil }, "claude-sonnet-4-6", srv.URL)
	resp, err := b.ChatCompletion(ctx, ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("expected normal completion, got error: %v", err)
	}
	if resp.Content != "Hello, world" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello, world")
	}
}
