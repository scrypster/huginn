package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/secrets"
)

// newTestSecretsManager creates an in-memory secrets manager backed by a temp
// file store so tests never touch the OS keychain or ~/.huginn/secrets.json.
func newTestSecretsManager(t *testing.T) *secrets.Manager {
	t.Helper()
	mem := secrets.NewMemoryKeyring()
	fs := secrets.NewFileStore(filepath.Join(t.TempDir(), "secrets.json"))
	return secrets.NewManager(mem, fs)
}

// TestHandleSetSecret_LLMSlot_UpdatesFallbackAPIKey is the regression test for
// the 401 "x-api-key header is required" bug:
//
// Root cause: BackendCache.fallbackAPIKey was never updated after the user
// stored their API key via the web UI. Agents with provider="anthropic" but
// no per-agent api_key inherited the stale empty fallbackAPIKey and sent
// requests without an Authorization header.
//
// Fix: handleSetSecret now calls s.orch.UpdateFallbackAPIKey(ref) for LLM
// provider slots, which atomically updates WithFallbackAPIKey + InvalidateCache.
func TestHandleSetSecret_LLMSlot_UpdatesFallbackAPIKey(t *testing.T) {
	mgr := newTestSecretsManager(t)
	secrets.SetDefault(mgr)
	t.Cleanup(func() { secrets.SetDefault(nil) })

	srv, ts := newTestServer(t)

	// Wire a BackendCache with an empty fallback key (server-startup state).
	cache := backend.NewBackendCache(nil)
	srv.orch.SetBackendCache(cache)

	// Prime the cache: For("anthropic", ...) with empty fallback key.
	// This is the stale backend that caused 401 before the fix.
	preKeyBackend, err := cache.For("anthropic", "", "", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("pre-key For(): %v", err)
	}

	// PUT /api/v1/secrets/anthropic with a new API key.
	body, _ := json.Marshal(map[string]string{"value": "sk-ant-test-key"})
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/secrets/anthropic", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/v1/secrets/anthropic: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["slot"] != "anthropic" {
		t.Errorf("slot = %v, want anthropic", result["slot"])
	}
	storage, _ := result["storage"].(string)
	if storage == "" {
		t.Error("expected non-empty storage field in response")
	}

	// Assert cfg.Backend.APIKey was updated to the keyring reference.
	srv.mu.Lock()
	apiKey := srv.cfg.Backend.APIKey
	srv.mu.Unlock()
	if apiKey != "keyring:huginn:anthropic" {
		t.Errorf("cfg.Backend.APIKey = %q, want keyring:huginn:anthropic", apiKey)
	}

	// Assert the BackendCache fallback key was updated (indirect check):
	// A new For("anthropic", ...) must produce a DIFFERENT backend because
	// InvalidateCache() evicted the stale entry and WithFallbackAPIKey() changed
	// the key reference used when building the new cached backend.
	postKeyBackend, err := cache.For("anthropic", "", "", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("post-key For(): %v", err)
	}
	if preKeyBackend == postKeyBackend {
		t.Error("expected different backend instance after UpdateFallbackAPIKey — " +
			"cache was not invalidated (stale empty-key backend would cause 401)")
	}
}

// TestHandleSetSecret_OpenAISlot_UpdatesFallbackAPIKey verifies that the
// openai slot also triggers UpdateFallbackAPIKey.
func TestHandleSetSecret_OpenAISlot_UpdatesFallbackAPIKey(t *testing.T) {
	mgr := newTestSecretsManager(t)
	secrets.SetDefault(mgr)
	t.Cleanup(func() { secrets.SetDefault(nil) })

	srv, ts := newTestServer(t)

	cache := backend.NewBackendCache(nil)
	srv.orch.SetBackendCache(cache)

	preBackend, _ := cache.For("openai", "", "", "gpt-4o")

	body, _ := json.Marshal(map[string]string{"value": "sk-openai-test"})
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/secrets/openai", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/v1/secrets/openai: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Cache should have been invalidated.
	postBackend, _ := cache.For("openai", "", "", "gpt-4o")
	if preBackend == postBackend {
		t.Error("expected cache invalidation after setting openai secret")
	}
}

// TestHandleSetSecret_NonLLMSlot_DoesNotInvalidateCache verifies that setting
// a non-LLM slot (e.g. "brave") does NOT call UpdateFallbackAPIKey and
// therefore does not needlessly evict cached LLM backends.
func TestHandleSetSecret_NonLLMSlot_DoesNotInvalidateCache(t *testing.T) {
	mgr := newTestSecretsManager(t)
	secrets.SetDefault(mgr)
	t.Cleanup(func() { secrets.SetDefault(nil) })

	srv, ts := newTestServer(t)

	cache := backend.NewBackendCache(nil)
	srv.orch.SetBackendCache(cache)

	// Prime the anthropic cache entry.
	preBackend, err := cache.For("anthropic", "", "", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("pre-key For(): %v", err)
	}

	// PUT /api/v1/secrets/brave — not an LLM slot.
	body, _ := json.Marshal(map[string]string{"value": "brave-api-key-value"})
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/secrets/brave", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/v1/secrets/brave: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// The anthropic backend should be the same cached instance.
	postBackend, err := cache.For("anthropic", "", "", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("post-brave For(): %v", err)
	}
	if preBackend != postBackend {
		t.Error("expected same cached anthropic backend after non-LLM secret update " +
			"(cache should not be invalidated by non-LLM slots)")
	}
}

// TestHandleSetSecret_UnknownSlot_Returns400 verifies that an unknown slot is
// rejected immediately.
func TestHandleSetSecret_UnknownSlot_Returns400(t *testing.T) {
	mgr := newTestSecretsManager(t)
	secrets.SetDefault(mgr)
	t.Cleanup(func() { secrets.SetDefault(nil) })

	_, ts := newTestServer(t)

	body, _ := json.Marshal(map[string]string{"value": "some-value"})
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/secrets/unknownslot", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/v1/secrets/unknownslot: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// TestHandleSetSecret_EmptyValue_Returns400 verifies that an empty value is
// rejected with 400.
func TestHandleSetSecret_EmptyValue_Returns400(t *testing.T) {
	mgr := newTestSecretsManager(t)
	secrets.SetDefault(mgr)
	t.Cleanup(func() { secrets.SetDefault(nil) })

	_, ts := newTestServer(t)

	body, _ := json.Marshal(map[string]string{"value": ""})
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/secrets/anthropic", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT /api/v1/secrets/anthropic: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}
