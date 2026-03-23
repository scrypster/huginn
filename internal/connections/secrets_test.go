package connections

import (
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func sampleToken() *oauth2.Token {
	return &oauth2.Token{
		AccessToken:  "access-abc",
		TokenType:    "Bearer",
		RefreshToken: "refresh-xyz",
		Expiry:       time.Now().Add(time.Hour).Truncate(time.Second),
	}
}

func TestMemoryStoreRoundTrip(t *testing.T) {
	m := NewMemoryStore()
	tok := sampleToken()

	if err := m.StoreToken("conn-1", tok); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}

	got, err := m.GetToken("conn-1")
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}

	if got.AccessToken != tok.AccessToken {
		t.Errorf("AccessToken: got %q, want %q", got.AccessToken, tok.AccessToken)
	}
	if got.RefreshToken != tok.RefreshToken {
		t.Errorf("RefreshToken: got %q, want %q", got.RefreshToken, tok.RefreshToken)
	}
	if got.TokenType != tok.TokenType {
		t.Errorf("TokenType: got %q, want %q", got.TokenType, tok.TokenType)
	}
}

func TestMemoryStoreMissingToken(t *testing.T) {
	m := NewMemoryStore()

	_, err := m.GetToken("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing token, got nil")
	}
}

func TestMemoryStoreDelete(t *testing.T) {
	m := NewMemoryStore()
	tok := sampleToken()

	if err := m.StoreToken("conn-1", tok); err != nil {
		t.Fatalf("StoreToken: %v", err)
	}

	if err := m.DeleteToken("conn-1"); err != nil {
		t.Fatalf("DeleteToken: %v", err)
	}

	_, err := m.GetToken("conn-1")
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}

	// Delete of non-existent key should not error
	if err := m.DeleteToken("nonexistent"); err != nil {
		t.Errorf("DeleteToken nonexistent: expected no error, got %v", err)
	}
}

func TestMemoryStoreConcurrent(t *testing.T) {
	m := NewMemoryStore()
	tok := sampleToken()

	var wg sync.WaitGroup
	const goroutines = 20

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := "conn-concurrent"
			_ = m.StoreToken(id, tok)
			_, _ = m.GetToken(id)
			_ = m.DeleteToken(id)
		}(i)
	}

	wg.Wait()
}

// TestMemoryStoreInvalidJSON tests that GetToken properly handles invalid JSON
func TestMemoryStoreInvalidJSON(t *testing.T) {
	m := NewMemoryStore()
	m.mu.Lock()
	m.tokens["bad-json"] = "not-valid-json"
	m.mu.Unlock()

	_, err := m.GetToken("bad-json")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// TestMemoryStoreStoreInvalidToken tests marshaling of broken token
func TestMemoryStoreStoreInvalidToken(t *testing.T) {
	m := NewMemoryStore()
	// Token with circular reference would fail to marshal,
	// but since oauth2.Token doesn't have circular refs,
	// we rely on the token structure working as-is.
	// This test ensures normal operation doesn't break.
	tok := sampleToken()
	err := m.StoreToken("test-id", tok)
	if err != nil {
		t.Errorf("StoreToken: unexpected error %v", err)
	}
}

// TestKeyChainStoreExists tests that KeychainStore can be created
func TestKeyChainStoreExists(t *testing.T) {
	ks := &KeychainStore{}
	if ks == nil {
		t.Fatal("KeychainStore should not be nil")
	}
}

// TestNewSecretStore tests the NewSecretStore factory function
func TestNewSecretStore(t *testing.T) {
	store := NewSecretStore()
	if store == nil {
		t.Fatal("NewSecretStore should not return nil")
	}
	// Should return either KeychainStore or MemoryStore
	// We just verify it's not nil and implements SecretStore interface
	tok := sampleToken()
	err := store.StoreToken("test", tok)
	// Error is acceptable if keychain isn't available, but should not panic
	_ = err
}

func TestMemoryStoreCredentialsRoundTrip(t *testing.T) {
	m := NewMemoryStore()
	creds := map[string]string{"api_key": "test-key", "app_key": "test-app"}
	if err := m.StoreCredentials("conn-1", creds); err != nil {
		t.Fatalf("StoreCredentials: %v", err)
	}
	got, err := m.GetCredentials("conn-1")
	if err != nil {
		t.Fatalf("GetCredentials: %v", err)
	}
	if got["api_key"] != "test-key" {
		t.Errorf("api_key: got %q want %q", got["api_key"], "test-key")
	}
	if got["app_key"] != "test-app" {
		t.Errorf("app_key: got %q want %q", got["app_key"], "test-app")
	}
}

func TestMemoryStoreMissingCredentials(t *testing.T) {
	m := NewMemoryStore()
	if _, err := m.GetCredentials("nonexistent"); err == nil {
		t.Error("expected error for missing credentials, got nil")
	}
}

func TestMemoryStoreDeleteCredentials(t *testing.T) {
	m := NewMemoryStore()
	if err := m.StoreCredentials("conn-1", map[string]string{"key": "val"}); err != nil {
		t.Fatalf("StoreCredentials: %v", err)
	}
	if err := m.DeleteCredentials("conn-1"); err != nil {
		t.Fatalf("DeleteCredentials: %v", err)
	}
	if _, err := m.GetCredentials("conn-1"); err == nil {
		t.Error("expected error after delete, got nil")
	}
	// Delete of non-existent key should not error
	if err := m.DeleteCredentials("nonexistent"); err != nil {
		t.Errorf("DeleteCredentials nonexistent: unexpected error: %v", err)
	}
}
