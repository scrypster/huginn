package backend

import (
	"testing"
)

// TestBackendCache_InvalidateCache_ForcesNewBackendCreation verifies that after
// InvalidateCache(), a subsequent For() creates a new backend instance rather than
// reusing the stale cached one.
func TestBackendCache_InvalidateCache_ForcesNewBackendCreation(t *testing.T) {
	c := NewBackendCache(nil)

	b1, err := c.For("ollama", "http://localhost:11434", "", "qwen2.5-coder")
	if err != nil {
		t.Fatalf("initial For(): %v", err)
	}

	c.InvalidateCache()

	b2, err := c.For("ollama", "http://localhost:11434", "", "qwen2.5-coder")
	if err != nil {
		t.Fatalf("post-invalidate For(): %v", err)
	}
	if b1 == b2 {
		t.Error("expected different backend instance after InvalidateCache (stale entry should be evicted)")
	}
}

// TestBackendCache_InvalidateCache_ClearsAllEntries verifies that InvalidateCache
// evicts all cached backends, not just a single slot.
func TestBackendCache_InvalidateCache_ClearsAllEntries(t *testing.T) {
	c := NewBackendCache(nil)

	b1, _ := c.For("ollama", "http://localhost:11434", "", "model-a")
	b2, _ := c.For("ollama", "http://localhost:11435", "", "model-b")

	c.InvalidateCache()

	b1After, _ := c.For("ollama", "http://localhost:11434", "", "model-a")
	b2After, _ := c.For("ollama", "http://localhost:11435", "", "model-b")

	if b1 == b1After {
		t.Error("expected new backend for model-a after InvalidateCache")
	}
	if b2 == b2After {
		t.Error("expected new backend for model-b after InvalidateCache")
	}
}

// TestBackendCache_LiveKeyUpdate_EndToEnd simulates the full "user sets API key
// via web UI" flow:
//  1. Server starts with empty fallback key → BackendCache built with no key.
//  2. User enters key via PUT /api/v1/secrets/anthropic.
//  3. WithFallbackAPIKey + InvalidateCache are called.
//  4. Next For() creates a fresh backend with the new key reference.
//
// This is the regression test for the 401 "x-api-key header is required" bug.
func TestBackendCache_LiveKeyUpdate_EndToEnd(t *testing.T) {
	t.Setenv("HUGINN_LIVE_KEY", "sk-ant-live-key")

	c := NewBackendCache(nil) // starts with no fallback key

	// Phase 1: before the user sets the key.
	preKeyBackend, err := c.For("anthropic", "", "", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("pre-key For(): %v", err)
	}

	// Phase 2: user sets the API key via PUT /api/v1/secrets/anthropic.
	// handleSetSecret calls s.orch.UpdateFallbackAPIKey(ref), which calls:
	//   bc.WithFallbackAPIKey(rawRef)
	//   bc.InvalidateCache()
	c.WithFallbackAPIKey("$HUGINN_LIVE_KEY")
	c.InvalidateCache()

	// Phase 3: next For() builds a fresh backend with the live key.
	postKeyBackend, err := c.For("anthropic", "", "", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("post-key For(): %v", err)
	}
	if postKeyBackend == nil {
		t.Fatal("expected non-nil backend post-key")
	}

	// Stale backend must be evicted; post-key backend is a new instance.
	if preKeyBackend == postKeyBackend {
		t.Error("expected different backend after live key update (cache miss proves invalidation worked)")
	}

	// Subsequent calls reuse the post-key backend (normal caching resumes).
	postKeyBackend2, err := c.For("anthropic", "", "", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("post-key For() #2: %v", err)
	}
	if postKeyBackend != postKeyBackend2 {
		t.Error("expected same cached backend for repeated calls after key update")
	}
}

// TestBackendCache_WithFallbackAPIKey_ChangesKeyUsedForNewBackends verifies
// that after updating the fallback key, new For() calls use the new key
// reference (resulting in a different cache entry than the old key).
func TestBackendCache_WithFallbackAPIKey_ChangesKeyUsedForNewBackends(t *testing.T) {
	t.Setenv("OLD_KEY", "sk-ant-old")
	t.Setenv("NEW_KEY", "sk-ant-new")

	c := NewBackendCache(nil).WithFallbackAPIKey("$OLD_KEY")

	// Backend created with old key.
	b1, err := c.For("anthropic", "", "", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("old-key For(): %v", err)
	}

	// Update key and invalidate.
	c.WithFallbackAPIKey("$NEW_KEY")
	c.InvalidateCache()

	// Backend created with new key — different instance.
	b2, err := c.For("anthropic", "", "", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("new-key For(): %v", err)
	}
	if b1 == b2 {
		t.Error("expected different backend instance after WithFallbackAPIKey + InvalidateCache")
	}
}
