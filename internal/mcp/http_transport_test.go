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
	_, err := tr.Receive(context.Background())
	if err == nil {
		t.Fatal("expected error when Receive called without prior Send")
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
	_, err := tr.Receive(ctx)
	if err == nil {
		t.Fatal("expected Receive to error after notification-only Send (pending should be nil)")
	}
}

func TestDefaultClientFactoryHTTPMissingURL(t *testing.T) {
	_, _, err := defaultClientFactory(context.Background(), MCPServerConfig{Name: "test", Transport: "http"})
	if err == nil {
		t.Fatal("expected error for http with missing URL")
	}
}
