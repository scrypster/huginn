package backend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// TestExternalBackend_StatusEvent_EmittedOnSlowHeaders verifies that a
// StreamStatus event is emitted when the server takes longer than
// externalModelLoadStatusDelay to send response headers (e.g. Ollama loading
// a model cold).
func TestExternalBackend_StatusEvent_EmittedOnSlowHeaders(t *testing.T) {
	t.Parallel()

	// Set delay to 20 ms. The server stalls 100 ms before sending headers.
	// After client.Do returns (100 ms elapsed), time.Since(headerStart) = 100 ms
	// which is >= 20 ms, so the status event fires synchronously — deterministic,
	// no goroutine-scheduling race.
	orig := externalModelLoadStatusDelay
	externalModelLoadStatusDelay = 20 * time.Millisecond
	t.Cleanup(func() { externalModelLoadStatusDelay = orig })

	// Server that stalls for 100 ms before writing headers, then returns a
	// minimal SSE stream with one token so the backend parses cleanly.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(100 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)

	var mu sync.Mutex
	var events []StreamEvent

	_, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hello"}},
		OnEvent: func(ev StreamEvent) {
			mu.Lock()
			events = append(events, ev)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion returned unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(events) == 0 {
		t.Fatal("expected at least one StreamEvent, got none")
	}

	// Status fires synchronously after client.Do returns, before parseSSE starts.
	// It is always the first event — this is a deterministic guarantee, not a race.
	if events[0].Type != StreamStatus {
		t.Errorf("expected first event type %q (synchronous, before parseSSE), got %q; events: %v",
			StreamStatus, events[0].Type, events)
	}
	if events[0].Content == "" {
		t.Error("StreamStatus event must have non-empty Content")
	}

	// At least one StreamText event must follow.
	hasText := false
	for _, ev := range events[1:] {
		if ev.Type == StreamText {
			hasText = true
			break
		}
	}
	if !hasText {
		t.Error("expected at least one StreamText event after StreamStatus, got none")
	}
}

// TestExternalBackend_StatusEvent_NotEmittedOnFastHeaders verifies that no
// StreamStatus event is emitted when headers arrive before
// externalModelLoadStatusDelay (the common case for warm-cached models and
// remote OpenAI-compatible endpoints).
func TestExternalBackend_StatusEvent_NotEmittedOnFastHeaders(t *testing.T) {
	t.Parallel()

	// Set a very long delay so the goroutine never fires within the test.
	orig := externalModelLoadStatusDelay
	externalModelLoadStatusDelay = 10 * time.Second
	t.Cleanup(func() { externalModelLoadStatusDelay = orig })

	// Server that responds immediately.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"fast\"}}]}\n\ndata: [DONE]\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)

	var mu sync.Mutex
	var events []StreamEvent

	_, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hello"}},
		OnEvent: func(ev StreamEvent) {
			mu.Lock()
			events = append(events, ev)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion returned unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	for _, ev := range events {
		if ev.Type == StreamStatus {
			t.Errorf("expected no StreamStatus event for fast server, but got one: %q", ev.Content)
		}
	}
}
