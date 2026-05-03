package skills

import (
	"sync"
	"testing"
)

// TestCombinedRuleContent_Cache verifies that repeated calls to
// CombinedRuleContent return the same value without recomputing when the
// registry has not changed (cache hit path).
func TestCombinedRuleContent_Cache(t *testing.T) {
	t.Parallel()

	reg := NewSkillRegistry()
	if err := reg.Register(&stubSkill{name: "skill-a", rules: "## Rules A\ndo not do bad things"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	first := reg.CombinedRuleContent()
	second := reg.CombinedRuleContent()

	if first == "" {
		t.Fatal("expected non-empty combined rule content")
	}
	if first != second {
		t.Errorf("second call returned different value: %q vs %q", first, second)
	}

	// After the first call the dirty flag should be clear.
	reg.mu.RLock()
	dirty := reg.rulesDirty
	reg.mu.RUnlock()
	if dirty {
		t.Error("expected rulesDirty=false after CombinedRuleContent call")
	}
}

// TestCombinedRuleContent_DirtyAfterRegister verifies that registering a new
// skill invalidates the rules cache so the next call returns updated content.
func TestCombinedRuleContent_DirtyAfterRegister(t *testing.T) {
	t.Parallel()

	reg := NewSkillRegistry()
	if err := reg.Register(&stubSkill{name: "skill-a", rules: "rules A"}); err != nil {
		t.Fatalf("Register skA: %v", err)
	}

	first := reg.CombinedRuleContent()
	if first == "" {
		t.Fatal("expected non-empty rules after first skill")
	}

	if err := reg.Register(&stubSkill{name: "skill-b", rules: "rules B"}); err != nil {
		t.Fatalf("Register skB: %v", err)
	}

	// Dirty flag should be set again after registration.
	reg.mu.RLock()
	dirty := reg.rulesDirty
	reg.mu.RUnlock()
	if !dirty {
		t.Error("expected rulesDirty=true after Register of second skill")
	}

	second := reg.CombinedRuleContent()
	if second == first {
		t.Error("expected rule content to change after second skill was registered")
	}
	// Both rule strings should appear in the combined result.
	if !containsSubstring(second, "rules A") || !containsSubstring(second, "rules B") {
		t.Errorf("combined rules missing expected content: %q", second)
	}
}

// TestCombinedRuleContent_DirtyAfterNotifyReload verifies that NotifyReload
// marks the rules cache dirty so the next call recomputes.
func TestCombinedRuleContent_DirtyAfterNotifyReload(t *testing.T) {
	t.Parallel()

	reg := NewSkillRegistry()
	if err := reg.Register(&stubSkill{name: "skill-x", rules: "rules X"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Warm the cache.
	_ = reg.CombinedRuleContent()

	// Verify dirty flag is clear (cache is warm).
	reg.mu.RLock()
	dirtyBefore := reg.rulesDirty
	reg.mu.RUnlock()
	if dirtyBefore {
		t.Error("expected rulesDirty=false after first CombinedRuleContent call")
	}

	// Trigger reload.
	reg.NotifyReload()

	reg.mu.RLock()
	dirtyAfter := reg.rulesDirty
	reg.mu.RUnlock()
	if !dirtyAfter {
		t.Error("expected rulesDirty=true after NotifyReload")
	}

	// Next call should succeed (re-warm the cache).
	result := reg.CombinedRuleContent()
	if result == "" {
		t.Error("expected non-empty result after NotifyReload recompute")
	}
}

// TestCombinedRuleContent_Concurrent verifies the rules cache is race-free
// under concurrent reads and writes.
func TestCombinedRuleContent_Concurrent(t *testing.T) {
	t.Parallel()

	reg := NewSkillRegistry()
	if err := reg.Register(&stubSkill{name: "skill-concurrent", rules: "concurrent rules"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = reg.CombinedRuleContent()
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
