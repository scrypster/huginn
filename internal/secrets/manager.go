package secrets

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

const keychainService = "huginn"

// StorageType describes where a secret is physically held.
type StorageType string

const (
	StorageKeychain StorageType = "keychain"
	StorageFile     StorageType = "file"
	StorageEnv      StorageType = "env"
	StorageNone     StorageType = "none"
)

// SecretStatus reports whether a secret slot is populated and where it is stored.
type SecretStatus struct {
	Set     bool        `json:"set"`
	Storage StorageType `json:"storage"`
}

// KnownSlots is the authoritative list of slots managed by Huginn.
// Server-side handlers enforce this list to prevent the secrets file from
// becoming a general-purpose key-value store.
var KnownSlots = []string{
	"anthropic", "openai", "openrouter", "brave",
	"google", "github", "slack", "jira", "bitbucket",
}

// Manager stores and resolves secrets using the OS keychain as primary storage
// with ~/.huginn/secrets.json as the persistent fallback for headless environments.
// It is safe for concurrent use.
type Manager struct {
	kr        KeyringProvider
	fileStore *FileStore
	mu        sync.RWMutex
}

// NewManager creates a Manager that probes the OS keychain and falls back to
// the provided FileStore if the keychain is unavailable.
func NewManager(kr KeyringProvider, fs *FileStore) *Manager {
	return &Manager{kr: kr, fileStore: fs}
}

// Store saves value under slot in the best available backend.
// It always returns the canonical reference string "keyring:huginn:<slot>"
// regardless of which backend was used — callers store this in config.json.
//
// Priority: OS keychain → secrets.json fallback.
func (m *Manager) Store(slot, value string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.kr.Set(keychainService, slot, value); err == nil {
		slog.Debug("secrets: stored in OS keychain", "slot", slot)
		return fmt.Sprintf("keyring:%s:%s", keychainService, slot), nil
	}
	// Keychain unavailable (headless Linux, Docker, CI) — persist to file store.
	if err := m.fileStore.Set(slot, value); err != nil {
		return "", fmt.Errorf("secrets: store failed for slot %q (tried keychain and file store): %w", slot, err)
	}
	slog.Info("secrets: OS keychain unavailable, stored in secrets file", "slot", slot)
	return fmt.Sprintf("keyring:%s:%s", keychainService, slot), nil
}

// Resolve returns the actual secret value from a raw config reference.
//
//   - ""                      → returns ""
//   - "$ENV_VAR"              → reads the environment variable
//   - "keyring:svc:user"      → tries OS keychain, then secrets.json fallback
//   - anything else           → returned as-is (literal value)
func (m *Manager) Resolve(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}

	if strings.HasPrefix(raw, "$") {
		envVar := strings.TrimPrefix(raw, "$")
		val := os.Getenv(envVar)
		if val == "" {
			return "", fmt.Errorf("secrets: environment variable %q is empty or unset", envVar)
		}
		return val, nil
	}

	if strings.HasPrefix(raw, "keyring:") {
		parts := strings.SplitN(strings.TrimPrefix(raw, "keyring:"), ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", fmt.Errorf("secrets: invalid keyring reference %q, expected \"keyring:<service>:<slot>\"", raw)
		}
		service, slot := parts[0], parts[1]

		// Try OS keychain first.
		m.mu.RLock()
		val, err := m.kr.Get(service, slot)
		m.mu.RUnlock()
		if err == nil {
			return val, nil
		}

		// Keychain failed (key deleted, machine migration, headless). Try file store.
		val, err = m.fileStore.Get(slot)
		if err == nil {
			return val, nil
		}

		return "", fmt.Errorf("secrets: key not found in keychain or secrets file for slot %q — re-enter your API key in Settings", slot)
	}

	// Literal value — returned as-is.
	return raw, nil
}

// Delete removes the secret for slot from both the OS keychain and the file store.
// Callers do not need to know which backend holds it.
func (m *Manager) Delete(slot string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Best-effort deletion from both backends; ignore "not found" errors.
	_ = m.kr.Delete(keychainService, slot)
	_ = m.fileStore.Delete(slot)
	return nil
}

// Status returns the set/unset status and storage type for a single slot.
// It resolves where the secret is stored without returning its value.
func (m *Manager) Status(slot string) SecretStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ref := fmt.Sprintf("keyring:%s:%s", keychainService, slot)

	if _, err := m.kr.Get(keychainService, slot); err == nil {
		return SecretStatus{Set: true, Storage: StorageKeychain}
	}
	if m.fileStore.Has(slot) {
		return SecretStatus{Set: true, Storage: StorageFile}
	}
	_ = ref
	return SecretStatus{Set: false, Storage: StorageNone}
}

// List returns the status of every slot in knownSlots.
func (m *Manager) List(knownSlots []string) map[string]SecretStatus {
	out := make(map[string]SecretStatus, len(knownSlots))
	for _, s := range knownSlots {
		out[s] = m.Status(s)
	}
	return out
}

// ─── Package-level default manager ───────────────────────────────────────────

var (
	defaultMu      sync.RWMutex
	defaultManager *Manager
)

// Default returns the package-level default Manager.
// It is initialised lazily on first call using the OS keychain and the
// default ~/.huginn/secrets.json file store.
func Default() *Manager {
	defaultMu.RLock()
	m := defaultManager
	defaultMu.RUnlock()
	if m != nil {
		return m
	}

	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultManager != nil {
		return defaultManager
	}

	fsPath, err := DefaultFileStorePath()
	if err != nil {
		// Extremely unlikely (no home dir). Fall back to temp path.
		slog.Warn("secrets: cannot determine home dir for secrets file, using /tmp fallback", "err", err)
		fsPath = os.TempDir() + "/huginn-secrets.json"
	}

	defaultManager = NewManager(OSKeyring{}, NewFileStore(fsPath))
	return defaultManager
}

// SetDefault replaces the package-level default Manager.
// Used in tests to inject a MemoryKeyring + temp-dir FileStore.
func SetDefault(m *Manager) {
	defaultMu.Lock()
	defaultManager = m
	defaultMu.Unlock()
}

// IsLiteralKey returns true if raw is a plain API key value — not an env var
// reference ("$VAR"), not a keyring reference ("keyring:..."), and not empty.
func IsLiteralKey(raw string) bool {
	if raw == "" {
		return false
	}
	return !strings.HasPrefix(raw, "$") && !strings.HasPrefix(raw, "keyring:")
}

// probeKeychain tests whether the OS keychain is reachable without returning
// the probe-value to callers. Used by NewSecretStoreProbed (connections pkg).
func probeKeychain(kr KeyringProvider) bool {
	const testSlot = "__huginn_api_key_probe__"
	if err := kr.Set(keychainService, testSlot, "probe"); err != nil {
		return false
	}
	_ = kr.Delete(keychainService, testSlot)
	return true
}

// KeychainAvailable reports whether the OS keychain is reachable.
func KeychainAvailable() bool {
	return probeKeychain(OSKeyring{})
}

// ─── Convenience wrappers used by internal/backend/keyring.go ────────────────

// Store is a shortcut for Default().Store(slot, value).
func Store(slot, value string) (string, error) {
	return Default().Store(slot, value)
}

// Resolve is a shortcut for Default().Resolve(raw).
func Resolve(raw string) (string, error) {
	return Default().Resolve(raw)
}

// Delete is a shortcut for Default().Delete(slot).
func Delete(slot string) error {
	return Default().Delete(slot)
}

// Unused import guard for go-keyring (used via OSKeyring).
var _ = keyring.ErrNotFound
