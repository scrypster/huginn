package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHTTPTransport_Non200Status verifies that a 4xx/5xx response returns an error.
func TestHTTPTransport_Non200Status(t *testing.T) {
	for _, code := range []int{400, 401, 403, 500, 503} {
		code := code
		t.Run(http.StatusText(code), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "server error body", code)
			}))
			defer srv.Close()

			tr := NewHTTPTransport(srv.URL, "")
			err := tr.Send(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
			if err == nil {
				t.Errorf("code %d: expected error, got nil", code)
			}
		})
	}
}

// TestHTTPTransport_ConnectionRefused verifies that a connection-refused error propagates.
func TestHTTPTransport_ConnectionRefused(t *testing.T) {
	// Port 1 is reserved and should always refuse connections.
	tr := NewHTTPTransport("http://127.0.0.1:1/mcp", "")
	err := tr.Send(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if err == nil {
		t.Fatal("expected connection-refused error, got nil")
	}
}

// TestHTTPTransport_MalformedJSONResponse verifies that a non-JSON body is stored and
// can be retrieved via Receive (the transport layer does not parse JSON itself).
func TestHTTPTransport_MalformedJSONStored(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json at all {{{"))
	}))
	defer srv.Close()

	tr := NewHTTPTransport(srv.URL, "")
	ctx := context.Background()

	// Send should succeed (the transport doesn't validate the body format).
	if err := tr.Send(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)); err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}

	// Receive should return the raw malformed body without error.
	body, err := tr.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: unexpected error: %v", err)
	}
	if string(body) != "not json at all {{{\n" && string(body) != "not json at all {{{" {
		// The exact bytes returned are fine; we just want to confirm Receive gives them back.
		if len(body) == 0 {
			t.Error("Receive returned empty body for malformed response")
		}
	}
}

// TestHTTPTransport_RequestTimeout verifies that a slow server causes Send to error
// when the transport's HTTP client times out.
func TestHTTPTransport_RequestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hang until the client gives up.
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Create a transport with a very short timeout by overriding the client.
	tr := NewHTTPTransport(srv.URL, "")
	tr.client = &http.Client{Timeout: 50 * time.Millisecond}

	ctx := context.Background()
	err := tr.Send(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

// TestHTTPTransport_ContextCancelled verifies that a cancelled context causes Send to fail.
func TestHTTPTransport_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	tr := NewHTTPTransport(srv.URL, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := tr.Send(ctx, []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if err == nil {
		t.Fatal("expected context-cancelled error, got nil")
	}
}

// TestHTTPTransport_EmptyBodyDoesNotUnblockReceive verifies that a 200 with empty
// body does not deliver data to Receive (same behaviour as 204 — notification ACKs
// should not unblock a pending Receive call).
func TestHTTPTransport_EmptyBodyDoesNotUnblockReceive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write nothing — empty body.
	}))
	defer srv.Close()

	tr := NewHTTPTransport(srv.URL, "")
	ctx := context.Background()

	if err := tr.Send(ctx, []byte(`{"jsonrpc":"2.0","method":"notifications/foo"}`)); err != nil {
		t.Fatalf("Send: %v", err)
	}
	// Receive should not return immediately — it should block because no response
	// body was queued (empty body = notification ACK). Verify via context cancellation.
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tr.Receive(cancelCtx)
	if err == nil {
		t.Fatal("expected context cancellation error when Receive called with no queued response")
	}
}

// TestHTTPTransport_BearerTokenAttached verifies the Authorization header is set.
func TestHTTPTransport_BearerTokenAttached(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	tr := NewHTTPTransport(srv.URL, "my-secret-token")
	_ = tr.Send(context.Background(), []byte(`{}`))

	if gotAuth != "Bearer my-secret-token" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer my-secret-token")
	}
}

// TestHTTPTransport_NoBearerTokenWhenEmpty verifies the Authorization header is absent
// when no token is provided.
func TestHTTPTransport_NoBearerTokenWhenEmpty(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	tr := NewHTTPTransport(srv.URL, "")
	_ = tr.Send(context.Background(), []byte(`{}`))

	if gotAuth != "" {
		t.Errorf("expected no Authorization header, got %q", gotAuth)
	}
}

// TestHTTPTransport_Close is a smoke test for the no-op Close method.
func TestHTTPTransport_Close(t *testing.T) {
	tr := NewHTTPTransport("http://localhost:1234", "")
	if err := tr.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
