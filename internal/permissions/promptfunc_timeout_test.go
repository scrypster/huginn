package permissions

import (
	"fmt"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/tools"
)

// TestGate_PromptFuncTimeout_Denies verifies that a promptFunc which blocks
// longer than promptFuncTimeout causes the gate to deny the request.
func TestGate_PromptFuncTimeout_Denies(t *testing.T) {
	// Override the package-level timeout so this test completes in milliseconds.
	old := promptFuncTimeout
	promptFuncTimeout = 100 * time.Millisecond
	defer func() { promptFuncTimeout = old }()

	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) }) // unblock the goroutine when test ends

	slowPrompt := func(req PermissionRequest) Decision {
		<-blocked // block until test cleanup
		return Allow
	}

	g := NewGate(false, slowPrompt)
	req := PermissionRequest{
		ToolName: "bash",
		Level:    tools.PermWrite,
	}

	start := time.Now()
	got := g.Check(req)
	elapsed := time.Since(start)

	if got {
		t.Error("expected Check to deny (false) after promptFunc timeout, got allow (true)")
	}
	// Should return quickly (within a generous 2s) after the 100ms timeout fires.
	if elapsed > 2*time.Second {
		t.Errorf("Check took too long (%v); timeout mechanism may be broken", elapsed)
	}
}

// TestGate_SessionAllowed_BoundsEnforced verifies that adding more than 1000
// distinct tool approvals never causes len(sessionAllowed) to exceed 1000.
func TestGate_SessionAllowed_BoundsEnforced(t *testing.T) {
	promptFn := func(req PermissionRequest) Decision {
		return AllowAll // always grant session-wide approval
	}

	g := NewGate(false, promptFn)

	for i := 0; i < 1001; i++ {
		req := PermissionRequest{
			ToolName: fmt.Sprintf("tool_%d", i),
			Level:    tools.PermWrite,
		}
		g.Check(req)
	}

	g.mu.Lock()
	n := len(g.sessionAllowed)
	g.mu.Unlock()

	if n > maxSessionAllowed {
		t.Errorf("sessionAllowed has %d entries, exceeds cap of %d", n, maxSessionAllowed)
	}
}
