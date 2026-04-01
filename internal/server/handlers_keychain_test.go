package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestHandleProviderModels_KeyringResolution verifies that when the backend API
// key is stored as a keyring reference ("keyring:huginn:anthropic"), the server
// correctly resolves it from the keychain before calling the provider API —
// not passing the reference string literally (which causes a 401).
// All Anthropic calls go to a mock server; no real API key is used.
func TestHandleProviderModels_KeyringResolution(t *testing.T) {
	keyring.MockInit()
	const realKey = "sk-ant-test-resolved-key"
	if err := keyring.Set("huginn", "anthropic", realKey); err != nil {
		t.Fatalf("keyring.Set: %v", err)
	}

	var receivedKey string
	mockAnthropicSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey = r.Header.Get("x-api-key")
		fmt.Fprint(w, `{"data":[{"type":"model","id":"claude-sonnet-4-6","display_name":"Claude Sonnet 4.6","created_at":"2026-01-01T00:00:00Z"}],"has_more":false}`)
	}))
	defer mockAnthropicSrv.Close()

	srv, ts := newTestServer(t)
	srv.cfg.Backend.Provider = "anthropic"
	srv.cfg.Backend.Endpoint = mockAnthropicSrv.URL
	// Store the keyring reference — not the literal key.
	srv.cfg.Backend.APIKey = "keyring:huginn:anthropic"

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/providers/anthropic/models", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if receivedKey != realKey {
		t.Errorf("Anthropic API received key %q, want %q — keyring reference was not resolved", receivedKey, realKey)
	}
}

// TestHandleListAvailableModels_KeyringResolution verifies the same keyring
// resolution for the agent model picker endpoint (/api/v1/models/available).
// All Anthropic calls go to a mock server; no real API key is used.
func TestHandleListAvailableModels_KeyringResolution(t *testing.T) {
	keyring.MockInit()
	const realKey = "sk-ant-agent-picker-key"
	if err := keyring.Set("huginn", "anthropic", realKey); err != nil {
		t.Fatalf("keyring.Set: %v", err)
	}

	var receivedKey string
	mockAnthropicSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey = r.Header.Get("x-api-key")
		fmt.Fprint(w, `{"data":[{"type":"model","id":"claude-sonnet-4-6","display_name":"Claude Sonnet 4.6","created_at":"2026-01-01T00:00:00Z"}],"has_more":false}`)
	}))
	defer mockAnthropicSrv.Close()

	srv, ts := newTestServer(t)
	srv.cfg.Backend.Provider = "anthropic"
	srv.cfg.Backend.Endpoint = mockAnthropicSrv.URL
	srv.cfg.Backend.APIKey = "keyring:huginn:anthropic"

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
	if receivedKey != realKey {
		t.Errorf("Anthropic API received key %q, want %q — keyring reference was not resolved in agent picker", receivedKey, realKey)
	}
}

// TestHandleProviderModels_AnthropicFallbackOnAPIError verifies that when the
// Anthropic API returns an auth error, the models page still returns the
// hardcoded known models rather than an empty error response.
// No real API key is used — mock server returns 401.
func TestHandleProviderModels_AnthropicFallbackOnAPIError(t *testing.T) {
	mockAnthropicSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
	}))
	defer mockAnthropicSrv.Close()

	srv, ts := newTestServer(t)
	srv.cfg.Backend.Provider = "anthropic"
	srv.cfg.Backend.Endpoint = mockAnthropicSrv.URL
	srv.cfg.Backend.APIKey = "sk-ant-any-key"

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/providers/anthropic/models", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 fallback response, got %d", resp.StatusCode)
	}

	var models []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&models); err != nil {
		t.Fatalf("decode models: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("expected known fallback models when API fails, got empty list")
	}
	found := false
	for _, m := range models {
		if m["id"] == "claude-opus-4-6" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected claude-opus-4-6 in fallback models, not found")
	}
}

// TestHandleListAvailableModels_AnthropicFallbackOnAPIError verifies the same
// fallback for the agent model picker when the Anthropic API is unreachable.
// No real API key is used — mock server returns 500.
func TestHandleListAvailableModels_AnthropicFallbackOnAPIError(t *testing.T) {
	mockAnthropicSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer mockAnthropicSrv.Close()

	srv, ts := newTestServer(t)
	srv.cfg.Backend.Provider = "anthropic"
	srv.cfg.Backend.Endpoint = mockAnthropicSrv.URL
	srv.cfg.Backend.APIKey = "sk-ant-any-key"

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
	json.NewDecoder(resp.Body).Decode(&body)
	var providerModels []map[string]any
	json.Unmarshal(body["provider_models"], &providerModels)

	if len(providerModels) == 0 {
		t.Fatal("expected known fallback models in agent picker when API fails, got empty list")
	}
}

// TestHandleProviderModels_AnthropicPagination verifies that the models fetcher
// follows has_more pagination and returns models from all pages.
// No real API key is used — all calls go to a mock server.
func TestHandleProviderModels_AnthropicPagination(t *testing.T) {
	page1Called, page2Called := false, false

	mockAnthropicSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		afterID := r.URL.Query().Get("after_id")
		if afterID == "" {
			page1Called = true
			fmt.Fprint(w, `{"data":[{"type":"model","id":"claude-opus-4-6","display_name":"Claude Opus 4.6","created_at":"2026-01-01T00:00:00Z"}],"has_more":true,"last_id":"claude-opus-4-6"}`)
		} else {
			page2Called = true
			fmt.Fprint(w, `{"data":[{"type":"model","id":"claude-haiku-4-5-20251001","display_name":"Claude Haiku 4.5","created_at":"2025-10-01T00:00:00Z"}],"has_more":false,"last_id":"claude-haiku-4-5-20251001"}`)
		}
	}))
	defer mockAnthropicSrv.Close()

	srv, ts := newTestServer(t)
	srv.cfg.Backend.Provider = "anthropic"
	srv.cfg.Backend.Endpoint = mockAnthropicSrv.URL
	srv.cfg.Backend.APIKey = "sk-ant-test"

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/providers/anthropic/models", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if !page1Called {
		t.Error("first page was not fetched")
	}
	if !page2Called {
		t.Error("second page was not fetched — pagination not followed")
	}
}
