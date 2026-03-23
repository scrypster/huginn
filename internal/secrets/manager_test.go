package secrets_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/secrets"
)

func newTestManager(t *testing.T) *secrets.Manager {
	t.Helper()
	kr := secrets.NewMemoryKeyring()
	fs := secrets.NewFileStore(filepath.Join(t.TempDir(), "secrets.json"))
	return secrets.NewManager(kr, fs)
}

// ─── Store / Resolve round-trip ───────────────────────────────────────────────

func TestStoreResolveKeychain(t *testing.T) {
	m := newTestManager(t)

	ref, err := m.Store("anthropic", "sk-test-key")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if ref != "keyring:huginn:anthropic" {
		t.Fatalf("unexpected ref %q", ref)
	}

	val, err := m.Resolve(ref)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if val != "sk-test-key" {
		t.Fatalf("got %q, want %q", val, "sk-test-key")
	}
}

func TestStoreResolveFileStoreFallback(t *testing.T) {
	// Simulate keyring unavailable by using a broken keyring.
	kr := &brokenKeyring{}
	fs := secrets.NewFileStore(filepath.Join(t.TempDir(), "secrets.json"))
	m := secrets.NewManager(kr, fs)

	ref, err := m.Store("brave", "BSA-key")
	if err != nil {
		t.Fatalf("Store with broken keyring: %v", err)
	}
	// Reference format is identical regardless of backend.
	if ref != "keyring:huginn:brave" {
		t.Fatalf("unexpected ref %q", ref)
	}

	val, err := m.Resolve(ref)
	if err != nil {
		t.Fatalf("Resolve from file store: %v", err)
	}
	if val != "BSA-key" {
		t.Fatalf("got %q, want %q", val, "BSA-key")
	}
}

// ─── Keychain-missing fallback on Resolve ─────────────────────────────────────

func TestResolveFallsBackToFileStoreWhenKeychainEmpty(t *testing.T) {
	// Store directly in file store (simulates migration from another machine).
	fs := secrets.NewFileStore(filepath.Join(t.TempDir(), "secrets.json"))
	if err := fs.Set("openai", "sk-openai"); err != nil {
		t.Fatalf("file store Set: %v", err)
	}

	// Keyring is empty (new machine / copied ~/.huginn/).
	kr := secrets.NewMemoryKeyring()
	m := secrets.NewManager(kr, fs)

	val, err := m.Resolve("keyring:huginn:openai")
	if err != nil {
		t.Fatalf("Resolve should fall back to file store: %v", err)
	}
	if val != "sk-openai" {
		t.Fatalf("got %q, want sk-openai", val)
	}
}

func TestResolveMissingKeyReturnsHelpfulError(t *testing.T) {
	m := newTestManager(t)
	_, err := m.Resolve("keyring:huginn:anthropic")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	// Error message must guide the user.
	if !containsSubstr(err.Error(), "re-enter") {
		t.Fatalf("error should hint user to re-enter key, got: %v", err)
	}
}

// ─── Resolve: other formats ───────────────────────────────────────────────────

func TestResolveEmpty(t *testing.T) {
	m := newTestManager(t)
	v, err := m.Resolve("")
	if err != nil || v != "" {
		t.Fatalf("empty string should resolve to empty, got %q err %v", v, err)
	}
}

func TestResolveEnvVar(t *testing.T) {
	t.Setenv("HUGINN_TEST_KEY", "env-value")
	m := newTestManager(t)
	v, err := m.Resolve("$HUGINN_TEST_KEY")
	if err != nil {
		t.Fatalf("Resolve $ENV: %v", err)
	}
	if v != "env-value" {
		t.Fatalf("got %q, want env-value", v)
	}
}

func TestResolveEnvVarMissing(t *testing.T) {
	os.Unsetenv("HUGINN_TEST_KEY_MISSING")
	m := newTestManager(t)
	_, err := m.Resolve("$HUGINN_TEST_KEY_MISSING")
	if err == nil {
		t.Fatal("expected error for unset env var")
	}
}

func TestResolveLiteral(t *testing.T) {
	m := newTestManager(t)
	v, err := m.Resolve("literal-key")
	if err != nil || v != "literal-key" {
		t.Fatalf("literal should pass through, got %q err %v", v, err)
	}
}

// ─── Delete ───────────────────────────────────────────────────────────────────

func TestDelete(t *testing.T) {
	m := newTestManager(t)

	if _, err := m.Store("slack", "xoxb-token"); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := m.Delete("slack"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// After deletion, the key should not be found.
	_, err := m.Resolve("keyring:huginn:slack")
	if err == nil {
		t.Fatal("expected error after deletion")
	}
}

// ─── Status / List ────────────────────────────────────────────────────────────

func TestStatusUnset(t *testing.T) {
	m := newTestManager(t)
	s := m.Status("anthropic")
	if s.Set {
		t.Fatal("expected unset status")
	}
	if s.Storage != secrets.StorageNone {
		t.Fatalf("expected StorageNone, got %v", s.Storage)
	}
}

func TestStatusSetKeychain(t *testing.T) {
	m := newTestManager(t)
	m.Store("anthropic", "sk-key") //nolint:errcheck
	s := m.Status("anthropic")
	if !s.Set {
		t.Fatal("expected set status")
	}
	if s.Storage != secrets.StorageKeychain {
		t.Fatalf("expected StorageKeychain, got %v", s.Storage)
	}
}

func TestStatusSetFile(t *testing.T) {
	kr := &brokenKeyring{}
	fs := secrets.NewFileStore(filepath.Join(t.TempDir(), "secrets.json"))
	m := secrets.NewManager(kr, fs)

	m.Store("openai", "sk-oa") //nolint:errcheck
	s := m.Status("openai")
	if !s.Set {
		t.Fatal("expected set status")
	}
	if s.Storage != secrets.StorageFile {
		t.Fatalf("expected StorageFile, got %v", s.Storage)
	}
}

func TestList(t *testing.T) {
	m := newTestManager(t)
	m.Store("anthropic", "k1") //nolint:errcheck
	result := m.List([]string{"anthropic", "openai"})
	if !result["anthropic"].Set {
		t.Error("anthropic should be set")
	}
	if result["openai"].Set {
		t.Error("openai should not be set")
	}
}

// ─── FileStore permissions ────────────────────────────────────────────────────

func TestFileStorePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	fs := secrets.NewFileStore(path)

	if err := fs.Set("test", "value"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600, got %04o", info.Mode().Perm())
	}
}

// ─── SetDefault ───────────────────────────────────────────────────────────────

func TestSetDefault(t *testing.T) {
	kr := secrets.NewMemoryKeyring()
	fs := secrets.NewFileStore(filepath.Join(t.TempDir(), "secrets.json"))
	m := secrets.NewManager(kr, fs)
	secrets.SetDefault(m)

	if _, err := secrets.Store("anthropic", "sk-via-default"); err != nil {
		t.Fatalf("Store via Default: %v", err)
	}
	val, err := secrets.Resolve("keyring:huginn:anthropic")
	if err != nil {
		t.Fatalf("Resolve via Default: %v", err)
	}
	if val != "sk-via-default" {
		t.Fatalf("got %q", val)
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// brokenKeyring always fails every operation.
type brokenKeyring struct{}

func (b *brokenKeyring) Get(_, _ string) (string, error) {
	return "", fmt.Errorf("keyring unavailable")
}
func (b *brokenKeyring) Set(_, _, _ string) error {
	return fmt.Errorf("keyring unavailable")
}
func (b *brokenKeyring) Delete(_, _ string) error {
	return fmt.Errorf("keyring unavailable")
}

func containsSubstr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
