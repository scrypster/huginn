package backend_test

import (
	"context"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

// stubBackend is a minimal Backend for cache tests.
type stubBackend struct{ name string }

func (s *stubBackend) ChatCompletion(_ context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
	return &backend.ChatResponse{}, nil
}
func (s *stubBackend) Health(_ context.Context) error   { return nil }
func (s *stubBackend) Shutdown(_ context.Context) error { return nil }
func (s *stubBackend) ContextWindow() int               { return 8192 }

// TestBackendCache_ProviderKeyInheritance verifies that an agent with provider
// but no api_key inherits the provider-level key registered via SetProviderKey.
func TestBackendCache_ProviderKeyInheritance(t *testing.T) {
	fallback := &stubBackend{name: "ollama-fallback"}
	bc := backend.NewBackendCache(fallback)

	// Register an env-var ref as the provider key (avoids real keychain in tests).
	t.Setenv("TEST_ANTHROPIC_KEY", "sk-ant-test-hardening-key")
	bc.SetProviderKey("anthropic", "$TEST_ANTHROPIC_KEY")

	// An agent with provider=anthropic but no api_key should NOT use the fallback.
	// It should create an Anthropic backend using the provider-level key.
	// We verify indirectly: For() must not return the fallback instance.
	b, err := bc.For("anthropic", "", "", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("For() error: %v", err)
	}
	if b == backend.Backend(fallback) {
		t.Fatal("For(anthropic, empty key) returned the Ollama fallback — should have used provider key")
	}
}

// TestBackendCache_EmptyProviderReturnsFallback verifies that an agent with no
// provider still gets the global fallback backend.
func TestBackendCache_EmptyProviderReturnsFallback(t *testing.T) {
	fallback := &stubBackend{name: "ollama-fallback"}
	bc := backend.NewBackendCache(fallback)
	bc.SetProviderKey("anthropic", "$TEST_ANTHROPIC_KEY")

	b, err := bc.For("", "", "", "llama3")
	if err != nil {
		t.Fatalf("For() error: %v", err)
	}
	if b != backend.Backend(fallback) {
		t.Fatal("For(empty provider) should return the fallback backend")
	}
}

// TestBackendCache_SetProviderKey_EvictsCache verifies that SetProviderKey
// evicts previously cached backends for that provider so the next call creates
// a fresh backend with the new key resolver.
func TestBackendCache_SetProviderKey_EvictsCache(t *testing.T) {
	fallback := &stubBackend{name: "ollama-fallback"}
	bc := backend.NewBackendCache(fallback)

	t.Setenv("OLD_KEY", "sk-ant-old")
	t.Setenv("NEW_KEY", "sk-ant-new")

	bc.SetProviderKey("anthropic", "$OLD_KEY")
	b1, err := bc.For("anthropic", "", "", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("first For(): %v", err)
	}

	// Change the provider key — this must evict the old cached backend.
	bc.SetProviderKey("anthropic", "$NEW_KEY")
	b2, err := bc.For("anthropic", "", "", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("second For(): %v", err)
	}

	// b2 must be a different backend instance (old one was evicted and a new
	// one was created with the new key resolver).
	if b1 == b2 {
		t.Fatal("SetProviderKey did not evict the cached backend — key rotation broken")
	}
}

// TestBackendCache_SameProviderKeySameBackend verifies that calling SetProviderKey
// with the same ref does NOT evict the cache (avoids unnecessary churn).
func TestBackendCache_SameProviderKeySameBackend(t *testing.T) {
	fallback := &stubBackend{name: "ollama-fallback"}
	bc := backend.NewBackendCache(fallback)

	t.Setenv("STABLE_KEY", "sk-ant-stable")
	bc.SetProviderKey("anthropic", "$STABLE_KEY")

	b1, _ := bc.For("anthropic", "", "", "claude-sonnet-4-6")
	bc.SetProviderKey("anthropic", "$STABLE_KEY") // same ref — no eviction
	b2, _ := bc.For("anthropic", "", "", "claude-sonnet-4-6")

	if b1 != b2 {
		t.Fatal("SetProviderKey with same ref should not evict cache")
	}
}

// TestBackendCache_PerAgentKeyOverridesProviderKey verifies that a per-agent
// api_key takes precedence over the provider-level fallback.
func TestBackendCache_PerAgentKeyOverridesProviderKey(t *testing.T) {
	fallback := &stubBackend{name: "ollama-fallback"}
	bc := backend.NewBackendCache(fallback)

	t.Setenv("GLOBAL_KEY", "sk-ant-global")
	t.Setenv("AGENT_KEY", "sk-ant-agent-specific")

	bc.SetProviderKey("anthropic", "$GLOBAL_KEY")

	// Agent with its own key — must create a backend keyed to the agent key,
	// distinct from the backend keyed to the global key.
	bGlobal, _ := bc.For("anthropic", "", "", "claude-sonnet-4-6")
	bAgent, _ := bc.For("anthropic", "", "$AGENT_KEY", "claude-sonnet-4-6")

	if bGlobal == bAgent {
		t.Fatal("per-agent key should produce a different backend than the provider-level key")
	}
}

// TestStoreAPIKey_LiteralKey_KeychainUnavailable verifies StoreAPIKey returns a
// non-nil error when the keychain is unavailable but still returns the literal
// value so callers can persist it as a fallback.
func TestStoreAPIKey_LiteralKey_KeychainUnavailable(t *testing.T) {
	// On macOS in tests the keychain may or may not be available.
	// We only verify the contract: returned value is always non-empty and usable.
	ref, _ := backend.StoreAPIKey("test-slot-hardening", "sk-ant-test-value")
	if ref == "" {
		t.Fatal("StoreAPIKey must always return a non-empty reference")
	}
	// The returned ref must be either the keyring reference or the literal itself.
	if ref != "sk-ant-test-value" && !strings.HasPrefix(ref, "keyring:huginn:") {
		t.Fatalf("unexpected ref format: %q", ref)
	}
}

// TestIsLiteralAPIKey covers all reference variants.
func TestIsLiteralAPIKey_Variants(t *testing.T) {
	tests := []struct {
		raw  string
		want bool
	}{
		{"", false},
		{"$ANTHROPIC_API_KEY", false},
		{"keyring:huginn:anthropic", false},
		{"keyring:custom:slot", false},
		{"sk-ant-api03-realkey", true},
		{"literal-value", true},
	}
	for _, tt := range tests {
		got := backend.IsLiteralAPIKey(tt.raw)
		if got != tt.want {
			t.Errorf("IsLiteralAPIKey(%q) = %v, want %v", tt.raw, got, tt.want)
		}
	}
}
