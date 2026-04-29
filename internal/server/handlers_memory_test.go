package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/memory"
)

func TestHandleReplicationStatusNoDB(t *testing.T) {
	_, ts := newTestServer(t)
	// No DB wired — should return 200 with zeroed counts, not 500.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/memory/replication-status", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, field := range []string{"pending", "failed", "dead", "connected"} {
		if _, ok := body[field]; !ok {
			t.Errorf("response missing %q field", field)
		}
	}
	if connected, _ := body["connected"].(bool); connected {
		t.Error("expected connected=false when no DB wired")
	}
}

func TestHandleMuninnToolRejectsUnknownTool(t *testing.T) {
	_, ts := newTestServer(t)
	body := `{"vault":"huginn:agent:user:alice","tool":"muninn_evil","args":{}}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/muninn/tool",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-whitelisted tool, got %d", resp.StatusCode)
	}
}

func TestHandleMuninnToolRejectsMissingFields(t *testing.T) {
	_, ts := newTestServer(t)
	body := `{"vault":"","tool":""}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/muninn/tool",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing fields, got %d", resp.StatusCode)
	}
}

func TestAllowedMuninnTools_IncludesMuninnRemember(t *testing.T) {
	if !allowedMuninnTools["muninn_remember"] {
		t.Error("allowedMuninnTools must include muninn_remember for user-initiated Save to Memory")
	}
}

func TestHandleMuninnTool_NotConfigured(t *testing.T) {
	_, ts := newTestServer(t)
	body := `{"vault":"huginn:agent:user:alice","tool":"muninn_recall","args":{"context":["hello"]}}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/muninn/tool", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when MuninnDB is not configured, got %d", resp.StatusCode)
	}
}

func TestHandleMuninnTool_NoTokenForVault(t *testing.T) {
	srv, ts := newTestServer(t)
	cfgPath := filepath.Join(t.TempDir(), "muninn.json")
	if err := memory.SaveGlobalConfig(cfgPath, &memory.GlobalConfig{
		Endpoint:    "http://localhost:8475",
		Username:    "root",
		VaultTokens: map[string]string{},
	}); err != nil {
		t.Fatalf("save muninn config: %v", err)
	}
	srv.muninnCfgPath = cfgPath

	body := `{"vault":"huginn:agent:user:alice","tool":"muninn_recall","args":{"context":["hello"]}}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/muninn/tool", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for missing vault token, got %d", resp.StatusCode)
	}
}

func TestHandleMuninnTool_BadEndpoint(t *testing.T) {
	srv, ts := newTestServer(t)
	cfgPath := filepath.Join(t.TempDir(), "muninn.json")
	if err := memory.SaveGlobalConfig(cfgPath, &memory.GlobalConfig{
		Endpoint: "://bad-endpoint",
		Username: "root",
		VaultTokens: map[string]string{
			"huginn:agent:user:alice": "mk_test_token",
		},
	}); err != nil {
		t.Fatalf("save muninn config: %v", err)
	}
	srv.muninnCfgPath = cfgPath

	body := `{"vault":"huginn:agent:user:alice","tool":"muninn_recall","args":{"context":["hello"]}}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/muninn/tool", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for invalid endpoint URL, got %d", resp.StatusCode)
	}
}
