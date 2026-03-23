package memory

import (
	"fmt"
	"sync"

	"github.com/zalando/go-keyring"
)

const (
	muninnKeychainService = "huginn"
	muninnKeychainKey     = "muninndb/root"
)

// PasswordStore abstracts root password storage (keychain or in-memory for tests).
type PasswordStore interface {
	StorePassword(password string) error
	GetPassword() (string, error)
	DeletePassword() error
}

// KeychainPasswordStore uses the OS keychain via go-keyring.
type KeychainPasswordStore struct{}

func (k *KeychainPasswordStore) StorePassword(password string) error {
	if err := keyring.Set(muninnKeychainService, muninnKeychainKey, password); err != nil {
		return fmt.Errorf("muninn keychain: store password: %w", err)
	}
	return nil
}

func (k *KeychainPasswordStore) GetPassword() (string, error) {
	pwd, err := keyring.Get(muninnKeychainService, muninnKeychainKey)
	if err != nil {
		return "", fmt.Errorf("muninn keychain: get password: %w", err)
	}
	return pwd, nil
}

func (k *KeychainPasswordStore) DeletePassword() error {
	if err := keyring.Delete(muninnKeychainService, muninnKeychainKey); err != nil {
		return fmt.Errorf("muninn keychain: delete password: %w", err)
	}
	return nil
}

// MemoryPasswordStore is an in-memory PasswordStore for tests and CI.
type MemoryPasswordStore struct {
	mu  sync.Mutex
	pwd string
	set bool
}

// NewMemoryPasswordStore returns an empty MemoryPasswordStore.
func NewMemoryPasswordStore() *MemoryPasswordStore { return &MemoryPasswordStore{} }

func (m *MemoryPasswordStore) StorePassword(password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pwd = password
	m.set = true
	return nil
}

func (m *MemoryPasswordStore) GetPassword() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.set {
		return "", fmt.Errorf("muninn: no password stored")
	}
	return m.pwd, nil
}

func (m *MemoryPasswordStore) DeletePassword() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pwd = ""
	m.set = false
	return nil
}

// NewKeychainPasswordStore returns the best available PasswordStore.
// Falls back to MemoryPasswordStore if OS keychain is unavailable (CI, Docker).
func NewKeychainPasswordStore() PasswordStore {
	ks := &KeychainPasswordStore{}
	if err := keyring.Set(muninnKeychainService, "__huginn_muninn_probe__", "probe"); err == nil {
		_ = keyring.Delete(muninnKeychainService, "__huginn_muninn_probe__")
		return ks
	}
	return NewMemoryPasswordStore()
}
