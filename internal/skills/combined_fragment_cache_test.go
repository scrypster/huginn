package skills

import (
	"sync"
	"testing"
)

// TestCombinedPromptFragment_Cache verifies that repeated calls to
// CombinedPromptFragment return the same value without recomputing when the
// registry has not changed (cache hit path).
func TestCombinedPromptFragment_Cache(t *testing.T) {
	t.Parallel()

	reg := NewSkillRegistry()
	if err := reg.Register(&stubSkill{name: "skill-a", prompt: "## A\nsome instructions"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	first := reg.CombinedPromptFragment()
	second := reg.CombinedPromptFragment()

	if first == "" {
		t.Fatal("expected non-empty combined fragment")
	}
	if first != second {
		t.Errorf("second call returned different value: %q vs %q", first, second)
	}

	// After the first call the dirty flag should be clear.
	reg.mu.RLock()
	dirty := reg.combinedDirty
	reg.mu.RUnlock()
	if dirty {
		t.Error("expected combinedDirty=false after CombinedPromptFragment call")
	}
}

// TestCombinedPromptFragment_DirtyAfterRegister verifies that registering a new
// skill invalidates the cache so the next call returns updated content.
func TestCombinedPromptFragment_DirtyAfterRegister(t *testing.T) {
	t.Parallel()

	reg := NewSkillRegistry()
	if err := reg.Register(&stubSkill{name: "skill-a", prompt: "instructions A"}); err != nil {
		t.Fatalf("Register skA: %v", err)
	}

	first := reg.CombinedPromptFragment()
	if first == "" {
		t.Fatal("expected non-empty fragment after first skill")
	}

	if err := reg.Register(&stubSkill{name: "skill-b", prompt: "instructions B"}); err != nil {
		t.Fatalf("Register skB: %v", err)
	}

	// Dirty flag should be set again after registration.
	reg.mu.RLock()
	dirty := reg.combinedDirty
	reg.mu.RUnlock()
	if !dirty {
		t.Error("expected combinedDirty=true after Register of second skill")
	}

	second := reg.CombinedPromptFragment()
	if second == first {
		t.Error("expected fragment to change after second skill was registered")
	}
	// Both skill names should appear in the combined fragment.
	if !containsSubstring(second, "skill-a") || !containsSubstring(second, "skill-b") {
		t.Errorf("combined fragment missing skill names: %q", second)
	}
}

// TestCombinedPromptFragment_DirtyAfterNotifyReload verifies that
// NotifyReload marks the cache dirty so the next call recomputes.
func TestCombinedPromptFragment_DirtyAfterNotifyReload(t *testing.T) {
	t.Parallel()

	reg := NewSkillRegistry()
	if err := reg.Register(&stubSkill{name: "skill-x", prompt: "instructions X"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Warm the cache.
	_ = reg.CombinedPromptFragment()

	// Verify dirty flag is clear (cache is warm).
	reg.mu.RLock()
	dirtyBefore := reg.combinedDirty
	reg.mu.RUnlock()
	if dirtyBefore {
		t.Error("expected combinedDirty=false after first CombinedPromptFragment call")
	}

	// Trigger reload.
	reg.NotifyReload()

	reg.mu.RLock()
	dirtyAfter := reg.combinedDirty
	reg.mu.RUnlock()
	if !dirtyAfter {
		t.Error("expected combinedDirty=true after NotifyReload")
	}

	// Next call should succeed (re-warm the cache).
	result := reg.CombinedPromptFragment()
	if result == "" {
		t.Error("expected non-empty result after NotifyReload recompute")
	}
}

// TestSetReloadCallback_Invoked verifies that the registered callback is called
// when NotifyReload fires.
func TestSetReloadCallback_Invoked(t *testing.T) {
	t.Parallel()

	reg := NewSkillRegistry()
	called := false
	reg.SetReloadCallback(func() {
		called = true
	})

	reg.NotifyReload()

	if !called {
		t.Error("expected reload callback to be invoked by NotifyReload")
	}
}

// TestSetReloadCallback_Nil verifies that setting a nil callback is safe.
func TestSetReloadCallback_Nil(t *testing.T) {
	t.Parallel()

	reg := NewSkillRegistry()
	reg.SetReloadCallback(func() {})
	reg.SetReloadCallback(nil) // clear it

	// Should not panic.
	reg.NotifyReload()
}

// TestSetReloadCallback_ReplacesExisting verifies that calling SetReloadCallback
// a second time replaces the previous callback.
func TestSetReloadCallback_ReplacesExisting(t *testing.T) {
	t.Parallel()

	reg := NewSkillRegistry()
	firstCalled := false
	reg.SetReloadCallback(func() { firstCalled = true })

	secondCalled := false
	reg.SetReloadCallback(func() { secondCalled = true })

	reg.NotifyReload()

	if firstCalled {
		t.Error("first callback should have been replaced, not called")
	}
	if !secondCalled {
		t.Error("second callback should have been called")
	}
}

// TestCombinedPromptFragment_Concurrent verifies the cache is race-free under
// concurrent reads and writes.
func TestCombinedPromptFragment_Concurrent(t *testing.T) {
	t.Parallel()

	reg := NewSkillRegistry()
	if err := reg.Register(&stubSkill{name: "skill-concurrent", prompt: "instructions"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = reg.CombinedPromptFragment()
		}()
	}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reg.NotifyReload()
		}()
	}
	wg.Wait()
}

// ---- helpers ----

func containsSubstring(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	if len(s) < len(sub) {
		return false
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
