package search

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestOllamaEmbedder_DefaultTimeout verifies that Embed returns an error
// when the server takes longer than the configured timeout.
// Uses a raw TCP server that accepts then hangs to avoid httptest.Server cleanup issues.
func TestOllamaEmbedder_DefaultTimeout(t *testing.T) {
	// Use a TCP listener that accepts connections but never sends HTTP responses.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	// Accept connections and immediately discard them (simulates a stalled server).
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			// Hold the connection open until the listener is closed.
			go func(c net.Conn) {
				buf := make([]byte, 1)
				c.Read(buf) //nolint:errcheck
				c.Close()
			}(conn)
		}
	}()
	defer ln.Close()

	addr := ln.Addr().String()
	e := NewOllamaEmbedder("http://"+addr, "nomic-embed-text")
	// Short timeout so the test finishes quickly.
	e.Timeout = 100 * time.Millisecond

	start := time.Now()
	_, err = e.Embed(context.Background(), "hello world")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if elapsed > 5*time.Second {
		t.Errorf("Embed took %v — did not respect Timeout", elapsed)
	}
}

// TestOllamaEmbedder_ConfigurableTimeout verifies that Timeout=0 falls back to
// defaultEmbeddingTimeout (not zero / no timeout).
func TestOllamaEmbedder_ConfigurableTimeout(t *testing.T) {
	e := NewOllamaEmbedder("http://localhost:0", "nomic-embed-text")
	if e.Timeout != 0 {
		t.Errorf("expected Timeout to be 0 (unset) after NewOllamaEmbedder, got %v", e.Timeout)
	}
	if defaultEmbeddingTimeout != 5*time.Second {
		t.Errorf("defaultEmbeddingTimeout: expected 5s, got %v", defaultEmbeddingTimeout)
	}
}

// TestOllamaEmbedder_RespectsCallerContext verifies that a cancelled parent
// context terminates the embed call even if the inner timeout hasn't fired.
func TestOllamaEmbedder_RespectsCallerContext(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				buf := make([]byte, 1)
				c.Read(buf) //nolint:errcheck
				c.Close()
			}(conn)
		}
	}()
	defer ln.Close()

	e := NewOllamaEmbedder("http://"+ln.Addr().String(), "nomic-embed-text")
	e.Timeout = 10 * time.Second // longer than the caller context timeout

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = e.Embed(ctx, "test")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if elapsed > 5*time.Second {
		t.Errorf("Embed did not respect caller context cancellation: took %v", elapsed)
	}
}

// TestOllamaEmbedder_SuccessfulEmbed verifies a successful round-trip.
func TestOllamaEmbedder_SuccessfulEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"embedding":[0.1,0.2,0.3]}`))
	}))
	defer srv.Close()

	e := NewOllamaEmbedder(srv.URL, "nomic-embed-text")
	e.Timeout = 2 * time.Second

	vec, err := e.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 3 {
		t.Errorf("expected 3-element embedding, got %d", len(vec))
	}
}
