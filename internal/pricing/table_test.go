package pricing_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/pricing"
)

func TestDefaultTable_KnownModels(t *testing.T) {
	tests := []struct {
		model          string
		wantPrompt     float64
		wantCompletion float64
	}{
		{"claude-opus-4-6", 3.00, 15.00},
		{"claude-sonnet-4-6", 3.00, 15.00},
		{"gpt-4o", 2.50, 10.00},
		{"gpt-4o-mini", 0.15, 0.60},
	}
	for _, tt := range tests {
		entry, ok := pricing.DefaultTable[tt.model]
		if !ok {
			t.Errorf("model %q not found in DefaultTable", tt.model)
			continue
		}
		if entry.PromptPer1M != tt.wantPrompt {
			t.Errorf("%q PromptPer1M = %v, want %v", tt.model, entry.PromptPer1M, tt.wantPrompt)
		}
		if entry.CompletionPer1M != tt.wantCompletion {
			t.Errorf("%q CompletionPer1M = %v, want %v", tt.model, entry.CompletionPer1M, tt.wantCompletion)
		}
	}
}

func TestLoadTable_NoOverride_ReturnsDefaults(t *testing.T) {
	tbl, err := pricing.LoadTable("")
	if err != nil {
		t.Fatalf("LoadTable error: %v", err)
	}
	entry, ok := tbl["gpt-4o"]
	if !ok {
		t.Fatal("expected gpt-4o in loaded table")
	}
	if entry.PromptPer1M != 2.50 {
		t.Errorf("PromptPer1M = %v, want 2.50", entry.PromptPer1M)
	}
}

func TestLoadTable_UserOverride_MergesOverDefaults(t *testing.T) {
	tmp := t.TempDir()
	overridePath := filepath.Join(tmp, "pricing.json")
	overrideJSON := `{
        "gpt-4o": {"prompt": 1.00, "completion": 4.00},
        "my-custom-model": {"prompt": 0.10, "completion": 0.20}
    }`
	if err := os.WriteFile(overridePath, []byte(overrideJSON), 0600); err != nil {
		t.Fatal(err)
	}
	tbl, err := pricing.LoadTable(overridePath)
	if err != nil {
		t.Fatalf("LoadTable error: %v", err)
	}
	if tbl["gpt-4o"].PromptPer1M != 1.00 {
		t.Errorf("expected overridden gpt-4o prompt=1.00, got %v", tbl["gpt-4o"].PromptPer1M)
	}
	if tbl["gpt-4o-mini"].PromptPer1M != 0.15 {
		t.Errorf("expected default gpt-4o-mini prompt=0.15, got %v", tbl["gpt-4o-mini"].PromptPer1M)
	}
	if tbl["my-custom-model"].PromptPer1M != 0.10 {
		t.Errorf("expected custom model prompt=0.10, got %v", tbl["my-custom-model"].PromptPer1M)
	}
}

func TestLoadTable_UserOverride_MissingFile_ReturnsDefaults(t *testing.T) {
	tbl, err := pricing.LoadTable("/nonexistent/path/pricing.json")
	if err != nil {
		t.Fatalf("LoadTable should not error on missing override: %v", err)
	}
	if len(tbl) == 0 {
		t.Error("expected non-empty table from defaults")
	}
}

func TestLoadTable_UserOverride_InvalidJSON_ReturnsError(t *testing.T) {
	tmp := t.TempDir()
	badPath := filepath.Join(tmp, "pricing.json")
	os.WriteFile(badPath, []byte("not json"), 0600)
	_, err := pricing.LoadTable(badPath)
	if err == nil {
		t.Error("expected error for invalid JSON override")
	}
}

func TestCalculateCost_KnownModel(t *testing.T) {
	tbl := pricing.DefaultTable
	cost := pricing.CalculateCost(tbl, "gpt-4o", 1_000_000, 1_000_000)
	if cost != 12.50 {
		t.Errorf("CalculateCost = %v, want 12.50", cost)
	}
}

func TestCalculateCost_SmallRequest(t *testing.T) {
	tbl := pricing.DefaultTable
	cost := pricing.CalculateCost(tbl, "gpt-4o", 100, 50)
	expected := 100.0/1_000_000*2.50 + 50.0/1_000_000*10.00
	if cost != expected {
		t.Errorf("CalculateCost = %v, want %v", cost, expected)
	}
}

func TestCalculateCost_UnknownModel_ReturnsZero(t *testing.T) {
	tbl := pricing.DefaultTable
	cost := pricing.CalculateCost(tbl, "some-free-model", 1000, 500)
	if cost != 0 {
		t.Errorf("CalculateCost for unknown model = %v, want 0", cost)
	}
}

func TestCalculateCost_OllamaModel_ReturnsZero(t *testing.T) {
	tbl := pricing.DefaultTable
	cost := pricing.CalculateCost(tbl, "qwen2.5-coder:14b", 5000, 2000)
	if cost != 0 {
		t.Errorf("CalculateCost for ollama model = %v, want 0", cost)
	}
}
