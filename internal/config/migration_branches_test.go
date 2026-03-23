package config

// coverage_boost_test.go — additional tests to push config package to 90%+.
// Targets:
//   - migrateV1toV2: DiffReviewMode == "" branch
//   - migrateV3toV4: NotepadsMaxTokens == 0 and !NotepadsEnabled branches
//   - migrateV4toV5: CompactMode == "" and CompactTrigger == 0 branches
//   - migrateV6toV7: called (no-op, just needs to be executed)
//   - migrateV8toV9: called (no-op, just needs to be executed)
//   - migrateV7toV8: WebUI.Enabled=false, Bind="", AutoOpen=false, Cloud.URL="" branches
//   - generateMachineID: hostname=="" branch
//   - Load: os.UserHomeDir() error branch (cannot easily trigger — skip and note)
//   - SaveTo: WriteFile error (unwritable dir)

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// migrateV1toV2 — DiffReviewMode already set: condition is false, branch not hit.
// To hit the true branch (cfg.DiffReviewMode == ""), we must write a V1 config
// with an EXPLICIT empty string for diff_review_mode, overriding the Default().
// ---------------------------------------------------------------------------

func TestMigrateV1toV2_EmptyDiffReviewMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Write a V1 config that explicitly sets diff_review_mode to "".
	// When LoadFrom unmarshals this over Default(), diff_review_mode becomes "".
	raw := `{"version":1,"diff_review_mode":""}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	// Migration should have set it to "always".
	if cfg.DiffReviewMode != "always" {
		t.Errorf("expected diff_review_mode='always' after migrateV1toV2, got %q", cfg.DiffReviewMode)
	}
}

// migrateV1toV2 — DiffReviewMode already non-empty: no override.
func TestMigrateV1toV2_DiffReviewModePreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	raw := `{"version":1,"diff_review_mode":"never"}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.DiffReviewMode != "never" {
		t.Errorf("expected diff_review_mode='never' (preserved), got %q", cfg.DiffReviewMode)
	}
}

// ---------------------------------------------------------------------------
// migrateV3toV4 — NotepadsMaxTokens == 0 branch
// ---------------------------------------------------------------------------

func TestMigrateV3toV4_ZeroNotepadsMaxTokens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// V3 config with notepads_max_tokens explicitly 0.
	raw := `{"version":3,"notepads_max_tokens":0,"notepads_enabled":true}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.NotepadsMaxTokens != 8192 {
		t.Errorf("expected NotepadsMaxTokens=8192, got %d", cfg.NotepadsMaxTokens)
	}
}

// migrateV3toV4 — !NotepadsEnabled branch
func TestMigrateV3toV4_NotepadsEnabledFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// V3 config with notepads_enabled explicitly false.
	// Note: bool false is the zero value; json.Unmarshal sets it to false.
	raw := `{"version":3,"notepads_enabled":false,"notepads_max_tokens":8192}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if !cfg.NotepadsEnabled {
		t.Error("expected NotepadsEnabled=true after migrateV3toV4")
	}
}

// ---------------------------------------------------------------------------
// migrateV4toV5 — CompactMode == "" and CompactTrigger == 0 branches
// ---------------------------------------------------------------------------

func TestMigrateV4toV5_ZeroCompactFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// V4 config with compact_mode="" and compact_trigger=0 explicitly.
	raw := `{"version":4,"compact_mode":"","compact_trigger":0}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.CompactMode != "auto" {
		t.Errorf("expected CompactMode='auto', got %q", cfg.CompactMode)
	}
	if cfg.CompactTrigger != 0.70 {
		t.Errorf("expected CompactTrigger=0.70, got %f", cfg.CompactTrigger)
	}
}

// migrateV4toV5 — both fields already set: no override.
func TestMigrateV4toV5_FieldsPreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	raw := `{"version":4,"compact_mode":"never","compact_trigger":0.5}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.CompactMode != "never" {
		t.Errorf("expected CompactMode='never' (preserved), got %q", cfg.CompactMode)
	}
	if cfg.CompactTrigger != 0.5 {
		t.Errorf("expected CompactTrigger=0.5 (preserved), got %f", cfg.CompactTrigger)
	}
}

// ---------------------------------------------------------------------------
// migrateV6toV7 — no-op, just needs to execute (called when loading V6 config)
// ---------------------------------------------------------------------------

func TestMigrateV6toV7_CalledAndNoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	raw := `{"version":6}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	// After migration through V6→V7, the config should be at currentConfigVersion.
	if cfg.Version != currentConfigVersion {
		t.Errorf("expected version=%d after migrations, got %d", currentConfigVersion, cfg.Version)
	}
}

// ---------------------------------------------------------------------------
// migrateV8toV9 — no-op, just needs to execute (loaded above but let's be explicit)
// ---------------------------------------------------------------------------

func TestMigrateV8toV9_CalledExplicitly(t *testing.T) {
	cfg := Default()
	cfg.ActiveAgent = "my-agent"
	// Calling the migration on a config that already has ActiveAgent set: should be no-op.
	migrateV8toV9(cfg)
	if cfg.ActiveAgent != "my-agent" {
		t.Errorf("expected ActiveAgent='my-agent' (unchanged), got %q", cfg.ActiveAgent)
	}
}

func TestMigrateV6toV7_CalledExplicitly(t *testing.T) {
	cfg := Default()
	cfg.BraveAPIKey = "brave-key"
	// Calling the migration: should be no-op.
	migrateV6toV7(cfg)
	if cfg.BraveAPIKey != "brave-key" {
		t.Errorf("expected BraveAPIKey='brave-key' (unchanged), got %q", cfg.BraveAPIKey)
	}
}

func TestMigrateV0toV1_CalledExplicitly(t *testing.T) {
	cfg := Default()
	// migrateV0toV1 is a no-op; just verify it doesn't panic or mutate.
	before := cfg.ReasonerModel
	migrateV0toV1(cfg)
	if cfg.ReasonerModel != before {
		t.Errorf("migrateV0toV1 modified PlannerModel unexpectedly")
	}
}

// ---------------------------------------------------------------------------
// migrateV7toV8 — WebUI.Enabled=false, Bind="", AutoOpen=false, Cloud.URL="" branches
// These require a V7 config with those fields explicitly set to empty/false.
// ---------------------------------------------------------------------------

func TestMigrateV7toV8_AllZeroValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// V7 config with all web_ui and cloud fields explicitly unset/zero.
	raw := `{"version":7,"web_ui":{"enabled":false,"bind":"","auto_open":false},"cloud":{"url":""}}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if !cfg.WebUI.Enabled {
		t.Error("expected WebUI.Enabled=true after migrateV7toV8")
	}
	if cfg.WebUI.Bind != "127.0.0.1" {
		t.Errorf("expected WebUI.Bind='127.0.0.1', got %q", cfg.WebUI.Bind)
	}
	if !cfg.WebUI.AutoOpen {
		t.Error("expected WebUI.AutoOpen=true after migrateV7toV8")
	}
	if cfg.Cloud.URL != "https://huginncloud.com" {
		t.Errorf("expected Cloud.URL='https://huginncloud.com', got %q", cfg.Cloud.URL)
	}
}

// migrateV7toV8 — Bind already set: no override.
func TestMigrateV7toV8_BindPreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	raw := `{"version":7,"web_ui":{"enabled":true,"bind":"localhost","auto_open":true},"cloud":{"url":"https://custom.example.com"}}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.WebUI.Bind != "localhost" {
		t.Errorf("expected Bind='localhost' (preserved), got %q", cfg.WebUI.Bind)
	}
	if cfg.Cloud.URL != "https://custom.example.com" {
		t.Errorf("expected Cloud.URL='https://custom.example.com' (preserved), got %q", cfg.Cloud.URL)
	}
}

// ---------------------------------------------------------------------------
// generateMachineID — hostname == "" branch
// We can directly call the function; the branch is hard to trigger via env
// since Hostname() rarely fails, but we can call it directly and verify format.
// The 0% is due to generateMachineID not being covered from any test path that
// exercises the hostname=="" branch. We cover the function itself, relying on
// the existing format test in hardening_round5_test.go.
// ---------------------------------------------------------------------------

func TestGenerateMachineID_ReturnsNonEmpty(t *testing.T) {
	id := generateMachineID()
	if id == "" {
		t.Error("expected non-empty machine ID")
	}
	// Must have at least one dash.
	hasDash := false
	for _, c := range id {
		if c == '-' {
			hasDash = true
			break
		}
	}
	if !hasDash {
		t.Errorf("expected machine ID to contain '-', got %q", id)
	}
}

// ---------------------------------------------------------------------------
// SaveTo — WriteFile error: write to a path under a read-only directory.
// ---------------------------------------------------------------------------

func TestSaveTo_WriteFileError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: cannot test read-only directory")
	}

	dir := t.TempDir()
	subdir := filepath.Join(dir, "ro")
	if err := os.MkdirAll(subdir, 0o555); err != nil {
		t.Fatal(err)
	}

	cfg := Default()
	// This will fail because subdir is read-only (cannot write .tmp file inside it).
	err := cfg.SaveTo(filepath.Join(subdir, "config.json"))
	if err == nil {
		t.Error("expected error writing to read-only directory, got nil")
	}
}

// ---------------------------------------------------------------------------
// Load — UserHomeDir error branch: very hard to trigger on normal OS.
// Instead, test the common Load path (covered) plus a test that exercises
// the function when HOME is set to an inaccessible directory causing
// LoadFrom to fail with a non-ErrNotExist error. This exercises the Load
// success path with a real (temp) home dir.
// ---------------------------------------------------------------------------

func TestLoad_HomeSetToTempDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// No config file exists — should return defaults.
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg == nil {
		t.Error("expected non-nil config")
	}
	if cfg.OllamaBaseURL == "" {
		t.Error("expected non-empty OllamaBaseURL from defaults")
	}
}
