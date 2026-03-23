package config

import (
	"os"
	"testing"
)

func TestValidate_DiffReviewMode_Valid(t *testing.T) {
	for _, mode := range []string{"always", "never", "auto"} {
		cfg := Default()
		cfg.DiffReviewMode = mode
		cfg.Validate()
		if cfg.DiffReviewMode != mode {
			t.Errorf("expected DiffReviewMode=%q to be preserved, got %q", mode, cfg.DiffReviewMode)
		}
	}
}

func TestValidate_DiffReviewMode_Invalid(t *testing.T) {
	cfg := Default()
	cfg.DiffReviewMode = "bogus"
	cfg.Validate()
	if cfg.DiffReviewMode != "always" {
		t.Errorf("expected DiffReviewMode='always' for invalid value, got %q", cfg.DiffReviewMode)
	}
}

func TestValidate_DiffReviewMode_Empty(t *testing.T) {
	cfg := Default()
	cfg.DiffReviewMode = ""
	cfg.Validate()
	if cfg.DiffReviewMode != "always" {
		t.Errorf("expected DiffReviewMode='always' for empty value, got %q", cfg.DiffReviewMode)
	}
}

func TestValidate_CompactMode_Valid(t *testing.T) {
	for _, mode := range []string{"auto", "never", "always"} {
		cfg := Default()
		cfg.CompactMode = mode
		cfg.Validate()
		if cfg.CompactMode != mode {
			t.Errorf("expected CompactMode=%q to be preserved, got %q", mode, cfg.CompactMode)
		}
	}
}

func TestValidate_CompactMode_Invalid(t *testing.T) {
	cfg := Default()
	cfg.CompactMode = "invalid"
	cfg.Validate()
	if cfg.CompactMode != "auto" {
		t.Errorf("expected CompactMode='auto' for invalid value, got %q", cfg.CompactMode)
	}
}

func TestValidate_CompactTrigger_Valid(t *testing.T) {
	cfg := Default()
	cfg.CompactTrigger = 0.5
	cfg.Validate()
	if cfg.CompactTrigger != 0.5 {
		t.Errorf("expected CompactTrigger=0.5, got %f", cfg.CompactTrigger)
	}
}

func TestValidate_CompactTrigger_Negative(t *testing.T) {
	cfg := Default()
	cfg.CompactTrigger = -0.1
	cfg.Validate()
	if cfg.CompactTrigger != 0.70 {
		t.Errorf("expected CompactTrigger=0.70 for negative value, got %f", cfg.CompactTrigger)
	}
}

func TestValidate_CompactTrigger_TooHigh(t *testing.T) {
	cfg := Default()
	cfg.CompactTrigger = 1.5
	cfg.Validate()
	if cfg.CompactTrigger != 0.70 {
		t.Errorf("expected CompactTrigger=0.70 for >1.0 value, got %f", cfg.CompactTrigger)
	}
}

func TestValidate_CompactTrigger_Boundary(t *testing.T) {
	cfg := Default()
	cfg.CompactTrigger = 1.0
	cfg.Validate()
	if cfg.CompactTrigger != 1.0 {
		t.Errorf("expected CompactTrigger=1.0 (boundary), got %f", cfg.CompactTrigger)
	}

	cfg.CompactTrigger = 0.0
	cfg.Validate()
	if cfg.CompactTrigger != 0.0 {
		t.Errorf("expected CompactTrigger=0.0 (boundary), got %f", cfg.CompactTrigger)
	}
}

func TestValidate_MaxImageSizeKB_Positive(t *testing.T) {
	cfg := Default()
	cfg.MaxImageSizeKB = 4096
	cfg.Validate()
	if cfg.MaxImageSizeKB != 4096 {
		t.Errorf("expected MaxImageSizeKB=4096, got %d", cfg.MaxImageSizeKB)
	}
}

func TestValidate_MaxImageSizeKB_Zero(t *testing.T) {
	cfg := Default()
	cfg.MaxImageSizeKB = 0
	cfg.Validate()
	if cfg.MaxImageSizeKB != 2048 {
		t.Errorf("expected MaxImageSizeKB=2048 for zero value, got %d", cfg.MaxImageSizeKB)
	}
}

func TestValidate_MaxImageSizeKB_Negative(t *testing.T) {
	cfg := Default()
	cfg.MaxImageSizeKB = -100
	cfg.Validate()
	if cfg.MaxImageSizeKB != 2048 {
		t.Errorf("expected MaxImageSizeKB=2048 for negative value, got %d", cfg.MaxImageSizeKB)
	}
}

func TestValidate_MaxTurns_Zero(t *testing.T) {
	cfg := Default()
	cfg.MaxTurns = 0
	cfg.Validate()
	if cfg.MaxTurns != 50 {
		t.Errorf("expected MaxTurns=50 for zero value, got %d", cfg.MaxTurns)
	}
}

func TestValidate_MaxTurns_Negative(t *testing.T) {
	cfg := Default()
	cfg.MaxTurns = -5
	cfg.Validate()
	if cfg.MaxTurns != 50 {
		t.Errorf("expected MaxTurns=50 for negative value, got %d", cfg.MaxTurns)
	}
}

func TestValidate_BashTimeoutSecs_Zero(t *testing.T) {
	cfg := Default()
	cfg.BashTimeoutSecs = 0
	cfg.Validate()
	if cfg.BashTimeoutSecs != 120 {
		t.Errorf("expected BashTimeoutSecs=120 for zero value, got %d", cfg.BashTimeoutSecs)
	}
}

func TestValidate_CalledByLoadFrom(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.json"

	raw := `{
		"diff_review_mode": "bogus",
		"compact_mode": "invalid",
		"compact_trigger": -5.0,
		"max_image_size_kb": -1,
		"max_turns": -10,
		"bash_timeout_secs": 0,
		"version": 5
	}`
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}

	if cfg.DiffReviewMode != "always" {
		t.Errorf("expected DiffReviewMode='always', got %q", cfg.DiffReviewMode)
	}
	if cfg.CompactMode != "auto" {
		t.Errorf("expected CompactMode='auto', got %q", cfg.CompactMode)
	}
	if cfg.CompactTrigger != 0.70 {
		t.Errorf("expected CompactTrigger=0.70, got %f", cfg.CompactTrigger)
	}
	if cfg.MaxImageSizeKB != 2048 {
		t.Errorf("expected MaxImageSizeKB=2048, got %d", cfg.MaxImageSizeKB)
	}
	if cfg.MaxTurns != 50 {
		t.Errorf("expected MaxTurns=50, got %d", cfg.MaxTurns)
	}
	if cfg.BashTimeoutSecs != 120 {
		t.Errorf("expected BashTimeoutSecs=120, got %d", cfg.BashTimeoutSecs)
	}
}
