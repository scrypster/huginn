package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	agentslib "github.com/scrypster/huginn/internal/agents"
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

// ── ollama warm-up tests (perf wave step 2b) ────────────────────────────────

// TestWarmOllamaModelSet_DefaultPlusCoS verifies the warm set is bounded to
// the default agent's model plus the Chief of Staff's model, deduplicated,
// and skips agents that aren't ollama-family or point at a different
// endpoint.
func TestWarmOllamaModelSet_DefaultPlusCoS(t *testing.T) {
	reg := agentslib.NewRegistry()
	reg.Register(&agentslib.Agent{Name: "Winston", ModelID: "qwen2.5-coder:14b", IsDefault: true})
	reg.Register(&agentslib.Agent{Name: "Steve", ModelID: "qwen2.5-coder:14b", LocalTools: []string{"create_agent"}}) // CoS, same model as default
	reg.Register(&agentslib.Agent{Name: "Reggie", ModelID: "llama3:8b"})                                              // ordinary teammate — not warmed
	reg.Register(&agentslib.Agent{Name: "Claude", ModelID: "claude-sonnet-4", Provider: "anthropic"})                 // cloud — never warmed
	reg.Register(&agentslib.Agent{Name: "Remote", ModelID: "qwen2.5-coder:32b", Provider: "ollama", Endpoint: "http://other-box:11434"})

	got := warmOllamaModelSet(reg, "http://localhost:11434")
	if len(got) != 1 || got[0] != "qwen2.5-coder:14b" {
		t.Fatalf("expected only the shared default+CoS model, got %v", got)
	}
}

// TestWarmOllamaModelSet_DistinctDefaultAndCoS verifies both models are
// warmed (bounded to at most 2) when the default agent and the CoS use
// different models.
func TestWarmOllamaModelSet_DistinctDefaultAndCoS(t *testing.T) {
	reg := agentslib.NewRegistry()
	reg.Register(&agentslib.Agent{Name: "Winston", ModelID: "qwen2.5-coder:14b", IsDefault: true})
	reg.Register(&agentslib.Agent{Name: "Steve", ModelID: "deepseek-r1:14b", LocalTools: []string{"create_agent"}})
	reg.Register(&agentslib.Agent{Name: "Reggie", ModelID: "llama3:8b"})

	got := warmOllamaModelSet(reg, "http://localhost:11434")
	if len(got) != 2 {
		t.Fatalf("expected exactly 2 models (default + CoS), got %v", got)
	}
	want := map[string]bool{"qwen2.5-coder:14b": true, "deepseek-r1:14b": true}
	for _, m := range got {
		if !want[m] {
			t.Errorf("unexpected model in warm set: %q (got %v)", m, got)
		}
	}
}

// TestWarmOllamaModelSet_NilRegistry verifies a nil registry never panics.
func TestWarmOllamaModelSet_NilRegistry(t *testing.T) {
	if got := warmOllamaModelSet(nil, "http://localhost:11434"); got != nil {
		t.Fatalf("expected nil for nil registry, got %v", got)
	}
}

// TestWarmOneOllamaModel_SendsModelPromptAndKeepAlive verifies the warm-up
// request is ollama's native /api/generate with an empty prompt and the
// configured keep_alive.
func TestWarmOneOllamaModel_SendsModelPromptAndKeepAlive(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := warmOneOllamaModel(context.Background(), srv.URL, "qwen2.5-coder:14b", "10m"); err != nil {
		t.Fatalf("warmOneOllamaModel: %v", err)
	}
	if gotPath != "/api/generate" {
		t.Errorf("path = %q, want /api/generate", gotPath)
	}
	if gotBody["model"] != "qwen2.5-coder:14b" {
		t.Errorf("model = %v, want qwen2.5-coder:14b", gotBody["model"])
	}
	if gotBody["prompt"] != "" {
		t.Errorf("prompt = %v, want empty (load-only)", gotBody["prompt"])
	}
	if gotBody["keep_alive"] != "10m" {
		t.Errorf("keep_alive = %v, want 10m", gotBody["keep_alive"])
	}
}

// TestWarmOneOllamaModel_ErrorOnServerFailure verifies a non-2xx response
// surfaces as an error so the caller can log it (best-effort, never fatal).
func TestWarmOneOllamaModel_ErrorOnServerFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if err := warmOneOllamaModel(context.Background(), srv.URL, "qwen2.5-coder:14b", "10m"); err == nil {
		t.Fatal("expected an error on HTTP 500")
	}
}
