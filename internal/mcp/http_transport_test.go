package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPTransportRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("missing bearer token, got: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer srv.Close()

	tr := NewHTTPTransport(srv.URL, "test-token")
	ctx := context.Background()

	msg := []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	if err := tr.Send(ctx, msg); err != nil {
		t.Fatalf("Send: %v", err)
	}
	resp, err := tr.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(resp, &got); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if got["result"] != "ok" {
		t.Fatalf("expected result=ok, got %v", got)
	}
}

func TestHTTPTransportReceiveWithoutSend(t *testing.T) {
	tr := NewHTTPTransport("http://localhost:9999", "tok")
	// Receive now blocks until Send provides data. Use a cancelled context
	// to verify it unblocks on context cancellation when no Send has been called.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	_, err := tr.Receive(ctx)
	if err == nil {
		t.Fatal("expected error when Receive called with cancelled context")
	}
}

func TestHTTPTransportNotificationNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	tr := NewHTTPTransport(srv.URL, "")
	ctx := context.Background()
	if err := tr.Send(ctx, []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)); err != nil {
		t.Fatalf("Send notification: %v", err)
	}
	// After a 204 notification ACK, no response is queued. Receive should block
	// (not error immediately). Verify via context cancellation.
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tr.Receive(cancelCtx)
	if err == nil {
		t.Fatal("expected Receive to unblock via context cancellation when no response is queued")
	}
}

func TestDefaultClientFactoryHTTPMissingURL(t *testing.T) {
	_, _, err := defaultClientFactory(context.Background(), MCPServerConfig{Name: "test", Transport: "http"})
	if err == nil {
		t.Fatal("expected error for http with missing URL")
	}
}
