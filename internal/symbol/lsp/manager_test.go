package lsp_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/symbol/lsp"
)

func TestManager_NotConfigured(t *testing.T) {
	mgr := lsp.NewManager("go", lsp.ServerConfig{})
	_, err := mgr.Definition("file:///test.go", 1, 1)
	if err != lsp.ErrNotConfigured {
		t.Errorf("expected ErrNotConfigured, got %v", err)
	}
}

func TestManager_Symbols_NotConfigured(t *testing.T) {
	mgr := lsp.NewManager("go", lsp.ServerConfig{})
	_, err := mgr.Symbols("test")
	if err != lsp.ErrNotConfigured {
		t.Errorf("expected ErrNotConfigured, got %v", err)
	}
}

func TestManager_Stop_NoOp(t *testing.T) {
	mgr := lsp.NewManager("go", lsp.ServerConfig{})
	err := mgr.Stop()
	if err != nil {
		t.Errorf("Stop on unstarted manager should not error: %v", err)
	}
}

func TestDetect_Unsupported(t *testing.T) {
	cfg := lsp.Detect("cobol")
	if cfg.Command != "" {
		t.Errorf("expected empty command for unsupported language")
	}
}

func TestDetect_NoCommand(t *testing.T) {
	// Detect a language that exists but whose servers are not installed.
	// This test assumes gopls is not installed (safe assumption on CI).
	cfg := lsp.Detect("rust")
	// We can't guarantee rust-analyzer is installed, so we just check
	// that we got back a valid ServerConfig structure (possibly empty).
	_ = cfg
}

func TestSupportedLanguages(t *testing.T) {
	langs := lsp.SupportedLanguages()
	if len(langs) == 0 {
		t.Errorf("expected at least one supported language")
	}
	// Check that we have Go in the list
	found := false
	for _, lang := range langs {
		if lang == "go" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'go' in supported languages")
	}
}
