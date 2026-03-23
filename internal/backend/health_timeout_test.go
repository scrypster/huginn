package backend_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
)

func TestAnthropicBackend_Health_ContextTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("test-key"), "claude-sonnet-4-6", srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := b.Health(ctx)
	if err == nil {
		t.Fatal("expected error on context timeout")
	}
}

func TestExternalBackend_Health_ContextTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	b := backend.NewExternalBackend(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := b.Health(ctx)
	if err == nil {
		t.Fatal("expected error on context timeout")
	}
}

func TestOpenRouterBackend_Health_ContextTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	b := backend.NewOpenRouterBackendWithEndpoint(backend.NewKeyResolver("test-key"), "anthropic/claude-sonnet-4-6", srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := b.Health(ctx)
	if err == nil {
		t.Fatal("expected error on context timeout")
	}
}
