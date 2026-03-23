package relay_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/relay"
)

func TestTokenStore_IsRegisteredFalseWhenEmpty(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	if store.IsRegistered() {
		t.Error("expected IsRegistered() == false on empty MemoryTokenStore")
	}
	if err := store.Save("test-token"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !store.IsRegistered() {
		t.Error("expected IsRegistered() == true after Save")
	}
}

func TestTokenStore_Load_ReturnsToken(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	if err := store.Save("my-jwt"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	tok, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if tok != "my-jwt" {
		t.Errorf("Load: got %q, want %q", tok, "my-jwt")
	}
}

func TestTokenStore_Clear_RemovesToken(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	store.Save("test-token")
	if err := store.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if store.IsRegistered() {
		t.Error("expected IsRegistered() == false after Clear")
	}
}

func TestTokenStore_Load_ErrorWhenEmpty(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	_, err := store.Load()
	if err == nil {
		t.Error("expected error from Load() on empty store, got nil")
	}
}

func TestMemoryTokenStore_ImplementsTokenStorer(t *testing.T) {
	var _ relay.TokenStorer = (*relay.MemoryTokenStore)(nil)
}

func TestTokenStore_ImplementsTokenStorer(t *testing.T) {
	var _ relay.TokenStorer = (*relay.TokenStore)(nil)
}

// TestMemoryTokenStore_SaveAndLoadRoundTrip verifies that a token can be saved
// and loaded back correctly.
func TestMemoryTokenStore_SaveAndLoadRoundTrip(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	testToken := "jwt.token.here"

	if err := store.Save(testToken); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded != testToken {
		t.Errorf("token mismatch: expected %q, got %q", testToken, loaded)
	}
}

// TestMemoryTokenStore_MultipleSave_OverwritesPrevious verifies that
// saving a new token overwrites the previous one.
func TestMemoryTokenStore_MultipleSave_OverwritesPrevious(t *testing.T) {
	store := &relay.MemoryTokenStore{}

	if err := store.Save("token1"); err != nil {
		t.Fatalf("Save token1: %v", err)
	}
	if err := store.Save("token2"); err != nil {
		t.Fatalf("Save token2: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded != "token2" {
		t.Errorf("expected second token to be loaded, got %q", loaded)
	}
}

// TestMemoryTokenStore_Clear_ClearsToken verifies that Clear() removes
// the stored token.
func TestMemoryTokenStore_Clear_ClearsToken(t *testing.T) {
	store := &relay.MemoryTokenStore{}
	store.Save("token")

	if err := store.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	_, err := store.Load()
	if err == nil {
		t.Error("expected error after Clear(), got nil")
	}
}

// TestMemoryTokenStore_IsRegistered_DependsOnLoad verifies that
// IsRegistered() relies on Load() working.
func TestMemoryTokenStore_IsRegistered_DependsOnLoad(t *testing.T) {
	store := &relay.MemoryTokenStore{}

	// Empty store should not be registered
	if store.IsRegistered() {
		t.Error("empty store should not be registered")
	}

	// After saving, should be registered
	store.Save("token")
	if !store.IsRegistered() {
		t.Error("store should be registered after Save")
	}

	// After clearing, should not be registered
	store.Clear()
	if store.IsRegistered() {
		t.Error("store should not be registered after Clear")
	}
}
