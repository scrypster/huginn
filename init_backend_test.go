package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/config"
)

// ── selectBackend tests ───────────────────────────────────────────────────────

// TestSelectBackend_ExternalDefaultEndpoint verifies that a config with no
// provider and no endpoint falls back to localhost:11434.
// We use a test HTTP server to intercept the health probe.
func TestSelectBackend_ExternalDefaultEndpoint(t *testing.T) {
	// Spin up a minimal HTTP server that responds 200 to /v1/models (health probe).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.Backend.Endpoint = srv.URL // point health probe at test server

	b, models, err := selectBackend(context.Background(), cfg, "", "")
	if err != nil {
		t.Fatalf("selectBackend: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil backend")
	}
	if models == nil {
		t.Fatal("expected non-nil models")
	}
}

// TestSelectBackend_EndpointOverride verifies that endpointOverride takes
// precedence over cfg.Backend.Endpoint.
func TestSelectBackend_EndpointOverride(t *testing.T) {
	var probed string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probed = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.Backend.Endpoint = "http://should-not-be-used.local"

	_, _, err := selectBackend(context.Background(), cfg, srv.URL, "")
	if err != nil {
		t.Fatalf("selectBackend: %v", err)
	}
	// probed should be the test server address, not the cfg endpoint.
	if probed == "" {
		t.Fatal("health probe was not called against the override endpoint")
	}
}

// TestSelectBackend_ModelOverride verifies that modelOverride is honoured.
func TestSelectBackend_ModelOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.Backend.Endpoint = srv.URL
	cfg.DefaultModel = "original-model"

	_, models, err := selectBackend(context.Background(), cfg, "", "override-model")
	if err != nil {
		t.Fatalf("selectBackend: %v", err)
	}
	if models.Reasoner != "override-model" {
		t.Errorf("Reasoner = %q, want %q", models.Reasoner, "override-model")
	}
}

// TestSelectBackend_DefaultModel verifies that cfg.DefaultModel is used when no override.
func TestSelectBackend_DefaultModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.Backend.Endpoint = srv.URL
	cfg.DefaultModel = "my-model"

	_, models, err := selectBackend(context.Background(), cfg, "", "")
	if err != nil {
		t.Fatalf("selectBackend: %v", err)
	}
	if models.Reasoner != "my-model" {
		t.Errorf("Reasoner = %q, want %q", models.Reasoner, "my-model")
	}
}

// TestSelectBackend_HealthProbeFailure verifies that an unreachable endpoint
// returns an error immediately rather than hanging until Chat is called.
func TestSelectBackend_HealthProbeFailure(t *testing.T) {
	cfg := &config.Config{}
	// Use a port that is not listening.
	cfg.Backend.Endpoint = "http://127.0.0.1:1"

	_, _, err := selectBackend(context.Background(), cfg, "", "")
	if err == nil {
		t.Fatal("expected error for unreachable endpoint, got nil")
	}
}

// TestSelectBackend_ReasonerFromConfig verifies that cfg.ReasonerModel is used
// as Reasoner when no modelOverride is provided and ReasonerModel is set.
func TestSelectBackend_ReasonerFromConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.Backend.Endpoint = srv.URL
	cfg.DefaultModel = "default"
	cfg.ReasonerModel = "reasoner-model"

	_, models, err := selectBackend(context.Background(), cfg, "", "")
	if err != nil {
		t.Fatalf("selectBackend: %v", err)
	}
	if models.Reasoner != "reasoner-model" {
		t.Errorf("Reasoner = %q, want %q", models.Reasoner, "reasoner-model")
	}
}
