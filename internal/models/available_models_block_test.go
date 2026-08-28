package models

import (
	"strings"
	"testing"
)

func TestAvailableModelsBlock_BundledCatalog(t *testing.T) {
	ResetGlobalProviderCatalog()
	cat := GlobalProviderCatalog()
	block := cat.AvailableModelsBlock()

	if block == "" {
		t.Fatal("expected non-empty block from bundled catalog")
	}
	if !strings.Contains(block, "claude-opus-4-6") {
		t.Errorf("expected claude-opus-4-6 in block, got:\n%s", block)
	}
	if !strings.Contains(block, "$5.00/$25.00") {
		t.Errorf("expected opus pricing $5.00/$25.00 in block, got:\n%s", block)
	}
	if !strings.Contains(block, "claude-sonnet-4-6") {
		t.Errorf("expected claude-sonnet-4-6 in block, got:\n%s", block)
	}
	if !strings.Contains(block, "$3.00/$15.00") {
		t.Errorf("expected sonnet pricing $3.00/$15.00 in block, got:\n%s", block)
	}
	// Deprecated models must not clutter the CoS's model-choice prompt.
	if strings.Contains(block, "claude-opus-4-5") {
		t.Errorf("expected deprecated claude-opus-4-5 excluded from block, got:\n%s", block)
	}
	if strings.Contains(block, "claude-sonnet-4-5") {
		t.Errorf("expected deprecated claude-sonnet-4-5 excluded from block, got:\n%s", block)
	}
}

func TestAvailableModelsBlock_EmptyCatalog(t *testing.T) {
	cat := &ProviderCatalog{}
	if got := cat.AvailableModelsBlock(); got != "" {
		t.Errorf("expected empty block for empty catalog, got %q", got)
	}
}

func TestAvailableModelsBlock_OmitsUnpricedModels(t *testing.T) {
	cat := &ProviderCatalog{}
	if err := cat.load([]byte(`{
		"catalog_version": "test",
		"providers": {
			"local": {
				"my-local-model": {"display_name": "Local Model"}
			}
		}
	}`)); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cat.AvailableModelsBlock(); got != "" {
		t.Errorf("expected empty block when only unpriced models present, got %q", got)
	}
}
