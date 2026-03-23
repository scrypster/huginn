package backend

import (
	"context"
	"sync"
	"testing"
)

// mockBackend is a no-op Backend implementation for BackendCache tests.
type mockBackend struct {
	id              string
	shutdownCalled  bool
	shutdownMu      sync.Mutex
}

func (m *mockBackend) ChatCompletion(_ context.Context, _ ChatRequest) (*ChatResponse, error) {
	return &ChatResponse{Content: "mock"}, nil
}
func (m *mockBackend) Health(_ context.Context) error { return nil }
func (m *mockBackend) Shutdown(_ context.Context) error {
	m.shutdownMu.Lock()
	m.shutdownCalled = true
	m.shutdownMu.Unlock()
	return nil
}
func (m *mockBackend) ContextWindow() int { return 4096 }

// TestBackendCache_EmptyProvider_ReturnsFallback verifies that For("", ...) returns the fallback.
func TestBackendCache_EmptyProvider_ReturnsFallback(t *testing.T) {
	fallback := &mockBackend{id: "fallback"}
	c := NewBackendCache(fallback)

	b, err := c.For("", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b != Backend(fallback) {
		t.Fatal("expected fallback backend to be returned for empty provider")
	}
}

// TestBackendCache_NilFallback_EmptyProvider_ReturnsError verifies Fix 4.
func TestBackendCache_NilFallback_EmptyProvider_ReturnsError(t *testing.T) {
	c := NewBackendCache(nil)

	_, err := c.For("", "", "", "")
	if err == nil {
		t.Fatal("expected error when fallback is nil and provider is empty")
	}
}

// TestBackendCache_SameProviderEndpointKey_ReturnsSameInstance verifies caching.
func TestBackendCache_SameProviderEndpointKey_ReturnsSameInstance(t *testing.T) {
	c := NewBackendCache(nil)

	b1, err := c.For("ollama", "http://localhost:11434", "", "qwen2.5-coder")
	if err != nil {
		t.Fatalf("first For() error: %v", err)
	}
	b2, err := c.For("ollama", "http://localhost:11434", "", "qwen2.5-coder")
	if err != nil {
		t.Fatalf("second For() error: %v", err)
	}
	if b1 != b2 {
		t.Fatal("expected the same backend instance for identical provider+endpoint+key")
	}
}

// TestBackendCache_DifferentAPIKeys_GetDifferentBackends verifies Fix 3.
// Two Anthropic agents with different literal API keys must get different backend instances.
func TestBackendCache_DifferentAPIKeys_GetDifferentBackends(t *testing.T) {
	c := NewBackendCache(nil)

	b1, err := c.For("anthropic", "", "sk-ant-key-one", "claude-3-haiku-20240307")
	if err != nil {
		t.Fatalf("first For() error: %v", err)
	}
	b2, err := c.For("anthropic", "", "sk-ant-key-two", "claude-3-haiku-20240307")
	if err != nil {
		t.Fatalf("second For() error: %v", err)
	}
	if b1 == b2 {
		t.Fatal("expected different backend instances for different API keys on the same provider")
	}
}

// TestBackendCache_SameRawReference_ReturnsSameInstance verifies that
// identical raw reference strings produce the same cached backend.
func TestBackendCache_SameRawReference_ReturnsSameInstance(t *testing.T) {
	t.Setenv("TEST_CACHE_ANTHRO_KEY", "sk-ant-resolved-value")

	c := NewBackendCache(nil)

	// Two calls with the same raw reference should hit the same cache entry
	b1, err := c.For("anthropic", "", "$TEST_CACHE_ANTHRO_KEY", "claude-3-haiku-20240307")
	if err != nil {
		t.Fatalf("first For() error: %v", err)
	}
	b2, err := c.For("anthropic", "", "$TEST_CACHE_ANTHRO_KEY", "claude-3-haiku-20240307")
	if err != nil {
		t.Fatalf("second For() error: %v", err)
	}
	if b1 != b2 {
		t.Fatal("expected same backend instance for identical raw reference strings")
	}
}

// TestBackendCache_DifferentRawReference_GetsDifferentBackend verifies that
// "$ENV_VAR" and a literal value are treated as different backends even if
// they resolve to the same secret (cache keys are based on raw reference).
func TestBackendCache_DifferentRawReference_GetsDifferentBackend(t *testing.T) {
	t.Setenv("TEST_CACHE_ANTHRO_KEY", "sk-ant-resolved-value")

	c := NewBackendCache(nil)

	b1, err := c.For("anthropic", "", "$TEST_CACHE_ANTHRO_KEY", "claude-3-haiku-20240307")
	if err != nil {
		t.Fatalf("env-var For() error: %v", err)
	}
	b2, err := c.For("anthropic", "", "sk-ant-resolved-value", "claude-3-haiku-20240307")
	if err != nil {
		t.Fatalf("literal For() error: %v", err)
	}
	if b1 == b2 {
		t.Fatal("expected different backend instances for different raw reference strings")
	}
}

// TestBackendCache_Shutdown_ShutsDownAllBackends verifies Shutdown propagates.
func TestBackendCache_Shutdown_ShutsDownAllBackends(t *testing.T) {
	// Use real backends (ExternalBackend is a no-op on Shutdown) to verify no error path.
	c := NewBackendCache(nil)

	_, err := c.For("ollama", "http://localhost:11434", "", "model-a")
	if err != nil {
		t.Fatalf("For() error: %v", err)
	}
	_, err = c.For("ollama", "http://localhost:11435", "", "model-b")
	if err != nil {
		t.Fatalf("For() second error: %v", err)
	}

	ctx := context.Background()
	if err := c.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() returned unexpected error: %v", err)
	}
}

// TestBackendCache_Shutdown_CallsFallbackShutdown verifies fallback is also shut down.
func TestBackendCache_Shutdown_CallsFallbackShutdown(t *testing.T) {
	fallback := &mockBackend{id: "fallback"}
	c := NewBackendCache(fallback)

	ctx := context.Background()
	if err := c.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error: %v", err)
	}
	if !fallback.shutdownCalled {
		t.Error("expected Shutdown to be called on the fallback backend")
	}
}

// TestBackendCache_ConcurrentFor_NoPanic verifies Fix 1 (no data race under concurrent access).
// Run with: go test -race ./internal/backend/...
func TestBackendCache_ConcurrentFor_NoPanic(t *testing.T) {
	c := NewBackendCache(nil)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.For("ollama", "http://localhost:11434", "", "qwen2.5-coder")
		}()
	}
	wg.Wait()
}

// TestBackendCache_DifferentModels_GetDifferentBackends verifies that two calls with
// the same provider+endpoint+key but different model names return distinct backend instances.
// This ensures model-specific configuration (e.g. context window, sampling params)
// is not accidentally shared across models.
func TestBackendCache_DifferentModels_GetDifferentBackends(t *testing.T) {
	c := NewBackendCache(nil)

	b1, err := c.For("ollama", "http://localhost:11434", "", "qwen2.5-coder:7b")
	if err != nil {
		t.Fatalf("first For() error: %v", err)
	}
	b2, err := c.For("ollama", "http://localhost:11434", "", "llama3:8b")
	if err != nil {
		t.Fatalf("second For() error: %v", err)
	}
	if b1 == b2 {
		t.Fatal("expected different backend instances for different model names on the same provider+endpoint+key")
	}
}

// TestBackendCache_SameModel_ReturnsSameInstance verifies that repeated calls with
// identical provider+endpoint+key+model return the same cached instance.
func TestBackendCache_SameModel_ReturnsSameInstance(t *testing.T) {
	c := NewBackendCache(nil)

	b1, err := c.For("ollama", "http://localhost:11434", "", "qwen2.5-coder:7b")
	if err != nil {
		t.Fatalf("first For() error: %v", err)
	}
	b2, err := c.For("ollama", "http://localhost:11434", "", "qwen2.5-coder:7b")
	if err != nil {
		t.Fatalf("second For() error: %v", err)
	}
	if b1 != b2 {
		t.Fatal("expected same backend instance for identical provider+endpoint+key+model")
	}
}

// TestKeyFingerprint_DeterministicAndDistinct verifies the internal fingerprint function.
func TestKeyFingerprint_DeterministicAndDistinct(t *testing.T) {
	fp1 := keyFingerprint("key-one")
	fp2 := keyFingerprint("key-one")
	fp3 := keyFingerprint("key-two")

	if fp1 != fp2 {
		t.Errorf("fingerprint must be deterministic: %q != %q", fp1, fp2)
	}
	if fp1 == fp3 {
		t.Errorf("distinct keys must produce distinct fingerprints: both %q", fp1)
	}
	if len(fp1) != 8 {
		t.Errorf("expected 8 hex chars, got %d: %q", len(fp1), fp1)
	}
}
