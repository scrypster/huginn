package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/memory"
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
	t.Setenv("HOME", t.TempDir())
	srv, ts := newTestServer(t)
	// Point muninn config to a temp dir (empty config = no endpoint / token).
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

func TestHandleMuninnStatus_ConfiguredConnected(t *testing.T) {
	srv, ts := newTestServer(t)
	cfgPath := filepath.Join(t.TempDir(), "muninn.json")
	if err := memory.SaveGlobalConfig(cfgPath, &memory.GlobalConfig{
		Endpoint: "http://muninn.example",
		Username: "root",
	}); err != nil {
		t.Fatalf("save muninn config: %v", err)
	}
	srv.muninnCfgPath = cfgPath

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
	if body["connected"] != true {
		t.Fatalf("connected = %v, want true", body["connected"])
	}
	if body["endpoint"] != "http://muninn.example" {
		t.Fatalf("endpoint = %v, want http://muninn.example", body["endpoint"])
	}
}

func TestHandleMuninnVaultsList_SortsVaultNames(t *testing.T) {
	srv, ts := newTestServer(t)
	cfgPath := filepath.Join(t.TempDir(), "muninn.json")
	if err := memory.SaveGlobalConfig(cfgPath, &memory.GlobalConfig{
		Endpoint: "http://muninn.example",
		Username: "root",
		VaultTokens: map[string]string{
			"zeta-vault":  "mk-z",
			"alpha-vault": "mk-a",
		},
	}); err != nil {
		t.Fatalf("save muninn config: %v", err)
	}
	srv.muninnCfgPath = cfgPath

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
	vaults, ok := body["vaults"].([]any)
	if !ok || len(vaults) != 2 {
		t.Fatalf("vaults = %v, want 2 sorted entries", body["vaults"])
	}
	if vaults[0] != "alpha-vault" || vaults[1] != "zeta-vault" {
		t.Fatalf("vault order = %v, want [alpha-vault zeta-vault]", vaults)
	}
}

func TestHandleMuninnConnectLocal_PersistsWithoutLeakingToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".muninn"), 0700); err != nil {
		t.Fatal(err)
	}
	secret := "mdb_handler_secret"
	if err := os.WriteFile(filepath.Join(home, ".muninn", "mcp.token"), []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}

	srv, ts := newTestServer(t)
	cfgPath := filepath.Join(t.TempDir(), "muninn.json")
	srv.muninnCfgPath = cfgPath

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/muninn/connect-local", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["ok"] != true || body["connected"] != true {
		t.Fatalf("unexpected body keys: ok=%v connected=%v", body["ok"], body["connected"])
	}
	if _, ok := body["token"]; ok {
		t.Fatal("response must not include token")
	}
	if _, ok := body["mcp_token"]; ok {
		t.Fatal("response must not include mcp_token")
	}
	raw, _ := json.Marshal(body)
	if strings.Contains(string(raw), secret) {
		t.Fatal("response leaked local daemon token")
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected muninn.json written: %v", err)
	}
	onDisk, err := memory.LoadGlobalConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(onDisk.VaultTokens) != 0 || onDisk.UserVault != "" {
		t.Fatal("must not invent a vault name")
	}
}

func TestHandleMuninnConnectLocal_UnavailableWithoutToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	srv, ts := newTestServer(t)
	srv.muninnCfgPath = filepath.Join(t.TempDir(), "muninn.json")

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/muninn/connect-local", nil)
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

func startMockMCP(t *testing.T, remember func(vault string)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		method, _ := req["method"].(string)
		id := req["id"]
		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "initialize":
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"protocolVersion": "2024-11-05"}})
		case "notifications/initialized":
			w.WriteHeader(http.StatusNoContent)
		case "tools/call":
			params, _ := req["params"].(map[string]any)
			args, _ := params["arguments"].(map[string]any)
			vault, _ := args["vault"].(string)
			if remember != nil {
				remember(vault)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result":  map[string]any{"content": []map[string]any{{"type": "text", "text": `{"id":"e1"}`}}},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
		}
	}))
}

func TestCreateMuninnVault_MCPDaemonSuccess(t *testing.T) {
	var mu sync.Mutex
	var got []string
	mcpSrv := startMockMCP(t, func(vault string) {
		mu.Lock()
		got = append(got, vault)
		mu.Unlock()
	})
	defer mcpSrv.Close()

	srv, _ := newTestServer(t)
	cfgPath := filepath.Join(t.TempDir(), "muninn.json")
	if err := memory.SaveGlobalConfig(cfgPath, &memory.GlobalConfig{
		Endpoint: mcpSrv.URL + "/mcp",
		MCPToken: "mdb_test_daemon",
	}); err != nil {
		t.Fatal(err)
	}
	srv.muninnCfgPath = cfgPath

	if _, err := srv.createMuninnVault("morgan-huginn", "huginn-morgan"); err != nil {
		t.Fatalf("createMuninnVault: %v", err)
	}
	mu.Lock()
	names := append([]string{}, got...)
	mu.Unlock()
	if len(names) != 1 || names[0] != "morgan-huginn" {
		t.Fatalf("remember vaults=%v, want morgan-huginn", names)
	}
	onDisk, err := memory.LoadGlobalConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(onDisk.VaultTokens) != 0 {
		t.Fatal("must not persist per-vault tokens")
	}
}

func TestCreateMuninnVault_UsesLocalTokenFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".muninn"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".muninn", "mcp.token"), []byte("mdb_from_file\n"), 0600); err != nil {
		t.Fatal(err)
	}

	var sawAuth bool
	mcpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") && len(r.Header.Get("Authorization")) > 8 {
			sawAuth = true
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		method, _ := req["method"].(string)
		id := req["id"]
		w.Header().Set("Content-Type", "application/json")
		if method == "notifications/initialized" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if method == "tools/call" {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"content": []map[string]any{{"type": "text", "text": `{"id":"e1"}`}}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"protocolVersion": "2024-11-05"}})
	}))
	defer mcpSrv.Close()

	srv, _ := newTestServer(t)
	cfgPath := filepath.Join(t.TempDir(), "muninn.json")
	if err := memory.SaveGlobalConfig(cfgPath, &memory.GlobalConfig{Endpoint: mcpSrv.URL + "/mcp"}); err != nil {
		t.Fatal(err)
	}
	srv.muninnCfgPath = cfgPath
	if _, err := srv.createMuninnVault("hire3probe-huginn", "huginn-hire3probe"); err != nil {
		t.Fatalf("createMuninnVault: %v", err)
	}
	if !sawAuth {
		t.Fatal("expected bearer from ~/.muninn/mcp.token")
	}
}

func TestCreateMuninnVault_RefusesReserved(t *testing.T) {
	srv, _ := newTestServer(t)
	cfgPath := filepath.Join(t.TempDir(), "muninn.json")
	if err := memory.SaveGlobalConfig(cfgPath, &memory.GlobalConfig{
		Endpoint: "http://127.0.0.1:8750",
		MCPToken: "mdb_x",
	}); err != nil {
		t.Fatal(err)
	}
	srv.muninnCfgPath = cfgPath
	for _, name := range []string{"default", "personal", "aws", "beacon", "shotgroup", "simplorium", "muninndb"} {
		if _, err := srv.createMuninnVault(name, "huginn-x"); err == nil {
			t.Fatalf("must refuse reserved vault %q", name)
		}
	}
}

func TestHandleMuninnVaultCreate_MCPSuccessNoTokenLeak(t *testing.T) {
	mcpSrv := startMockMCP(t, nil)
	defer mcpSrv.Close()
	srv, ts := newTestServer(t)
	cfgPath := filepath.Join(t.TempDir(), "muninn.json")
	if err := memory.SaveGlobalConfig(cfgPath, &memory.GlobalConfig{
		Endpoint: mcpSrv.URL + "/mcp",
		MCPToken: "mdb_handler_secret",
	}); err != nil {
		t.Fatal(err)
	}
	srv.muninnCfgPath = cfgPath

	body := `{"vault_name":"morgan-huginn","agent_label":"huginn-morgan"}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/muninn/vaults", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	raw, _ := json.Marshal(map[string]any{})
	var respBody map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatal(err)
	}
	_ = raw
	if respBody["ok"] != true || respBody["vault_name"] != "morgan-huginn" {
		t.Fatalf("body=%v", respBody)
	}
	if _, ok := respBody["token"]; ok {
		t.Fatal("response must not include token")
	}
	if _, ok := respBody["mcp_token"]; ok {
		t.Fatal("response must not include mcp_token")
	}
	enc, _ := json.Marshal(respBody)
	if strings.Contains(string(enc), "mdb_handler_secret") {
		t.Fatal("response leaked daemon token")
	}
}

