package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg := Default()
	if cfg.OllamaBaseURL != "http://localhost:11434" {
		t.Errorf("expected default URL, got %q", cfg.OllamaBaseURL)
	}
	if cfg.ReasonerModel == "" {
		t.Errorf("expected non-empty default reasoner model, got %q", cfg.ReasonerModel)
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.ReasonerModel = "llama3:8b"
	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.ReasonerModel != "llama3:8b" {
		t.Errorf("expected llama3:8b, got %q", loaded.ReasonerModel)
	}
}

func TestLoadMissingReturnsDefaults(t *testing.T) {
	cfg, err := LoadFrom("/nonexistent/path/config.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OllamaBaseURL != "http://localhost:11434" {
		t.Error("expected defaults on missing file")
	}
	// MachineID must be generated even when SaveTo fails (non-writable path).
	if cfg.MachineID == "" {
		t.Error("expected MachineID to be generated even when config cannot be saved")
	}
}

// TestDefault_AllFieldsSet verifies that Default() populates all critical fields.
func TestDefault_AllFieldsSet(t *testing.T) {
	cfg := Default()
	if cfg.ReasonerModel == "" {
		t.Error("expected non-empty ReasonerModel")
	}
	if cfg.OllamaBaseURL == "" {
		t.Error("expected non-empty OllamaBaseURL")
	}
	if cfg.Theme == "" {
		t.Error("expected non-empty Theme")
	}
	if cfg.ContextLimitKB <= 0 {
		t.Errorf("expected positive ContextLimitKB, got %d", cfg.ContextLimitKB)
	}
	if cfg.MaxTurns <= 0 {
		t.Errorf("expected positive MaxTurns, got %d", cfg.MaxTurns)
	}
	if cfg.BashTimeoutSecs <= 0 {
		t.Errorf("expected positive BashTimeoutSecs, got %d", cfg.BashTimeoutSecs)
	}
	if !cfg.ToolsEnabled {
		t.Error("expected ToolsEnabled=true by default")
	}
}

// TestLoadFrom_InvalidJSON verifies that invalid JSON returns an error.
func TestLoadFrom_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{invalid json!!!}"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFrom(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON config")
	}
}

// TestLoadFrom_PartialConfig verifies that a partial config merges with defaults.
// Only the specified fields should be overridden; unspecified fields keep defaults.
func TestLoadFrom_PartialConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Write a config with only one field.
	partial := map[string]any{
		"reasoner_model": "custom-reasoner:7b",
	}
	data, err := json.Marshal(partial)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.ReasonerModel != "custom-reasoner:7b" {
		t.Errorf("expected reasoner_model=custom-reasoner:7b, got %q", cfg.ReasonerModel)
	}
	// Default values for unset fields should be preserved.
	if cfg.OllamaBaseURL != "http://localhost:11434" {
		t.Errorf("expected default OllamaBaseURL, got %q", cfg.OllamaBaseURL)
	}
	if cfg.MaxTurns != 50 {
		t.Errorf("expected default MaxTurns=50, got %d", cfg.MaxTurns)
	}
}

// TestSaveTo_CreatesDirectory verifies that SaveTo creates the parent directory.
func TestSaveTo_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	// Use a nested path that doesn't exist yet.
	path := filepath.Join(dir, "subdir", "nested", "config.json")

	cfg := Default()
	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config file to be created at %q: %v", path, err)
	}
}

// TestSaveTo_FilePermissions verifies that the config file is created with mode 0600.
func TestSaveTo_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected file mode 0600, got %o", info.Mode().Perm())
	}
}

// TestSaveTo_ValidJSON verifies that the saved file is valid JSON.
func TestSaveTo_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.AllowedTools = []string{"read_file", "grep"}
	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("saved file is not valid JSON: %v", err)
	}
	// Verify a nested string field is preserved.
	if at, ok := parsed["allowed_tools"].([]any); !ok || len(at) != 2 {
		t.Errorf("expected allowed_tools=[read_file,grep] in JSON, got %v", parsed["allowed_tools"])
	}
}

// TestLoadFrom_BackendConfig verifies that backend sub-object is loaded.
func TestLoadFrom_BackendConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := `{
		"backend": {
			"type": "managed",
			"endpoint": "http://custom:8080"
		}
	}`
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Backend.Type != "managed" {
		t.Errorf("expected backend.type=managed, got %q", cfg.Backend.Type)
	}
	if cfg.Backend.Endpoint != "http://custom:8080" {
		t.Errorf("expected backend.endpoint=http://custom:8080, got %q", cfg.Backend.Endpoint)
	}
}

// TestSaveAndLoad_AllFields verifies round-trip for all important fields.
func TestSaveAndLoad_AllFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.ReasonerModel = "reasoner-test"
	cfg.Theme = "light"
	cfg.ContextLimitKB = 256
	cfg.GitStageOnWrite = true
	cfg.MaxTurns = 100
	cfg.BashTimeoutSecs = 60
	cfg.AllowedTools = []string{"grep", "read_file"}
	cfg.DisallowedTools = []string{"bash"}
	cfg.WorkspacePath = "/tmp/workspace"

	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}

	checks := []struct {
		field string
		got   any
		want  any
	}{
		{"ReasonerModel", loaded.ReasonerModel, "reasoner-test"},
		{"Theme", loaded.Theme, "light"},
		{"ContextLimitKB", loaded.ContextLimitKB, 256},
		{"GitStageOnWrite", loaded.GitStageOnWrite, true},
		{"MaxTurns", loaded.MaxTurns, 100},
		{"BashTimeoutSecs", loaded.BashTimeoutSecs, 60},
		{"WorkspacePath", loaded.WorkspacePath, "/tmp/workspace"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.field, c.got, c.want)
		}
	}
	if len(loaded.AllowedTools) != 2 || loaded.AllowedTools[0] != "grep" {
		t.Errorf("AllowedTools mismatch: %v", loaded.AllowedTools)
	}
	if len(loaded.DisallowedTools) != 1 || loaded.DisallowedTools[0] != "bash" {
		t.Errorf("DisallowedTools mismatch: %v", loaded.DisallowedTools)
	}
	_ = strings.Contains // avoid unused import
}

// TestLoad_DefaultPath verifies Load() reads from ~/.huginn/config.json by
// temporarily pointing HOME at a temp directory.
func TestLoad_DefaultPath(t *testing.T) {
	dir := t.TempDir()
	huginnDir := filepath.Join(dir, ".huginn")
	if err := os.MkdirAll(huginnDir, 0750); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(huginnDir, "config.json")

	// Write a distinctive config file.
	cfg := Default()
	cfg.ReasonerModel = "load-default-path-test"
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	// Override HOME so Load() finds our temp dir.
	t.Setenv("HOME", dir)

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.ReasonerModel != "load-default-path-test" {
		t.Errorf("expected load-default-path-test, got %q", loaded.ReasonerModel)
	}
}

// TestLoad_MissingFile verifies Load() returns defaults when the config file
// does not exist (no error expected).
func TestLoad_MissingFile(t *testing.T) {
	dir := t.TempDir() // no .huginn/config.json here
	t.Setenv("HOME", dir)

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	def := Default()
	if loaded.OllamaBaseURL != def.OllamaBaseURL {
		t.Errorf("expected default OllamaBaseURL, got %q", loaded.OllamaBaseURL)
	}
	if loaded.ReasonerModel != def.ReasonerModel {
		t.Errorf("expected default ReasonerModel, got %q", loaded.ReasonerModel)
	}
}

// TestSave_DefaultPath verifies Save() writes to ~/.huginn/config.json.
func TestSave_DefaultPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cfg := Default()
	cfg.ReasonerModel = "save-default-path-test"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// The file should exist at <dir>/.huginn/config.json.
	cfgPath := filepath.Join(dir, ".huginn", "config.json")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected config file at %q: %v", cfgPath, err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("saved file is not valid JSON: %v", err)
	}
	if cm, ok := parsed["reasoner_model"].(string); !ok || cm != "save-default-path-test" {
		t.Errorf("expected reasoner_model=save-default-path-test, got %v", parsed["reasoner_model"])
	}
}

// TestSave_RoundTrip verifies that Save then Load produces identical field values.
func TestSave_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cfg := Default()
	cfg.Theme = "monokai"
	cfg.MaxTurns = 99
	cfg.BashTimeoutSecs = 300

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Theme != "monokai" {
		t.Errorf("Theme: want monokai, got %q", loaded.Theme)
	}
	if loaded.MaxTurns != 99 {
		t.Errorf("MaxTurns: want 99, got %d", loaded.MaxTurns)
	}
	if loaded.BashTimeoutSecs != 300 {
		t.Errorf("BashTimeoutSecs: want 300, got %d", loaded.BashTimeoutSecs)
	}
}

// TestSaveTo_CreatesParentDir verifies that SaveTo creates deeply nested
// parent directories that do not yet exist.
func TestSaveTo_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "config.json")

	cfg := Default()
	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file at %q: %v", path, err)
	}
}

// TestSaveTo_InvalidPath verifies that SaveTo returns an error when the parent
// directory cannot be created (e.g. /dev/null is not a directory).
func TestSaveTo_InvalidPath(t *testing.T) {
	cfg := Default()
	// /dev/null is a file on macOS/Linux, so MkdirAll on it as a directory fails.
	err := cfg.SaveTo("/dev/null/subdir/config.json")
	if err == nil {
		t.Fatal("expected error for invalid path, got nil")
	}
}

// TestConfig_MachineID_AutoGenerated verifies that MachineID is populated on
// LoadFrom when not previously set.
func TestConfig_MachineID_AutoGenerated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom failed: %v", err)
	}
	if cfg.MachineID == "" {
		t.Error("expected MachineID to be auto-generated, got empty string")
	}
	// Format: <hostname>-<4hexbytes> — must contain at least one hyphen.
	if !strings.Contains(cfg.MachineID, "-") {
		t.Errorf("expected MachineID to contain '-', got %q", cfg.MachineID)
	}
}

// TestConfig_MachineID_Idempotent verifies that loading the config twice from
// the same path produces the same MachineID.
func TestConfig_MachineID_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	cfg1, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("first LoadFrom failed: %v", err)
	}
	cfg2, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("second LoadFrom failed: %v", err)
	}
	if cfg1.MachineID != cfg2.MachineID {
		t.Errorf("MachineID changed between loads: %q vs %q", cfg1.MachineID, cfg2.MachineID)
	}
}

// TestLoadFrom_SetsVersionOnNewConfig verifies that creating a new config
// sets the Version to currentConfigVersion.
func TestLoadFrom_SetsVersionOnNewConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Version != currentConfigVersion {
		t.Errorf("expected Version=%d, got %d", currentConfigVersion, cfg.Version)
	}
}

// TestLoadFrom_MigratesLegacyConfig verifies that a config with no version
// field (legacy version 0) is migrated to currentConfigVersion.
func TestLoadFrom_MigratesLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Legacy config using old fields that no longer exist in the struct.
	// After migration they are ignored; only version matters.
	legacy := `{"theme":"dark"}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Version != currentConfigVersion {
		t.Errorf("expected Version=%d after migration, got %d", currentConfigVersion, cfg.Version)
	}
	if cfg.Theme != "dark" {
		t.Errorf("expected Theme='dark', got %q", cfg.Theme)
	}
}

// TestLoadFrom_MigratedConfigWrittenBack verifies that a migrated config
// is written back to disk with the new version.
func TestLoadFrom_MigratedConfigWrittenBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	_ = os.WriteFile(path, []byte(`{"theme":"solarized"}`), 0o644)

	_, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(path)
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	v, _ := raw["version"].(float64)
	if int(v) != currentConfigVersion {
		t.Errorf("expected version=%d written back, got %v", currentConfigVersion, raw["version"])
	}
}

// TestCurrentConfigVersion_IsPositive verifies that currentConfigVersion
// is set to a positive value (>= 1).
func TestCurrentConfigVersion_IsPositive(t *testing.T) {
	if currentConfigVersion < 1 {
		t.Errorf("currentConfigVersion must be >= 1, got %d", currentConfigVersion)
	}
}

// TestDefault_VisionFieldsSet verifies that Default() sets vision fields.
func TestDefault_VisionFieldsSet(t *testing.T) {
	cfg := Default()
	if !cfg.VisionEnabled {
		t.Error("expected VisionEnabled=true by default")
	}
	if cfg.MaxImageSizeKB <= 0 {
		t.Errorf("expected positive MaxImageSizeKB, got %d", cfg.MaxImageSizeKB)
	}
	if cfg.MaxImageSizeKB != 2048 {
		t.Errorf("expected MaxImageSizeKB=2048, got %d", cfg.MaxImageSizeKB)
	}
}

// TestLoadFrom_VisionDefaults verifies that a config missing vision fields
// gets reasonable defaults.
func TestLoadFrom_VisionDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Write a minimal config without vision fields.
	minimal := map[string]any{
		"reasoner_model": "test-model",
	}
	data, err := json.Marshal(minimal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if !cfg.VisionEnabled {
		t.Error("expected VisionEnabled=true from defaults")
	}
	if cfg.MaxImageSizeKB != 2048 {
		t.Errorf("expected MaxImageSizeKB=2048, got %d", cfg.MaxImageSizeKB)
	}
}

// TestConfigV8Defaults verifies that a v7 config gets web_ui, cloud defaults after migration.
func TestConfigV8Defaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	raw := `{"version": 7}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.WebUI.Bind != "127.0.0.1" {
		t.Errorf("expected web_ui.bind=127.0.0.1, got %q", cfg.WebUI.Bind)
	}
	if !cfg.WebUI.Enabled {
		t.Error("expected web_ui.enabled=true after migration")
	}
	if !cfg.WebUI.AutoOpen {
		t.Error("expected web_ui.auto_open=true after migration")
	}
	if cfg.Cloud.URL != "https://huginncloud.com" {
		t.Errorf("expected cloud.url=https://huginncloud.com, got %q", cfg.Cloud.URL)
	}
	if cfg.Version != currentConfigVersion {
		t.Errorf("expected Version=%d after migration, got %d", currentConfigVersion, cfg.Version)
	}
}

// TestValidate_ValidPort verifies that a valid port passes validation.
func TestValidate_ValidPort(t *testing.T) {
	cfg := Config{}
	cfg.WebUI.Port = 8080
	if err := Validate(cfg); err != nil {
		t.Errorf("expected no error for port 8080, got: %v", err)
	}
}

// TestValidate_ZeroPort verifies that port 0 (dynamic) passes validation.
func TestValidate_ZeroPort(t *testing.T) {
	cfg := Config{}
	cfg.WebUI.Port = 0
	if err := Validate(cfg); err != nil {
		t.Errorf("expected no error for port 0, got: %v", err)
	}
}

// TestValidate_InvalidPort verifies that a port below 1024 fails validation.
func TestValidate_InvalidPort(t *testing.T) {
	cfg := Config{}
	cfg.WebUI.Port = 100
	if err := Validate(cfg); err == nil {
		t.Error("expected error for port 100 (too low), got nil")
	}
}

// TestValidate_InvalidPortTooHigh verifies that a port above 65535 fails validation.
func TestValidate_InvalidPortTooHigh(t *testing.T) {
	cfg := Config{}
	cfg.WebUI.Port = 70000
	if err := Validate(cfg); err == nil {
		t.Error("expected error for port 70000 (too high), got nil")
	}
}

// TestValidate_ValidBind verifies that allowed bind addresses pass validation.
func TestValidate_ValidBind(t *testing.T) {
	for _, bind := range []string{"127.0.0.1", "localhost", ""} {
		cfg := Config{}
		cfg.WebUI.Bind = bind
		if err := Validate(cfg); err != nil {
			t.Errorf("expected no error for bind %q, got: %v", bind, err)
		}
	}
}

// TestValidate_InvalidBind verifies that a non-loopback bind address fails validation.
func TestValidate_InvalidBind(t *testing.T) {
	cfg := Config{}
	cfg.WebUI.Bind = "0.0.0.0"
	if err := Validate(cfg); err == nil {
		t.Error("expected error for bind 0.0.0.0, got nil")
	}
}

// TestDefault_ActiveAgentEmpty verifies that Default() leaves ActiveAgent empty.
func TestDefault_ActiveAgentEmpty(t *testing.T) {
	cfg := Default()
	if cfg.ActiveAgent != "" {
		t.Errorf("expected ActiveAgent to be empty by default, got %q", cfg.ActiveAgent)
	}
}

// TestMigrateV8toV9_ActiveAgentEmpty verifies that loading a v8 config produces
// Version=9 and an empty ActiveAgent after migration.
func TestMigrateV8toV9_ActiveAgentEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	raw := `{"version": 8}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.Version != currentConfigVersion {
		t.Errorf("expected Version=%d after migration, got %d", currentConfigVersion, cfg.Version)
	}
	if cfg.ActiveAgent != "" {
		t.Errorf("expected ActiveAgent to be empty after migration, got %q", cfg.ActiveAgent)
	}
}

// TestDefault_WebUIFields verifies that Default() sets web_ui and cloud fields.
func TestDefault_WebUIFields(t *testing.T) {
	cfg := Default()
	if !cfg.WebUI.Enabled {
		t.Error("expected WebUI.Enabled=true by default")
	}
	if cfg.WebUI.Bind != "127.0.0.1" {
		t.Errorf("expected WebUI.Bind=127.0.0.1, got %q", cfg.WebUI.Bind)
	}
	if !cfg.WebUI.AutoOpen {
		t.Error("expected WebUI.AutoOpen=true by default")
	}
	if cfg.Cloud.URL != "https://huginncloud.com" {
		t.Errorf("expected Cloud.URL=https://huginncloud.com, got %q", cfg.Cloud.URL)
	}
}

// TestSaveAndLoad_WebUIFields verifies round-trip for web_ui and cloud fields.
// AutoOpen=false requires the saved config to be at currentConfigVersion so
// the v7→v8 migration (which sets defaults for zero-value bools) does not run.
func TestSaveAndLoad_WebUIFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.Version = currentConfigVersion // prevent migrations from resetting AutoOpen
	cfg.WebUI.Port = 9090
	cfg.WebUI.Bind = "localhost"
	cfg.WebUI.AutoOpen = false
	cfg.Cloud.URL = "https://custom.huginncloud.com"

	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if loaded.WebUI.Port != 9090 {
		t.Errorf("expected WebUI.Port=9090, got %d", loaded.WebUI.Port)
	}
	if loaded.WebUI.Bind != "localhost" {
		t.Errorf("expected WebUI.Bind=localhost, got %q", loaded.WebUI.Bind)
	}
	if loaded.WebUI.AutoOpen {
		t.Error("expected WebUI.AutoOpen=false after save/load")
	}
	if loaded.Cloud.URL != "https://custom.huginncloud.com" {
		t.Errorf("expected Cloud.URL=https://custom.huginncloud.com, got %q", loaded.Cloud.URL)
	}
}

// TestSaveAndLoad_VisionFields verifies round-trip for vision fields.
// Note: bool fields with omitempty won't serialize false values, so we test true and custom MaxImageSizeKB.
func TestSaveAndLoad_VisionFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Default()
	cfg.VisionEnabled = true
	cfg.MaxImageSizeKB = 4096

	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if !loaded.VisionEnabled {
		t.Errorf("expected VisionEnabled=true, got %v", loaded.VisionEnabled)
	}
	if loaded.MaxImageSizeKB != 4096 {
		t.Errorf("expected MaxImageSizeKB=4096, got %d", loaded.MaxImageSizeKB)
	}
}

// TestConfigMigrationV9toV10 verifies that a v9 config is migrated through
// v10 (SchedulerEnabled) and v11 (fixed port 8421).
func TestConfigMigrationV9toV10(t *testing.T) {
	v9 := `{"version":9,"theme":"dark"}`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(v9), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != currentConfigVersion {
		t.Errorf("want version %d, got %d", currentConfigVersion, cfg.Version)
	}
	if !cfg.SchedulerEnabled {
		t.Error("want SchedulerEnabled=true after migration, got false")
	}
	if cfg.WebUI.Port != 8421 {
		t.Errorf("want WebUI.Port=8421 after migration, got %d", cfg.WebUI.Port)
	}
}
