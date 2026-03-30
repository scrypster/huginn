package memory

import "testing"

func TestMCPURLFromEndpoint(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"http://localhost:8475", "http://localhost:8750/mcp"},
		{"http://192.168.1.100:8475", "http://192.168.1.100:8750/mcp"},
		{"http://localhost:8475/", "http://localhost:8750/mcp"},
	}
	for _, tc := range tests {
		got, err := MCPURLFromEndpoint(tc.in)
		if err != nil {
			t.Fatalf("input %q: unexpected error: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("input %q: got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestVaultTokenFor(t *testing.T) {
	cfg := &GlobalConfig{
		Endpoint:    "http://localhost:8475",
		VaultTokens: map[string]string{"huginn:agent:mj:alice": "tok-abc"},
	}
	tok, err := VaultTokenFor(cfg, "huginn:agent:mj:alice")
	if err != nil {
		t.Fatal(err)
	}
	if tok != "tok-abc" {
		t.Fatalf("expected tok-abc, got %q", tok)
	}
	_, err = VaultTokenFor(cfg, "huginn:agent:mj:unknown")
	if err == nil {
		t.Fatal("expected error for unknown vault")
	}
}

// MCPTokenFor tests — vault MCP uses a daemon token, not per-vault API keys.

func TestMCPTokenFor_PrefersMCPToken(t *testing.T) {
	// When both mcp_token and vault_tokens are set, mcp_token wins.
	cfg := &GlobalConfig{
		MCPToken:    "mdb_daemon_token",
		VaultTokens: map[string]string{"huginn-mike": "mk_api_key"},
	}
	tok, err := MCPTokenFor(cfg, "huginn-mike")
	if err != nil {
		t.Fatal(err)
	}
	if tok != "mdb_daemon_token" {
		t.Fatalf("got %q, want mdb_daemon_token", tok)
	}
}

func TestMCPTokenFor_FallbackToVaultToken(t *testing.T) {
	// When mcp_token is empty, fall back to vault token.
	cfg := &GlobalConfig{
		VaultTokens: map[string]string{"huginn-mike": "mk_api_key"},
	}
	tok, err := MCPTokenFor(cfg, "huginn-mike")
	if err != nil {
		t.Fatal(err)
	}
	if tok != "mk_api_key" {
		t.Fatalf("got %q, want mk_api_key", tok)
	}
}

func TestMCPTokenFor_NilConfig(t *testing.T) {
	_, err := MCPTokenFor(nil, "huginn-mike")
	if err == nil {
		t.Fatal("expected error for nil config")
	}
}

func TestMCPTokenFor_BothEmpty(t *testing.T) {
	// No mcp_token and no vault token for the vault — error.
	cfg := &GlobalConfig{}
	_, err := MCPTokenFor(cfg, "huginn-mike")
	if err == nil {
		t.Fatal("expected error when both mcp_token and vault_tokens are empty")
	}
}

func TestMCPTokenFor_WhitespaceOnlyMCPToken_FallsBack(t *testing.T) {
	// A whitespace-only mcp_token must NOT be used — fall back to vault token.
	cfg := &GlobalConfig{
		MCPToken:    "   \t\n",
		VaultTokens: map[string]string{"huginn-mike": "mk_api_key"},
	}
	tok, err := MCPTokenFor(cfg, "huginn-mike")
	if err != nil {
		t.Fatal(err)
	}
	if tok != "mk_api_key" {
		t.Fatalf("whitespace mcp_token should fall back to vault token, got %q", tok)
	}
}

func TestGlobalConfig_MCPToken_RoundTrip(t *testing.T) {
	// mcp_token survives SaveGlobalConfig → LoadGlobalConfig.
	dir := t.TempDir()
	path := dir + "/muninn.json"
	cfg := &GlobalConfig{
		Endpoint:        "http://localhost:8475",
		MCPToken:        "mdb_testtoken",
		VaultTokens:     map[string]string{"huginn-mike": "mk_api_key"},
	}
	if err := SaveGlobalConfig(path, cfg); err != nil {
		t.Fatalf("SaveGlobalConfig: %v", err)
	}
	loaded, err := LoadGlobalConfig(path)
	if err != nil {
		t.Fatalf("LoadGlobalConfig: %v", err)
	}
	if loaded.MCPToken != "mdb_testtoken" {
		t.Fatalf("MCPToken: got %q, want mdb_testtoken", loaded.MCPToken)
	}
}
