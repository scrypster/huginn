package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleMuninnTest_MissingConfig(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/muninn/test",
		strings.NewReader(`{"endpoint":"http://localhost:8475","username":"root","password":"password"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Should return 200 with ok:false or 200 with ok:true — just no 500.
	if resp.StatusCode == http.StatusInternalServerError {
		t.Errorf("unexpected 500: status %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["ok"]; !ok {
		t.Error("expected 'ok' field in response")
	}
}

func TestHandleMuninnVaults_ReturnsKnownVaults(t *testing.T) {
	srv, ts := newTestServer(t)
	// Point muninn config to a temp dir so we get an empty but valid config.
	srv.muninnCfgPath = filepath.Join(t.TempDir(), "muninn.json")

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/muninn/vaults", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["vaults"]; !ok {
		t.Error("expected 'vaults' field in response")
	}
}

func TestHandleMuninnStatus_NotConnected(t *testing.T) {
	srv, ts := newTestServer(t)
	// Point muninn config to a temp dir (empty config = not connected).
	srv.muninnCfgPath = filepath.Join(t.TempDir(), "muninn.json")

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/muninn/status", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	connected, ok := body["connected"]
	if !ok {
		t.Fatal("expected 'connected' field in response")
	}
	if connected != false {
		t.Errorf("expected connected=false for empty config, got %v", connected)
	}
}

func TestHandleMuninnVaultCreate_NotConfigured(t *testing.T) {
	srv, ts := newTestServer(t)
	// Point muninn config to a temp dir (empty config = no endpoint).
	srv.muninnCfgPath = filepath.Join(t.TempDir(), "muninn.json")

	body := `{"vault_name":"huginn-test","agent_label":"huginn-agent"}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/muninn/vaults", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Should return 400 because MuninnDB is not configured.
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	var respBody map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := respBody["error"]; !ok {
		t.Error("expected 'error' field in response")
	}
}

func TestHandleMuninnVaultCreate_MissingFields(t *testing.T) {
	_, ts := newTestServer(t)
	body := `{"vault_name":"","agent_label":""}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/muninn/vaults", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestHandleMuninnTest_InvalidJSON(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/muninn/test",
		strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusInternalServerError {
		t.Errorf("unexpected 500: status %d", resp.StatusCode)
	}
	var respBody map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ok, _ := respBody["ok"].(bool); ok {
		t.Error("expected ok:false for invalid JSON request")
	}
}

func TestHandleMuninnConnect_InvalidJSON(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/muninn/connect",
		strings.NewReader(`not-json`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Bad JSON => 400 Bad Request.
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestHandleMuninnTest_Unauthenticated(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/muninn/test",
		strings.NewReader(`{"endpoint":"http://localhost:8475","username":"root","password":"password"}`))
	req.Header.Set("Content-Type", "application/json")
	// No Authorization header.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}
