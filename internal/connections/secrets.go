package connections

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
)

// SecretStore abstracts token storage so that implementations can use
// the OS keychain, an in-memory map (for tests/CI), or any other backend.
type SecretStore interface {
	StoreToken(connID string, token *oauth2.Token) error
	GetToken(connID string) (*oauth2.Token, error)
	DeleteToken(connID string) error

	StoreCredentials(connID string, creds map[string]string) error
	GetCredentials(connID string) (map[string]string, error)
	DeleteCredentials(connID string) error
}

const keychainService = "huginn"

func keychainKey(connID string) string { return "connection/" + connID }

func credsKey(connID string) string { return "creds/" + connID }

// KeychainStore uses the OS keychain (macOS Keychain, GNOME Keyring,
// Windows Credential Manager) via go-keyring.
//
// # Security model
//
// Tokens are serialised as JSON and stored verbatim in the platform keychain.
// Application-layer encryption is intentionally NOT added on top because:
//
//  1. macOS Keychain items are encrypted at rest using AES-256 with a key derived
//     from the user's login password. Access requires the user to be authenticated
//     and respects per-app access-control lists (ACLs).
//  2. GNOME Keyring (Linux) and Windows Credential Manager apply analogous
//     OS-managed encryption tied to the user session.
//  3. Adding a second encryption layer with an application-managed key does not
//     improve security if the key itself must be stored on the same machine — an
//     attacker with sufficient privilege to read the keychain can also read the
//     application key. Symmetric layering without a true hardware root of trust
//     (TPM/Secure Enclave) does not raise the threat bar meaningfully.
//
// Fallback behaviour (CI / Docker / SSH sessions without a keyring daemon):
// NewSecretStore() detects keychain unavailability via a probe write and falls
// back to MemoryStore, which holds tokens only for the duration of the process.
// Tokens are never written to a plaintext file on disk.
type KeychainStore struct{}

func (k *KeychainStore) StoreToken(connID string, token *oauth2.Token) error {
	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("keychain: marshal token: %w", err)
	}
	return keyring.Set(keychainService, keychainKey(connID), string(data))
}

func (k *KeychainStore) GetToken(connID string) (*oauth2.Token, error) {
	raw, err := keyring.Get(keychainService, keychainKey(connID))
	if err != nil {
		return nil, fmt.Errorf("keychain: get token %s: %w", connID, err)
	}
	var t oauth2.Token
	if err := json.Unmarshal([]byte(raw), &t); err != nil {
		return nil, fmt.Errorf("keychain: unmarshal token %s: %w", connID, err)
	}
	return &t, nil
}

func (k *KeychainStore) DeleteToken(connID string) error {
	return keyring.Delete(keychainService, keychainKey(connID))
}

func (k *KeychainStore) StoreCredentials(connID string, creds map[string]string) error {
	data, err := json.Marshal(creds)
	if err != nil {
		return fmt.Errorf("keychain: marshal credentials: %w", err)
	}
	return keyring.Set(keychainService, credsKey(connID), string(data))
}

func (k *KeychainStore) GetCredentials(connID string) (map[string]string, error) {
	raw, err := keyring.Get(keychainService, credsKey(connID))
	if err != nil {
		return nil, fmt.Errorf("keychain: get credentials %s: %w", connID, err)
	}
	var creds map[string]string
	if err := json.Unmarshal([]byte(raw), &creds); err != nil {
		return nil, fmt.Errorf("keychain: unmarshal credentials %s: %w", connID, err)
	}
	return creds, nil
}

func (k *KeychainStore) DeleteCredentials(connID string) error {
	return keyring.Delete(keychainService, credsKey(connID))
}

// MemoryStore is a SecretStore backed by an in-memory map.
// Suitable for tests and CI environments that lack an OS keychain.
// Tokens do not persist across process restarts.
type MemoryStore struct {
	mu     sync.Mutex
	tokens map[string]string
	creds  map[string]string
}

// NewMemoryStore creates an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		tokens: make(map[string]string),
		creds:  make(map[string]string),
	}
}

func (m *MemoryStore) StoreToken(connID string, token *oauth2.Token) error {
	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("memory store: marshal token: %w", err)
	}
	m.mu.Lock()
	m.tokens[connID] = string(data)
	m.mu.Unlock()
	return nil
}

func (m *MemoryStore) GetToken(connID string) (*oauth2.Token, error) {
	m.mu.Lock()
	raw, ok := m.tokens[connID]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("token not found: %s", connID)
	}
	var t oauth2.Token
	if err := json.Unmarshal([]byte(raw), &t); err != nil {
		return nil, fmt.Errorf("memory store: unmarshal token %s: %w", connID, err)
	}
	return &t, nil
}

func (m *MemoryStore) DeleteToken(connID string) error {
	m.mu.Lock()
	delete(m.tokens, connID)
	m.mu.Unlock()
	return nil
}

func (m *MemoryStore) StoreCredentials(connID string, creds map[string]string) error {
	data, err := json.Marshal(creds)
	if err != nil {
		return fmt.Errorf("memory store: marshal credentials: %w", err)
	}
	m.mu.Lock()
	m.creds[connID] = string(data)
	m.mu.Unlock()
	return nil
}

func (m *MemoryStore) GetCredentials(connID string) (map[string]string, error) {
	m.mu.Lock()
	raw, ok := m.creds[connID]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("credentials not found: %s", connID)
	}
	var creds map[string]string
	if err := json.Unmarshal([]byte(raw), &creds); err != nil {
		return nil, fmt.Errorf("memory store: unmarshal credentials %s: %w", connID, err)
	}
	return creds, nil
}

func (m *MemoryStore) DeleteCredentials(connID string) error {
	m.mu.Lock()
	delete(m.creds, connID)
	m.mu.Unlock()
	return nil
}

// NewSecretStore returns the best available SecretStore.
// It probes the OS keychain; if unavailable it falls back to MemoryStore.
// The fallback is appropriate for CI, Docker, and SSH sessions.
// keychainProbeTimeout bounds the keychain availability probe. It must be
// short: it runs on the startup path, before the HTTP server binds.
const keychainProbeTimeout = 3 * time.Second

func NewSecretStore() SecretStore {
	ks := &KeychainStore{}
	testKey := "__huginn_probe__"

	// Explicit opt-out. The keychain is only used to persist Connections
	// OAuth tokens; everything else works without it. Set this when running
	// headless, in CI/Docker, or from a freshly built binary that would
	// otherwise trigger a per-binary keychain ACL prompt.
	if v := os.Getenv("HUGINN_NO_KEYCHAIN"); v == "1" || strings.EqualFold(v, "true") {
		slog.Debug("connections: HUGINN_NO_KEYCHAIN set, using in-memory secret store")
		return NewMemoryStore()
	}

	// The probe must not be able to hang. On macOS go-keyring shells out to
	// the `security` binary, which blocks indefinitely waiting for a GUI
	// unlock prompt when the keychain is locked or the calling binary has no
	// ACL entry (keychain ACLs are per-binary, so a freshly built binary
	// always prompts). A bare keyring.Set therefore never returns the error
	// this fallback was written for, and startup hangs before the server
	// binds -- exactly the CI/Docker/SSH case the fallback exists to serve.
	done := make(chan error, 1)
	go func() { done <- keyring.Set(keychainService, testKey, "probe") }()

	select {
	case err := <-done:
		if err == nil {
			_ = keyring.Delete(keychainService, testKey)
			return ks
		}
		slog.Debug("connections: keychain unavailable, using in-memory secret store", "err", err)
	case <-keychainProbeTimer():
		// The goroutine and its `security` subprocess are left to finish on
		// their own; this runs once per process start, so it cannot accumulate.
		slog.Warn("connections: keychain probe timed out, using in-memory secret store",
			"timeout", keychainProbeTimeout)
	}
	return NewMemoryStore()
}

// keychainProbeTimer is a variable so tests can shorten the timeout.
var keychainProbeTimer = func() <-chan time.Time { return time.After(keychainProbeTimeout) }
