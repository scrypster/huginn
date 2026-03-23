package relay

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GetMachineID returns a stable, unique identifier for this machine.
// Format: <8hex> — a random suffix stored in ~/.huginn/machine_id.
// The ID is independent of hostname so it survives machine renames.
// The human-readable display name is set separately (os.Hostname at registration,
// editable in HuginnCloud thereafter).
func GetMachineID() string {
	return loadOrCreateMachineSuffix()
}

// loadOrCreateMachineSuffix returns the 8-char hex UUID suffix for this machine.
// Creates and persists a new random suffix if none exists yet.
func loadOrCreateMachineSuffix() string {
	path := machineSuffixPath()
	if data, err := os.ReadFile(path); err == nil {
		s := strings.TrimSpace(string(data))
		if len(s) == 8 && isHex(s) {
			return s
		}
	}
	// Generate new random suffix.
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fallback: use hostname hash (old behavior) — better than panic.
		h := sha256.Sum256([]byte(func() string { h, _ := os.Hostname(); return h }()))
		return fmt.Sprintf("%x", h[:4])
	}
	suffix := fmt.Sprintf("%x", b[:])
	_ = os.MkdirAll(filepath.Dir(path), 0o750)
	_ = os.WriteFile(path, []byte(suffix), 0o600)
	return suffix
}

func machineSuffixPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".huginn", "machine_id")
	}
	return filepath.Join(home, ".huginn", "machine_id")
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func sanitizeHostname(h string) string {
	var out []byte
	for _, c := range []byte(h) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			out = append(out, c)
		} else {
			out = append(out, '-')
		}
	}
	if len(out) > 24 {
		out = out[:24]
	}
	if len(out) == 0 {
		out = []byte("unknown")
	}
	return string(out)
}

// Identity holds the relay registration for this machine.
// Stored in ~/.huginn/relay.json.
type Identity struct {
	AgentID  string `json:"agent_id"`
	Endpoint string `json:"endpoint"`
	APIKey   string `json:"api_key,omitempty"`
}

// ErrNotRegistered is returned when no relay.json exists.
var ErrNotRegistered = errors.New("relay: not registered — run `huginn relay register`")

// LoadIdentity reads ~/.huginn/relay.json.
func LoadIdentity() (*Identity, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".huginn", "relay.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, ErrNotRegistered
	}
	if err != nil {
		return nil, err
	}
	var id Identity
	if err := json.Unmarshal(data, &id); err != nil {
		return nil, err
	}
	return &id, nil
}

// Save writes the identity to ~/.huginn/relay.json.
func (id *Identity) Save() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".huginn", "relay.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
