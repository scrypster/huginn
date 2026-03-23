package config

import "testing"

// TestValidateWarnings_ReturnedForClampedFields verifies that Validate returns
// a ValidationWarning for each field that was clamped to a safe default.
func TestValidateWarnings_ReturnedForClampedFields(t *testing.T) {
	cfg := Default()
	cfg.DiffReviewMode = "bogus"
	cfg.CompactMode = "invalid"
	cfg.CompactTrigger = 5.0
	cfg.MaxImageSizeKB = -1
	cfg.MaxTurns = -10
	cfg.BashTimeoutSecs = 0

	warnings, err := cfg.Validate()
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}

	// Expect 6 warnings: DiffReviewMode, CompactMode, CompactTrigger,
	// MaxImageSizeKB, MaxTurns, BashTimeoutSecs.
	if len(warnings) != 6 {
		t.Fatalf("expected 6 warnings, got %d: %v", len(warnings), warnings)
	}

	// Build a map for easy lookup.
	warnMap := make(map[string]ValidationWarning)
	for _, w := range warnings {
		warnMap[w.Field] = w
	}

	// Check DiffReviewMode.
	if w, ok := warnMap["DiffReviewMode"]; !ok {
		t.Error("missing warning for DiffReviewMode")
	} else {
		if w.OldValue != "bogus" {
			t.Errorf("DiffReviewMode OldValue: expected 'bogus', got %v", w.OldValue)
		}
		if w.NewValue != "always" {
			t.Errorf("DiffReviewMode NewValue: expected 'always', got %v", w.NewValue)
		}
	}

	// Check MaxTurns.
	if w, ok := warnMap["MaxTurns"]; !ok {
		t.Error("missing warning for MaxTurns")
	} else {
		if w.OldValue != -10 {
			t.Errorf("MaxTurns OldValue: expected -10, got %v", w.OldValue)
		}
		if w.NewValue != 50 {
			t.Errorf("MaxTurns NewValue: expected 50, got %v", w.NewValue)
		}
	}
}

// TestValidateWarnings_MaxTurnsTooHigh checks the clamp-to-max path.
func TestValidateWarnings_MaxTurnsTooHigh(t *testing.T) {
	cfg := Default()
	cfg.MaxTurns = 5000

	warnings, err := cfg.Validate()
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d", len(warnings))
	}
	w := warnings[0]
	if w.Field != "MaxTurns" {
		t.Errorf("expected Field='MaxTurns', got %q", w.Field)
	}
	if w.OldValue != 5000 {
		t.Errorf("expected OldValue=5000, got %v", w.OldValue)
	}
	if w.NewValue != 1000 {
		t.Errorf("expected NewValue=1000, got %v", w.NewValue)
	}
	if cfg.MaxTurns != 1000 {
		t.Errorf("expected MaxTurns clamped to 1000, got %d", cfg.MaxTurns)
	}
}

// TestValidateWarnings_NoWarningsForValidConfig ensures valid configs produce
// zero warnings.
func TestValidateWarnings_NoWarningsForValidConfig(t *testing.T) {
	cfg := Default()
	warnings, err := cfg.Validate()
	if err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings for valid config, got %d: %v", len(warnings), warnings)
	}
}

// TestValidateWarnings_ClampStillApplied confirms clamping occurs even though
// warnings are returned.
func TestValidateWarnings_ClampStillApplied(t *testing.T) {
	cfg := Default()
	cfg.BashTimeoutSecs = -5
	cfg.CompactTrigger = -1.0

	warnings, _ := cfg.Validate()
	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d", len(warnings))
	}
	if cfg.BashTimeoutSecs != 120 {
		t.Errorf("expected BashTimeoutSecs=120, got %d", cfg.BashTimeoutSecs)
	}
	if cfg.CompactTrigger != 0.70 {
		t.Errorf("expected CompactTrigger=0.70, got %f", cfg.CompactTrigger)
	}
}
