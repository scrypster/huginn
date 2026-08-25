package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/modelconfig"
)

// TestHandleListAvailableModels_IncludesProviderModels verifies that when a cloud
// provider (Anthropic) is configured with an API key, GET /api/v1/models/available
// includes the provider's models so the agent model picker can display them.
func TestHandleListAvailableModels_IncludesProviderModels(t *testing.T) {
	// Stand up a mock Anthropic-compatible /v1/models endpoint.
	mockAnthropicSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"data":[
			{"type":"model","id":"claude-sonnet-4-6","display_name":"Claude Sonnet 4.6","created_at":"2026-01-01T00:00:00Z"},
			{"type":"model","id":"claude-haiku-4-5","display_name":"Claude Haiku 4.5","created_at":"2026-01-01T00:00:00Z"}
		]}`)
	}))
	defer mockAnthropicSrv.Close()

	srv, ts := newTestServer(t)

	// Configure the server to use the mock Anthropic endpoint.
	srv.cfg.Backend.Provider = "anthropic"
	srv.cfg.Backend.Endpoint = mockAnthropicSrv.URL
	srv.cfg.Backend.APIKey = "sk-ant-test-key"

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/models/available", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	rawProvider, ok := body["provider_models"]
	if !ok {
		t.Fatal("response missing 'provider_models' key — Anthropic models are not shown in the agent model picker (issue #30)")
	}

	var providerModels []map[string]any
	if err := json.Unmarshal(rawProvider, &providerModels); err != nil {
		t.Fatalf("decode provider_models: %v", err)
	}

	if len(providerModels) < 2 {
		t.Errorf("expected at least 2 provider models, got %d", len(providerModels))
	}
}

// TestHandleListAvailableModels_NoProviderModelsWhenUnconfigured verifies that
// when no cloud provider is configured, provider_models is absent or empty.
func TestHandleListAvailableModels_NoProviderModelsWhenUnconfigured(t *testing.T) {
	_, ts := newTestServer(t)

	req2, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/models/available", nil)
	req2.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// If present, must be empty.
	if raw, ok := body["provider_models"]; ok {
		var models []any
		if err := json.Unmarshal(raw, &models); err != nil {
			t.Fatalf("decode provider_models: %v", err)
		}
		if len(models) != 0 {
			t.Errorf("expected 0 provider models when unconfigured, got %d", len(models))
		}
	}

	// Existing keys must still be present.
	if _, ok := body["models"]; !ok {
		t.Error("response missing 'models' key")
	}
}

// TestHandleListAvailableModels_OllamaHangRespectsRequestContext verifies that
// when the Ollama server hangs and the request context is cancelled, the handler
// returns promptly instead of blocking until OS TCP timeout.
func TestHandleListAvailableModels_OllamaHangRespectsRequestContext(t *testing.T) {
	// Start an HTTP server that hangs forever — never writes a response.
	hangSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the test ends (server is closed).
		<-r.Context().Done()
	}))
	defer hangSrv.Close()

	srv, ts := newTestServer(t)
	// Point Huginn's Ollama base URL at the hanging server.
	srv.cfg.OllamaBaseURL = hangSrv.URL

	// Build a request with a short context timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/v1/models/available", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	elapsed := time.Since(start)

	// The request should complete (either with a response or a context error)
	// well within 500ms — not hang for minutes.
	if elapsed > 500*time.Millisecond {
		t.Errorf("handler took %v — expected <500ms; Ollama HTTP call may not respect request context", elapsed)
	}

	if err == nil {
		defer resp.Body.Close()
		// If a response came back, it should be 200 (with ollamaErr set internally).
		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	}
	// If err != nil it's because the context was cancelled before the response,
	// which is also acceptable — the key assertion is elapsed < 500ms.
}

func TestHandleListAvailableModels_IncludesToolCapabilities(t *testing.T) {
	fakeOllama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/tags":
			fmt.Fprint(w, `{"models":[
				{"name":"qwen2.5-coder:7b","details":{"parameter_size":"7.6B"}},
				{"name":"qwen2.5-coder:14b","details":{"parameter_size":"14.8B"}},
				{"name":"plain:latest","details":{"parameter_size":"1B"}}
			]}`)
		case "/api/show":
			var req struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			caps := []string{"completion"}
			if req.Name == "qwen2.5-coder:7b" || req.Name == "qwen2.5-coder:14b" {
				caps = append(caps, "tools")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"capabilities": caps,
				"details":      map[string]any{"family": "qwen2"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer fakeOllama.Close()

	reg := modelconfig.NewRegistry(modelconfig.DefaultModels())
	b := &stubBackend{}
	orch, err := agent.NewOrchestrator(b, modelconfig.DefaultModels(), nil, reg, nil, nil)
	if err != nil {
		t.Fatalf("orch: %v", err)
	}

	srv, _ := newTestServer(t)
	srv.cfg.OllamaBaseURL = fakeOllama.URL
	srv.orch = orch

	req := httptest.NewRequest(http.MethodGet, "/api/v1/models/available", nil)
	w := httptest.NewRecorder()
	srv.handleListAvailableModels(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byName := map[string]map[string]any{}
	for _, m := range result.Models {
		name, _ := m["name"].(string)
		byName[name] = m
	}

	seven := byName["qwen2.5-coder:7b"]
	if seven == nil {
		t.Fatal("missing 7b in response")
	}
	if seven["supportsTools"] != true {
		t.Errorf("7b supportsTools = %v, live Ollama advertises tools", seven["supportsTools"])
	}
	if seven["supportsDelegation"] != false {
		t.Errorf("7b supportsDelegation = %v, want false", seven["supportsDelegation"])
	}
	if seven["tier"] != "low" {
		t.Errorf("7b tier = %v, want low", seven["tier"])
	}

	fourteen := byName["qwen2.5-coder:14b"]
	if fourteen["supportsTools"] != true {
		t.Errorf("14b supportsTools = %v, want true", fourteen["supportsTools"])
	}
	if fourteen["supportsDelegation"] != true {
		t.Errorf("14b supportsDelegation = %v, want true", fourteen["supportsDelegation"])
	}
	if fourteen["tier"] != "medium" {
		t.Errorf("14b tier = %v, want medium", fourteen["tier"])
	}

	if byName["plain:latest"]["supportsTools"] != false {
		t.Errorf("plain supportsTools = %v, want false", byName["plain:latest"]["supportsTools"])
	}

	if !reg.ModelSupportsTools("qwen2.5-coder:7b") {
		t.Error("registry should record probed 7b SupportsTools=true")
	}
	if reg.ModelSupportsTools("plain:latest") {
		t.Error("registry should record probed plain SupportsTools=false")
	}
}
