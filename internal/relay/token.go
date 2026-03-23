package relay

import (
	"fmt"
	"sync"

	"github.com/zalando/go-keyring"
)

const (
	relayKeychainService = "huginn"
	relayKeychainKey     = "relay/machine-token"
)

// TokenStorer is the interface for saving, loading, and clearing a relay token.
type TokenStorer interface {
	Save(token string) error
	Load() (string, error)
	Clear() error
	IsRegistered() bool
}

// TokenStore is a keychain-backed token store using the OS keyring.
type TokenStore struct{}

// NewTokenStore returns a new TokenStore backed by the OS keyring.
func NewTokenStore() *TokenStore { return &TokenStore{} }

func (t *TokenStore) Save(token string) error {
	return keyring.Set(relayKeychainService, relayKeychainKey, token)
}

func (t *TokenStore) Load() (string, error) {
	tok, err := keyring.Get(relayKeychainService, relayKeychainKey)
	if err != nil {
		return "", fmt.Errorf("relay: no machine token stored (run 'huginn cloud register'): %w", err)
	}
	return tok, nil
}

func (t *TokenStore) Clear() error {
	return keyring.Delete(relayKeychainService, relayKeychainKey)
}

func (t *TokenStore) IsRegistered() bool {
	_, err := t.Load()
	return err == nil
}

// NewMemoryTokenStore returns an empty MemoryTokenStore for use in tests.
func NewMemoryTokenStore() *MemoryTokenStore { return &MemoryTokenStore{} }

// MemoryTokenStore is a test/CI alternative to TokenStore that uses in-memory storage.
// Safe for concurrent use.
type MemoryTokenStore struct {
	mu    sync.RWMutex
	token string
}

func (m *MemoryTokenStore) Save(token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.token = token
	return nil
}

func (m *MemoryTokenStore) Load() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.token == "" {
		return "", fmt.Errorf("no token")
	}
	return m.token, nil
}

func (m *MemoryTokenStore) Clear() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.token = ""
	return nil
}

func (m *MemoryTokenStore) IsRegistered() bool { _, err := m.Load(); return err == nil }

// ClearToken removes the stored machine token from the OS keyring.
func ClearToken() error {
	return NewTokenStore().Clear()
}
