package config

import (
	"os"
	"testing"
)

// TestValidate_ManagedBackend_RequiresAPIKey verifies that Validate returns an
// error when backend type is "managed" and no API key is set.
func TestValidate_ManagedBackend_RequiresAPIKey(t *testing.T) {
	cfg := Config{}
	cfg.Backend.Type = "managed"
	cfg.Backend.APIKey = "" // no key

	err := Validate(cfg)
	if err == nil {
		t.Error("expected error for managed backend with no API key, got nil")
	}
}

// TestValidate_ManagedBackend_WithLiteralKey passes when a literal key is set.
func TestValidate_ManagedBackend_WithLiteralKey(t *testing.T) {
	cfg := Config{}
	cfg.Backend.Type = "managed"
	cfg.Backend.APIKey = "sk-ant-literal-key-here"

	err := Validate(cfg)
	if err != nil {
		t.Errorf("expected no error for managed backend with literal API key, got: %v", err)
	}
}

// TestValidate_ManagedBackend_WithEnvVarKey passes when an env-var key is set and populated.
func TestValidate_ManagedBackend_WithEnvVarKey(t *testing.T) {
	t.Setenv("TEST_HUGINN_API_KEY", "sk-ant-env-key-here")

	cfg := Config{}
	cfg.Backend.Type = "managed"
	cfg.Backend.APIKey = "$TEST_HUGINN_API_KEY"

	err := Validate(cfg)
	if err != nil {
		t.Errorf("expected no error for managed backend with populated env var, got: %v", err)
	}
}

// TestValidate_ManagedBackend_WithEmptyEnvVar fails when the env var is empty.
func TestValidate_ManagedBackend_WithEmptyEnvVar(t *testing.T) {
	t.Setenv("TEST_HUGINN_EMPTY_KEY", "")

	cfg := Config{}
	cfg.Backend.Type = "managed"
	cfg.Backend.APIKey = "$TEST_HUGINN_EMPTY_KEY"

	err := Validate(cfg)
	if err == nil {
		t.Error("expected error for managed backend with empty env var, got nil")
	}
}

// TestValidate_ExternalBackend_NoKeyRequired verifies that external backend
// does not require an API key.
func TestValidate_ExternalBackend_NoKeyRequired(t *testing.T) {
	cfg := Config{}
	cfg.Backend.Type = "external"
	cfg.Backend.APIKey = "" // no key — should be fine

	err := Validate(cfg)
	if err != nil {
		t.Errorf("expected no error for external backend without API key, got: %v", err)
	}
}

// TestValidate_OllamaBackend_NoKeyRequired verifies that ollama provider
// does not require an API key.
func TestValidate_OllamaBackend_NoKeyRequired(t *testing.T) {
	cfg := Config{}
	cfg.Backend.Type = "external"
	cfg.Backend.Provider = "ollama"
	cfg.Backend.APIKey = ""

	err := Validate(cfg)
	if err != nil {
		t.Errorf("expected no error for ollama provider without API key, got: %v", err)
	}
}

// TestValidate_PortRange_Boundaries verifies boundary values for port validation.
func TestValidate_PortRange_Boundaries(t *testing.T) {
	cases := []struct {
		port    int
		wantErr bool
	}{
		{0, false},    // dynamic — always valid
		{1023, true},  // below minimum
		{1024, false}, // minimum valid
		{65535, false}, // maximum valid
		{65536, true},  // above maximum
	}
	for _, tc := range cases {
		cfg := Config{}
		cfg.WebUI.Port = tc.port
		err := Validate(cfg)
		if tc.wantErr && err == nil {
			t.Errorf("port=%d: expected error, got nil", tc.port)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("port=%d: expected no error, got: %v", tc.port, err)
		}
	}
}

// TestResolvedAPIKey_LiteralValue verifies literal key resolution.
func TestResolvedAPIKey_LiteralValue(t *testing.T) {
	bc := &BackendConfig{APIKey: "sk-literal"}
	if got := bc.ResolvedAPIKey(); got != "sk-literal" {
		t.Errorf("expected 'sk-literal', got %q", got)
	}
}

// TestResolvedAPIKey_EnvVar verifies environment variable resolution.
func TestResolvedAPIKey_EnvVar(t *testing.T) {
	t.Setenv("HUGINN_TEST_KEY", "sk-from-env")
	bc := &BackendConfig{APIKey: "$HUGINN_TEST_KEY"}
	if got := bc.ResolvedAPIKey(); got != "sk-from-env" {
		t.Errorf("expected 'sk-from-env', got %q", got)
	}
}

// TestResolvedAPIKey_Empty verifies empty key returns empty string.
func TestResolvedAPIKey_Empty(t *testing.T) {
	bc := &BackendConfig{APIKey: ""}
	if got := bc.ResolvedAPIKey(); got != "" {
		t.Errorf("expected '', got %q", got)
	}
}

// TestResolvedAPIKey_UnsetEnvVar verifies unset env var returns empty string.
func TestResolvedAPIKey_UnsetEnvVar(t *testing.T) {
	os.Unsetenv("HUGINN_UNSET_KEY_TEST_ITER2")
	bc := &BackendConfig{APIKey: "$HUGINN_UNSET_KEY_TEST_ITER2"}
	if got := bc.ResolvedAPIKey(); got != "" {
		t.Errorf("expected '' for unset env var, got %q", got)
	}
}

// TestMigrateV5toV6_Anthropic directly tests the migration function.
func TestMigrateV5toV6_Anthropic(t *testing.T) {
	cfg := Default()
	cfg.Backend.Provider = "" // simulate pre-migration state
	cfg.Backend.Endpoint = "https://api.anthropic.com/v1"
	migrateV5toV6(cfg)
	if cfg.Backend.Provider != "anthropic" {
		t.Errorf("expected provider='anthropic' after migration, got %q", cfg.Backend.Provider)
	}
}

// TestMigrateV5toV6_OpenAI directly tests the migration function.
func TestMigrateV5toV6_OpenAI(t *testing.T) {
	cfg := Default()
	cfg.Backend.Provider = ""
	cfg.Backend.Endpoint = "https://api.openai.com/v1"
	migrateV5toV6(cfg)
	if cfg.Backend.Provider != "openai" {
		t.Errorf("expected provider='openai' after migration, got %q", cfg.Backend.Provider)
	}
}

// TestMigrateV5toV6_OpenRouter directly tests the migration function.
func TestMigrateV5toV6_OpenRouter(t *testing.T) {
	cfg := Default()
	cfg.Backend.Provider = ""
	cfg.Backend.Endpoint = "https://openrouter.ai/api/v1"
	migrateV5toV6(cfg)
	if cfg.Backend.Provider != "openrouter" {
		t.Errorf("expected provider='openrouter' after migration, got %q", cfg.Backend.Provider)
	}
}

// TestMigrateV5toV6_DefaultsToOllama directly tests the migration function.
func TestMigrateV5toV6_DefaultsToOllama(t *testing.T) {
	cfg := Default()
	cfg.Backend.Provider = ""
	cfg.Backend.Endpoint = "http://localhost:11434"
	migrateV5toV6(cfg)
	if cfg.Backend.Provider != "ollama" {
		t.Errorf("expected provider='ollama' after migration, got %q", cfg.Backend.Provider)
	}
}

// TestMigrateV5toV6_AlreadySet verifies migration is skipped when provider is set.
func TestMigrateV5toV6_AlreadySet(t *testing.T) {
	cfg := Default()
	cfg.Backend.Provider = "anthropic"
	cfg.Backend.Endpoint = "http://localhost:11434"
	migrateV5toV6(cfg)
	if cfg.Backend.Provider != "anthropic" {
		t.Errorf("expected provider to remain 'anthropic', got %q", cfg.Backend.Provider)
	}
}

// TestLoadFrom_MigratesV1toV2_SetsDiffReviewMode verifies v1→v2 migration.
func TestLoadFrom_MigratesV1toV2_SetsDiffReviewMode(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.json"

	raw := `{"version": 1}` // no diff_review_mode set
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.DiffReviewMode != "always" {
		t.Errorf("expected diff_review_mode='always' after v1→v2 migration, got %q", cfg.DiffReviewMode)
	}
}

// TestLoadFrom_MigratesV3toV4_SetsNotepads verifies v3→v4 migration.
func TestLoadFrom_MigratesV3toV4_SetsNotepads(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.json"

	raw := `{"version": 3}` // no notepads fields
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.NotepadsMaxTokens != 8192 {
		t.Errorf("expected NotepadsMaxTokens=8192 after v3→v4 migration, got %d", cfg.NotepadsMaxTokens)
	}
	if !cfg.NotepadsEnabled {
		t.Error("expected NotepadsEnabled=true after v3→v4 migration")
	}
}

// TestLoadFrom_MigratesV4toV5_SetsCompactFields verifies v4→v5 migration.
func TestLoadFrom_MigratesV4toV5_SetsCompactFields(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.json"

	raw := `{"version": 4}` // no compact fields
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.CompactMode != "auto" {
		t.Errorf("expected CompactMode='auto' after migration, got %q", cfg.CompactMode)
	}
	if cfg.CompactTrigger != 0.70 {
		t.Errorf("expected CompactTrigger=0.70 after migration, got %f", cfg.CompactTrigger)
	}
}

// TestValidate_Method_ClampsInvalidValues verifies the Validate method (on Config)
// clamps out-of-range values.
func TestValidate_Method_ClampsInvalidValues(t *testing.T) {
	cfg := Config{}
	cfg.DiffReviewMode = "invalid"
	cfg.CompactMode = "invalid"
	cfg.CompactTrigger = -1.0
	cfg.MaxImageSizeKB = -1
	cfg.MaxTurns = 0
	cfg.BashTimeoutSecs = -1
	cfg.Validate()

	if cfg.DiffReviewMode != "always" {
		t.Errorf("DiffReviewMode: expected 'always', got %q", cfg.DiffReviewMode)
	}
	if cfg.CompactMode != "auto" {
		t.Errorf("CompactMode: expected 'auto', got %q", cfg.CompactMode)
	}
	if cfg.CompactTrigger != 0.70 {
		t.Errorf("CompactTrigger: expected 0.70, got %f", cfg.CompactTrigger)
	}
	if cfg.MaxImageSizeKB != 2048 {
		t.Errorf("MaxImageSizeKB: expected 2048, got %d", cfg.MaxImageSizeKB)
	}
	if cfg.MaxTurns != 50 {
		t.Errorf("MaxTurns: expected 50, got %d", cfg.MaxTurns)
	}
	if cfg.BashTimeoutSecs != 120 {
		t.Errorf("BashTimeoutSecs: expected 120, got %d", cfg.BashTimeoutSecs)
	}
}
