package radar

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLayerConfig_FromFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	yamlContent := `layers:
  api:
    - "src/api/*"
    - "src/routes/*"
  domain:
    - "src/models/*"
  infra:
    - "src/db/*"
`
	if err := os.WriteFile(filepath.Join(dir, radarConfigFile), []byte(yamlContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadLayerConfig(dir)
	if err != nil {
		t.Fatalf("LoadLayerConfig: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.Layers) != 3 {
		t.Errorf("expected 3 layers, got %d", len(cfg.Layers))
	}
	apiPatterns := cfg.Layers["api"]
	if len(apiPatterns) != 2 {
		t.Errorf("api patterns: got %d, want 2", len(apiPatterns))
	}
	if apiPatterns[0] != "src/api/*" {
		t.Errorf("api[0]: got %q, want %q", apiPatterns[0], "src/api/*")
	}
}

func TestLoadLayerConfig_MissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	cfg, err := LoadLayerConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Errorf("expected nil config for missing file, got %+v", cfg)
	}
}

func TestLoadLayerConfig_InvalidYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, radarConfigFile), []byte("{{invalid yaml"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadLayerConfig(dir)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if cfg != nil {
		t.Error("expected nil config on error")
	}
}

func TestLoadLayerConfig_EmptyFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, radarConfigFile), []byte(""), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadLayerConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty YAML produces a zero-value struct, not nil
	if cfg == nil {
		t.Fatal("expected non-nil config for empty file")
	}
	if len(cfg.Layers) != 0 {
		t.Errorf("expected 0 layers, got %d", len(cfg.Layers))
	}
}
