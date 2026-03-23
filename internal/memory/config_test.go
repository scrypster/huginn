package memory_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/memory"
)

func TestLoadGlobalConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	cfg, err := memory.LoadGlobalConfig(filepath.Join(dir, "muninn.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Endpoint != "" {
		t.Errorf("expected empty default endpoint, got %q", cfg.Endpoint)
	}
	if cfg.Strategy != memory.StrategyTwoTier {
		t.Errorf("expected default strategy two-tier, got %q", cfg.Strategy)
	}
}

func TestLoadGlobalConfig_ReadsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "muninn.json")
	content := `{"endpoint":"http://10.0.0.1:3030","user_vault":"huginn:user:testuser","strategy":"single"}`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := memory.LoadGlobalConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Endpoint != "http://10.0.0.1:3030" {
		t.Errorf("got endpoint %q", cfg.Endpoint)
	}
	if cfg.UserVault != "huginn:user:testuser" {
		t.Errorf("got user_vault %q", cfg.UserVault)
	}
	if cfg.Strategy != memory.StrategySingle {
		t.Errorf("got strategy %q", cfg.Strategy)
	}
}

func TestLoadProjectConfig_MissingReturnsNil(t *testing.T) {
	dir := t.TempDir()
	cfg, err := memory.LoadProjectConfig(filepath.Join(dir, "muninn.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Error("expected nil config when file missing")
	}
}

func TestLoadProjectConfig_ReadsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "muninn.json")
	content := `{"project_vault":"huginn:project:acme/myrepo","additional_vaults":["huginn:project:acme/shared"]}`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := memory.LoadProjectConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ProjectVault != "huginn:project:acme/myrepo" {
		t.Errorf("got project_vault %q", cfg.ProjectVault)
	}
	if len(cfg.AdditionalVaults) != 1 || cfg.AdditionalVaults[0] != "huginn:project:acme/shared" {
		t.Errorf("got additional_vaults %v", cfg.AdditionalVaults)
	}
}

// TestLoadGlobalConfig_InvalidJSON returns error for corrupt JSON.
func TestLoadGlobalConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "muninn.json")
	if err := os.WriteFile(path, []byte("{invalid json}"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := memory.LoadGlobalConfig(path)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

// TestLoadProjectConfig_InvalidJSON returns error for corrupt JSON.
func TestLoadProjectConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "muninn.json")
	if err := os.WriteFile(path, []byte("{not valid}"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := memory.LoadProjectConfig(path)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

// TestLoadGlobalConfig_WithStrategy verifies that different strategies are loaded.
func TestLoadGlobalConfig_WithStrategy(t *testing.T) {
	strategies := []string{"single", "personal-only", "project-only", "two-tier"}
	for _, strat := range strategies {
		t.Run(strat, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "muninn.json")
			content := `{"strategy":"` + strat + `"}`
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := memory.LoadGlobalConfig(path)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(cfg.Strategy) != strat {
				t.Errorf("strategy not loaded correctly: expected %q, got %q", strat, cfg.Strategy)
			}
		})
	}
}

// TestLoadProjectConfig_WithStrategy verifies that project strategy is loaded.
func TestLoadProjectConfig_WithStrategy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "muninn.json")
	content := `{"project_vault":"huginn:project:test","strategy":"project-only"}`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := memory.LoadProjectConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Strategy != memory.StrategyProjectOnly {
		t.Errorf("expected ProjectOnly strategy, got %v", cfg.Strategy)
	}
}

// TestLoadGlobalConfig_PartialFile verifies loading config with missing optional fields.
func TestLoadGlobalConfig_PartialFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "muninn.json")
	// Minimal valid config - just endpoint
	content := `{"endpoint":"http://localhost:3030"}`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := memory.LoadGlobalConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Endpoint != "http://localhost:3030" {
		t.Errorf("endpoint not loaded: %q", cfg.Endpoint)
	}
	// Strategy should have default when not specified
	if cfg.Strategy == "" {
		t.Error("strategy should have default value when not in config")
	}
}

func TestGlobalConfig_VaultTokensRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "muninn.json")

	cfg := &memory.GlobalConfig{
		Endpoint:        "http://localhost:8475",
		Username:        "root",
		VaultTokens:     map[string]string{"huginn-steve": "mk_abc123"},
		Strategy:        memory.StrategyTwoTier,
		ActivationLimit: 10,
	}
	if err := memory.SaveGlobalConfig(path, cfg); err != nil {
		t.Fatalf("SaveGlobalConfig: %v", err)
	}
	loaded, err := memory.LoadGlobalConfig(path)
	if err != nil {
		t.Fatalf("LoadGlobalConfig: %v", err)
	}
	if loaded.Username != "root" {
		t.Errorf("Username: got %q want %q", loaded.Username, "root")
	}
	if loaded.VaultTokens["huginn-steve"] != "mk_abc123" {
		t.Errorf("VaultTokens: got %v", loaded.VaultTokens)
	}
}

func TestGlobalConfig_UsernameDefaultsToRoot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "muninn.json")
	if err := os.WriteFile(path, []byte(`{"endpoint":"http://localhost:8475"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := memory.LoadGlobalConfig(path)
	if err != nil {
		t.Fatalf("LoadGlobalConfig: %v", err)
	}
	if cfg.Username != "root" {
		t.Errorf("expected default Username=root, got %q", cfg.Username)
	}
}

func TestGlobalConfig_VaultTokensDefaultsToEmptyMap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "muninn.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := memory.LoadGlobalConfig(path)
	if err != nil {
		t.Fatalf("LoadGlobalConfig: %v", err)
	}
	if cfg.VaultTokens == nil {
		t.Error("expected VaultTokens to be non-nil empty map, got nil")
	}
}

func TestSaveGlobalConfig_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "muninn.json")
	cfg := &memory.GlobalConfig{Endpoint: "http://localhost:8475", Username: "root"}
	if err := memory.SaveGlobalConfig(path, cfg); err != nil {
		t.Fatalf("SaveGlobalConfig: %v", err)
	}
	// Tmp file must not exist after atomic rename
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error("expected .tmp file to be cleaned up")
	}
	// File permissions must be 0600
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 permissions, got %o", info.Mode().Perm())
	}
}
