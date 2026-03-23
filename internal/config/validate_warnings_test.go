package config

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Validate() — field clamping tests
// ---------------------------------------------------------------------------

func TestValidate_DiffReviewMode(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.DiffReviewMode = "invalid"
	warnings, err := cfg.Validate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a warning for invalid DiffReviewMode")
	}
	if cfg.DiffReviewMode != "always" {
		t.Errorf("expected DiffReviewMode clamped to 'always', got %q", cfg.DiffReviewMode)
	}
}

func TestValidate_CompactMode(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.CompactMode = "invalid"
	warnings, err := cfg.Validate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a warning for invalid CompactMode")
	}
	if cfg.CompactMode != "auto" {
		t.Errorf("expected CompactMode clamped to 'auto', got %q", cfg.CompactMode)
	}
}

func TestValidate_CompactTrigger_TooHigh_Warning(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.CompactTrigger = 1.5
	warnings, err := cfg.Validate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a warning for CompactTrigger > 1.0")
	}
	if cfg.CompactTrigger != 0.70 {
		t.Errorf("expected CompactTrigger clamped to 0.70, got %v", cfg.CompactTrigger)
	}
}

func TestValidate_CompactTrigger_Negative_Warning(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.CompactTrigger = -0.1
	warnings, err := cfg.Validate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a warning for negative CompactTrigger")
	}
	if cfg.CompactTrigger != 0.70 {
		t.Errorf("expected CompactTrigger clamped to 0.70, got %v", cfg.CompactTrigger)
	}
}

func TestValidate_MaxImageSizeKB_Negative_Warning(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.MaxImageSizeKB = -1
	warnings, err := cfg.Validate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a warning for negative MaxImageSizeKB")
	}
	if cfg.MaxImageSizeKB != 2048 {
		t.Errorf("expected MaxImageSizeKB clamped to 2048, got %d", cfg.MaxImageSizeKB)
	}
}

func TestValidate_MaxTurns_Zero_Warning(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.MaxTurns = 0
	warnings, err := cfg.Validate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a warning for zero MaxTurns")
	}
	if cfg.MaxTurns != 50 {
		t.Errorf("expected MaxTurns clamped to 50, got %d", cfg.MaxTurns)
	}
}

func TestValidate_MaxTurns_TooHigh(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.MaxTurns = 10000
	warnings, err := cfg.Validate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a warning for MaxTurns > 1000")
	}
	if cfg.MaxTurns != 1000 {
		t.Errorf("expected MaxTurns clamped to 1000, got %d", cfg.MaxTurns)
	}
}

func TestValidate_BashTimeoutSecs_Negative(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.BashTimeoutSecs = -1
	warnings, err := cfg.Validate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected a warning for negative BashTimeoutSecs")
	}
	if cfg.BashTimeoutSecs != 120 {
		t.Errorf("expected BashTimeoutSecs clamped to 120, got %d", cfg.BashTimeoutSecs)
	}
}

// ---------------------------------------------------------------------------
// ValidateConfig() — error tests
// ---------------------------------------------------------------------------

func TestValidateConfig_ManagedWithoutAPIKey(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.Backend.Type = "managed"
	cfg.Backend.APIKey = ""
	err := ValidateConfig(*cfg)
	if err == nil {
		t.Fatal("expected error for managed backend without API key")
	}
}

func TestValidateConfig_BashTimeoutTooHigh(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.BashTimeoutSecs = 3601
	err := ValidateConfig(*cfg)
	if err == nil {
		t.Fatal("expected error for BashTimeoutSecs > 3600")
	}
}

func TestValidateConfig_ContextLimitKBNegative(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.ContextLimitKB = -1
	err := ValidateConfig(*cfg)
	if err == nil {
		t.Fatal("expected error for negative ContextLimitKB")
	}
}
