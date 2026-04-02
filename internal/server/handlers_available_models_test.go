package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
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
