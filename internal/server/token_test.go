package server

import (
	"os"
	"path/filepath"
	"testing"
)

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func TestLoadOrCreateToken_CreatesOnFirstRun(t *testing.T) {
	dir := t.TempDir()
	tok, err := LoadOrCreateToken(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tok) != 64 {
		t.Errorf("expected token length 64, got %d", len(tok))
	}
	if !isHex(tok) {
		t.Errorf("token is not valid hex: %q", tok)
	}
}

func TestLoadOrCreateToken_LoadsExisting(t *testing.T) {
	dir := t.TempDir()
	tok1, err := LoadOrCreateToken(dir)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	tok2, err := LoadOrCreateToken(dir)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if tok1 != tok2 {
		t.Errorf("expected same token on reload, got %q vs %q", tok1, tok2)
	}
}

func TestLoadOrCreateToken_ReplacesInvalidToken(t *testing.T) {
	dir := t.TempDir()
	// Write an invalid token (too short)
	if err := os.WriteFile(filepath.Join(dir, "server.token"), []byte("short"), 0600); err != nil {
		t.Fatalf("failed to write short token: %v", err)
	}
	tok, err := LoadOrCreateToken(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tok) != 64 {
		t.Errorf("expected regenerated token length 64, got %d", len(tok))
	}
}
