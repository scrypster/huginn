package permissions

import (
	"fmt"
	"testing"

	"github.com/scrypster/huginn/internal/tools"
)

// TestGate_LRU_EvictsLeastRecentlyUsed verifies that when sessionAllowed
// exceeds sessionAllowedMaxEntries, the least-recently-used entry is evicted
// rather than the oldest-created entry (FIFO) or a random entry.
func TestGate_LRU_EvictsLeastRecentlyUsed(t *testing.T) {
	// Fill the gate to the cap by AllowAll-ing N tools.
	g := NewGate(false, func(req PermissionRequest) Decision {
		return AllowAll
	})

	// Add (cap) entries in order: tool_0, tool_1, … tool_(cap-1).
	// After filling, touch tool_0 again (making it the MRU).
	// The next AllowAll call should evict tool_1 (the new LRU), not tool_0.
	cap := sessionAllowedMaxEntries

	for i := 0; i < cap; i++ {
		toolName := fmt.Sprintf("tool_%d", i)
		ok := g.Check(PermissionRequest{
			ToolName: toolName,
			Level:    tools.PermWrite,
		})
		if !ok {
			t.Fatalf("Check should allow tool %q via AllowAll", toolName)
		}
	}

	// All cap entries should be in sessionAllowed.
	g.mu.Lock()
	if len(g.sessionAllowed) != cap {
		t.Errorf("expected %d sessionAllowed entries, got %d", cap, len(g.sessionAllowed))
	}
	g.mu.Unlock()

	// Re-access tool_0 to make it the MRU (moving it to front of LRU list).
	g.mu.Lock()
	g.lruTouch("tool_0")
	g.mu.Unlock()

	// Now trigger eviction by adding one more entry (cap+1 total).
	g.Check(PermissionRequest{
		ToolName: "tool_overflow",
		Level:    tools.PermWrite,
	})

	g.mu.Lock()
	defer g.mu.Unlock()

	// The total should still be capped.
	if len(g.sessionAllowed) != cap {
		t.Errorf("after overflow, expected %d entries, got %d", cap, len(g.sessionAllowed))
	}

	// tool_0 should still be present (it was recently used).
	if !g.sessionAllowed["tool_0"] {
		t.Error("tool_0 should not have been evicted (it was the MRU)")
	}

	// tool_overflow should be present (just added).
	if !g.sessionAllowed["tool_overflow"] {
		t.Error("tool_overflow should be in sessionAllowed after being added")
	}

	// tool_1 should have been evicted (it became the LRU after tool_0 was touched).
	if g.sessionAllowed["tool_1"] {
		t.Error("tool_1 should have been evicted as the LRU entry")
	}
}

// TestGate_LRU_NoCapOnSmallWorkloads verifies that the LRU machinery does not
// corrupt sessionAllowed when fewer than sessionAllowedMaxEntries tools are used.
func TestGate_LRU_NoCapOnSmallWorkloads(t *testing.T) {
	g := NewGate(false, func(req PermissionRequest) Decision {
		return AllowAll
	})

	const n = 10
	for i := 0; i < n; i++ {
		toolName := fmt.Sprintf("small_tool_%d", i)
		if !g.Check(PermissionRequest{ToolName: toolName, Level: tools.PermWrite}) {
			t.Fatalf("expected allow for %q", toolName)
		}
	}

	g.mu.Lock()
	if len(g.sessionAllowed) != n {
		t.Errorf("expected %d entries, got %d", n, len(g.sessionAllowed))
	}
	if g.lruList.Len() != n {
		t.Errorf("expected lruList len=%d, got %d", n, g.lruList.Len())
	}
	g.mu.Unlock()
}

// TestGate_LRU_ForkPreservesOrder verifies that Fork copies LRU order correctly.
func TestGate_LRU_ForkPreservesOrder(t *testing.T) {
	g := NewGate(false, func(req PermissionRequest) Decision { return AllowAll })

	for _, name := range []string{"alpha", "beta", "gamma"} {
		g.Check(PermissionRequest{ToolName: name, Level: tools.PermWrite})
	}

	// Touch "alpha" to make it MRU.
	g.mu.Lock()
	g.lruTouch("alpha")
	g.mu.Unlock()

	fork := g.Fork(nil, nil)

	fork.mu.Lock()
	defer fork.mu.Unlock()

	// All three entries should be present in the fork.
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if !fork.sessionAllowed[name] {
			t.Errorf("fork missing sessionAllowed entry %q", name)
		}
		if _, ok := fork.lruItems[name]; !ok {
			t.Errorf("fork missing lruItems entry %q", name)
		}
	}

	// MRU should be "alpha" (front of list).
	if front := fork.lruList.Front(); front == nil || front.Value.(string) != "alpha" {
		val := ""
		if front != nil {
			val = front.Value.(string)
		}
		t.Errorf("expected front of forked lruList to be 'alpha', got %q", val)
	}
}
