package permissions

import (
	"fmt"
	"testing"

	"github.com/scrypster/huginn/internal/tools"
)

// TestGate_SessionAllowed_BoundedGrowth documents that the sessionAllowed map
// grows only as large as the number of unique tool names ever AllowAll-ed.
//
// P3-3: Gate.sessionAllowed is a map[string]bool keyed by tool name. In a real
// session, the number of distinct tools is bounded (O(10s)), so the map never
// grows large. This test documents that behavior as a regression guard.
//
// Conclusion: unbounded growth is NOT a practical concern because tool names
// are a small, finite set per session. Documented here; no code change needed.
func TestGate_SessionAllowed_BoundedGrowth(t *testing.T) {
	t.Parallel()

	const uniqueTools = 500 // far more than any real session would have

	// Each unique tool gets AllowAll — worst-case map growth.
	g := NewGate(false, func(req PermissionRequest) Decision {
		return AllowAll
	})

	for i := 0; i < uniqueTools; i++ {
		toolName := fmt.Sprintf("tool-%d", i)
		req := PermissionRequest{
			ToolName: toolName,
			Level:    tools.PermWrite,
		}
		if !g.Check(req) {
			t.Fatalf("expected AllowAll for tool %s, got deny", toolName)
		}
	}

	g.mu.Lock()
	mapSize := len(g.sessionAllowed)
	g.mu.Unlock()

	// Verify map size equals the number of unique tools (each added once).
	if mapSize != uniqueTools {
		t.Errorf("expected sessionAllowed map size %d, got %d", uniqueTools, mapSize)
	}

	// A second call for the same tool must NOT grow the map further.
	for i := 0; i < uniqueTools; i++ {
		toolName := fmt.Sprintf("tool-%d", i)
		req := PermissionRequest{
			ToolName: toolName,
			Level:    tools.PermWrite,
		}
		if !g.Check(req) {
			t.Fatalf("expected cached AllowAll for %s, got deny", toolName)
		}
	}

	g.mu.Lock()
	mapSizeAfter := len(g.sessionAllowed)
	g.mu.Unlock()

	if mapSizeAfter != uniqueTools {
		t.Errorf("second pass grew map: expected %d, got %d", uniqueTools, mapSizeAfter)
	}

	t.Logf("P3-3 sessionAllowed map size after %d unique AllowAll tools: %d entries (idempotent)", uniqueTools, mapSizeAfter)
}

// TestGate_SessionAllowed_SameToolDeduped verifies that repeated AllowAll
// decisions for the same tool do not create duplicate entries.
func TestGate_SessionAllowed_SameToolDeduped(t *testing.T) {
	t.Parallel()

	calls := 0
	g := NewGate(false, func(req PermissionRequest) Decision {
		calls++
		return AllowAll
	})

	req := PermissionRequest{
		ToolName: "bash",
		Level:    tools.PermExec,
	}

	// First call: prompts and adds to map.
	if !g.Check(req) {
		t.Fatal("expected allow")
	}
	// Subsequent calls: hits the allow-list, never prompts again.
	for i := 0; i < 100; i++ {
		if !g.Check(req) {
			t.Fatalf("expected cached allow on call %d", i+2)
		}
	}

	if calls != 1 {
		t.Errorf("expected promptFunc called exactly once, got %d", calls)
	}

	g.mu.Lock()
	mapSize := len(g.sessionAllowed)
	g.mu.Unlock()

	if mapSize != 1 {
		t.Errorf("expected map size 1, got %d", mapSize)
	}
	t.Logf("P3-3 same tool 101 calls: promptFunc=%d, mapSize=%d", calls, mapSize)
}
