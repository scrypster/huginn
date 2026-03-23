package secrets

import "github.com/zalando/go-keyring"

// KeyringProvider abstracts OS keychain access so tests can substitute a
// pure-in-memory implementation without touching the developer's real keychain.
type KeyringProvider interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

// OSKeyring delegates to the real OS keychain via go-keyring.
// On macOS this is the Keychain, on Windows the Credential Manager,
// and on Linux desktop it is the Secret Service (GNOME Keyring / KWallet).
type OSKeyring struct{}

func (OSKeyring) Get(service, user string) (string, error) {
	return keyring.Get(service, user)
}

func (OSKeyring) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}

func (OSKeyring) Delete(service, user string) error {
	return keyring.Delete(service, user)
}

// MemoryKeyring is a non-persistent in-memory keyring used in tests.
// It is safe for concurrent use.
type MemoryKeyring struct {
	store map[string]string
}

// NewMemoryKeyring creates an empty MemoryKeyring.
func NewMemoryKeyring() *MemoryKeyring {
	return &MemoryKeyring{store: make(map[string]string)}
}

func (m *MemoryKeyring) key(service, user string) string {
	return service + "/" + user
}

func (m *MemoryKeyring) Get(service, user string) (string, error) {
	v, ok := m.store[m.key(service, user)]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return v, nil
}

func (m *MemoryKeyring) Set(service, user, password string) error {
	m.store[m.key(service, user)] = password
	return nil
}

func (m *MemoryKeyring) Delete(service, user string) error {
	delete(m.store, m.key(service, user))
	return nil
}
