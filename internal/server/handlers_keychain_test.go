package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestHandleUpdateConfig_LiteralAPIKey_StoredInKeychain verifies that when a
// literal API key is submitted via PUT /api/v1/config, handleUpdateConfig routes
// it through storeAPIKey so it lands in the OS keychain — not in plaintext inside
// config.json. The in-memory config must contain the keyring reference, not the
// raw secret.
func TestHandleUpdateConfig_LiteralAPIKey_StoredInKeychain(t *testing.T) {
	srv, ts := newTestServer(t)

	payload := `{"version":1,"web_ui":{"port":0},"backend":{"provider":"anthropic","api_key":"sk-ant-test-literal-key"}}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/config", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	if body["saved"] != true {
		t.Errorf("expected saved=true, got %v", body["saved"])
	}

	// The in-memory config must hold the keyring reference, not the literal key.
	// newTestServer installs a keyStorerFn that returns "keyring:huginn:<slot>".
	srv.mu.Lock()
	storedKey := srv.cfg.Backend.APIKey
	srv.mu.Unlock()

	if storedKey == "sk-ant-test-literal-key" {
		t.Errorf("API key stored as plaintext literal — expected keyring reference; "+
			"literal keys must be migrated to OS keychain via storeAPIKey (got %q)", storedKey)
	}
	if storedKey != "keyring:huginn:anthropic" {
		t.Errorf("expected keyring ref %q, got %q", "keyring:huginn:anthropic", storedKey)
	}
}

// TestHandleUpdateConfig_KeyringRef_NotDoubleStored verifies that a config payload
// that already contains a keyring reference is NOT re-stored (idempotent).
func TestHandleUpdateConfig_KeyringRef_NotDoubleStored(t *testing.T) {
	srv, ts := newTestServer(t)

	// Pre-set in-memory key so we can detect mutation.
	srv.mu.Lock()
	srv.cfg.Backend.APIKey = "keyring:huginn:anthropic"
	srv.mu.Unlock()

	var storerCalled bool
	srv.keyStorerFn = func(slot, value string) (string, error) {
		storerCalled = true
		return "keyring:huginn:" + slot, nil
	}

	payload := `{"version":1,"web_ui":{"port":0},"backend":{"provider":"anthropic","api_key":"keyring:huginn:anthropic"}}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/config", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if storerCalled {
		t.Error("storeAPIKey must not be called when the key is already a keyring reference")
	}
}
