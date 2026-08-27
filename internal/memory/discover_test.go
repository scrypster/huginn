package memory_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/memory"
)

func TestLoadGlobalConfig_DiscoversLocalDaemonWhenDefaultMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".muninn"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".muninn", "mcp.token"), []byte("mdb_test_token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(home, ".config", "huginn", "muninn.json")
	cfg, err := memory.LoadGlobalConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadGlobalConfig: %v", err)
	}
	if cfg.Endpoint != memory.LocalMCPEndpoint {
		t.Errorf("endpoint = %q, want %q", cfg.Endpoint, memory.LocalMCPEndpoint)
	}
	if cfg.MCPToken != "mdb_test_token" {
		t.Errorf("token leaked or wrong: len=%d", len(cfg.MCPToken))
	}
	if cfg.MCPToken != "mdb_test_token" {
		t.Fatalf("expected token from mcp.token file")
	}
}

func TestLoadGlobalConfig_DiscoverFailClosedWhenTokenMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".muninn"), 0700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(home, ".config", "huginn", "muninn.json")
	cfg, err := memory.LoadGlobalConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadGlobalConfig: %v", err)
	}
	if cfg.Endpoint != "" {
		t.Errorf("expected empty endpoint when token file missing, got %q", cfg.Endpoint)
	}
	if cfg.MCPToken != "" {
		t.Errorf("expected empty token when token file missing")
	}
}

func TestLoadGlobalConfig_DoesNotDiscoverNonDefaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".muninn"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".muninn", "mcp.token"), []byte("mdb_test_token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "muninn.json")
	cfg, err := memory.LoadGlobalConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadGlobalConfig: %v", err)
	}
	if cfg.Endpoint != "" || cfg.MCPToken != "" {
		t.Errorf("must not discover for non-default path, endpoint=%q", cfg.Endpoint)
	}
}

func TestLoadGlobalConfig_DoesNotDiscoverWhenFileExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".muninn"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".muninn", "mcp.token"), []byte("mdb_test_token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(home, ".config", "huginn", "muninn.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(`{"username":"root"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := memory.LoadGlobalConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadGlobalConfig: %v", err)
	}
	if cfg.Endpoint != "" {
		t.Errorf("existing muninn.json must not be silently filled, endpoint=%q", cfg.Endpoint)
	}
	if cfg.MCPToken != "" {
		t.Errorf("existing muninn.json must not receive a discovered token")
	}
}

func TestDetectMuninnPresence_HomeDirInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".muninn"), 0700); err != nil {
		t.Fatal(err)
	}
	installed, _ := memory.DetectMuninnPresence()
	if !installed {
		t.Fatal("expected installed=true when ~/.muninn exists")
	}
}

func TestDetectMuninnPresence_NotInstalledWithoutDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	installed, running := memory.DetectMuninnPresence()
	if running {
		// Real :8750 may be up on this machine; installed is then true too.
		if !installed {
			t.Fatal("running implies installed")
		}
		return
	}
	if installed {
		t.Fatal("expected installed=false when ~/.muninn is absent and daemon is down")
	}
}

func TestApplyLocalDaemonDiscovery_DoesNotInventVault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".muninn"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".muninn", "mcp.token"), []byte("mdb_x"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &memory.GlobalConfig{VaultTokens: map[string]string{}}
	if !memory.ApplyLocalDaemonDiscovery(cfg) {
		t.Fatal("expected discovery to succeed")
	}
	if len(cfg.VaultTokens) != 0 {
		t.Fatalf("must not invent vault tokens, got %v", cfg.VaultTokens)
	}
	if cfg.UserVault != "" {
		t.Fatalf("must not invent user vault, got %q", cfg.UserVault)
	}
}

func TestPersistLocalDaemonDiscovery_WritesFileWithoutInventingVault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".muninn"), 0700); err != nil {
		t.Fatal(err)
	}
	secret := "mdb_persist_test"
	if err := os.WriteFile(filepath.Join(home, ".muninn", "mcp.token"), []byte(secret+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(t.TempDir(), "muninn.json")
	cfg, wrote, err := memory.PersistLocalDaemonDiscovery(cfgPath)
	if err != nil {
		t.Fatalf("PersistLocalDaemonDiscovery: %v", err)
	}
	if !wrote {
		t.Fatal("expected muninn.json to be written")
	}
	if cfg.Endpoint != memory.LocalMCPEndpoint {
		t.Errorf("endpoint = %q, want local MCP", cfg.Endpoint)
	}
	if len(cfg.VaultTokens) != 0 || cfg.UserVault != "" {
		t.Fatalf("must not invent a vault")
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected muninn.json on disk: %v", err)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "mcp_token") {
		t.Fatal("expected persisted mcp_token field")
	}
}

func TestPersistLocalDaemonDiscovery_FailClosedWithoutToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgPath := filepath.Join(t.TempDir(), "muninn.json")
	cfg, wrote, err := memory.PersistLocalDaemonDiscovery(cfgPath)
	if err != nil {
		t.Fatalf("PersistLocalDaemonDiscovery: %v", err)
	}
	if wrote {
		t.Fatal("must not write muninn.json without a local token")
	}
	if cfg.Endpoint != "" {
		t.Errorf("expected empty endpoint, got %q", cfg.Endpoint)
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatal("must not create muninn.json when discovery fails")
	}
}

func TestPersistLocalDaemonDiscovery_KeepsExistingVaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgPath := filepath.Join(t.TempDir(), "muninn.json")
	if err := memory.SaveGlobalConfig(cfgPath, &memory.GlobalConfig{
		Endpoint:    "http://127.0.0.1:8750",
		MCPToken:    "mdb_existing",
		VaultTokens: map[string]string{"already-there": "mk_x"},
	}); err != nil {
		t.Fatal(err)
	}
	cfg, wrote, err := memory.PersistLocalDaemonDiscovery(cfgPath)
	if err != nil {
		t.Fatalf("PersistLocalDaemonDiscovery: %v", err)
	}
	if wrote {
		t.Fatal("already-complete config should not be rewritten")
	}
	if len(cfg.VaultTokens) != 1 || cfg.VaultTokens["already-there"] == "" {
		t.Fatalf("must keep existing vault, got %d", len(cfg.VaultTokens))
	}
}

func TestVaultNames_NamesOnly(t *testing.T) {
	names := memory.VaultNames(&memory.GlobalConfig{VaultTokens: map[string]string{"b": "t2", "a": "t1"}})
	if len(names) != 2 {
		t.Fatalf("len=%d", len(names))
	}
	for _, n := range names {
		if n != "a" && n != "b" {
			t.Fatalf("unexpected name %q", n)
		}
	}
	if len(memory.VaultNames(nil)) != 0 {
		t.Fatal("nil config should yield empty names")
	}
}

func TestPinDaemonAuth_FillsTokenFromFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".muninn"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".muninn", "mcp.token"), []byte("mdb_pin_test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &memory.GlobalConfig{Endpoint: "http://127.0.0.1:8750"}
	if !memory.PinDaemonAuth(cfg) {
		t.Fatal("expected pin to succeed")
	}
	if cfg.MCPToken != "mdb_pin_test" {
		t.Fatalf("token path wrong: len=%d", len(cfg.MCPToken))
	}
	if cfg.Endpoint != memory.LocalMCPEndpoint && cfg.Endpoint != "http://127.0.0.1:8750" {
		t.Fatalf("endpoint=%q", cfg.Endpoint)
	}
}

func TestIsReservedVaultName(t *testing.T) {
	for _, n := range []string{"default", "personal", "aws", "beacon", "shotgroup", "simplorium", "muninndb", "DEFAULT"} {
		if !memory.IsReservedVaultName(n) {
			t.Fatalf("want reserved %q", n)
		}
	}
	if memory.IsReservedVaultName("morgan-huginn") || memory.IsReservedVaultName("hire3probe-huginn") {
		t.Fatal("hire vaults must not be reserved")
	}
}

