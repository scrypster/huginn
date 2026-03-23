package permissions

import (
	"runtime"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/tools"
)

// TestGate_PromptFunc_Timeout verifies that when promptFunc takes longer than
// promptFuncTimeout, Check returns false (deny) rather than blocking forever.
//
// This test exercises the real 30s timeout; it is skipped in short mode.
func TestGate_PromptFunc_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 30s timeout test in short mode")
	}

	slowPrompt := func(req PermissionRequest) Decision {
		// Block for much longer than the test is willing to wait.
		time.Sleep(10 * time.Minute)
		return Allow
	}
	g := NewGate(false, slowPrompt)

	// Verify the slow gate eventually returns false (timer path).
	done := make(chan bool, 1)
	go func() {
		done <- g.Check(PermissionRequest{ToolName: "bash", Level: tools.PermWrite})
	}()

	select {
	case result := <-done:
		if result {
			t.Error("slow promptFunc should be denied by timeout")
		}
	case <-time.After(promptFuncTimeout + 5*time.Second):
		t.Error("Check did not return within promptFuncTimeout + 5s — goroutine leak?")
	}
}

// TestGate_PromptFunc_FastDeny verifies that a fast-returning Deny promptFunc
// works correctly (no timeout interference).
func TestGate_PromptFunc_FastDeny(t *testing.T) {
	fastPrompt := func(req PermissionRequest) Decision {
		return Deny
	}
	g := NewGate(false, fastPrompt)
	result := g.Check(PermissionRequest{ToolName: "bash", Level: tools.PermWrite})
	if result {
		t.Error("fast deny promptFunc should return false")
	}
}

// TestGate_PromptFunc_TimerStoppedOnFastReturn verifies that when promptFunc
// returns quickly, the timer goroutine does not leak. We check goroutine count
// before and after to detect leaks.
func TestGate_PromptFunc_TimerStoppedOnFastReturn(t *testing.T) {
	const iterations = 50

	fastDeny := func(req PermissionRequest) Decision {
		return Deny
	}
	g := NewGate(false, fastDeny)

	// Allow goroutine count to stabilize.
	runtime.GC()
	time.Sleep(5 * time.Millisecond)
	before := runtime.NumGoroutine()

	for i := 0; i < iterations; i++ {
		g.Check(PermissionRequest{
			ToolName: "write_file",
			Level:    tools.PermWrite,
		})
	}

	// Give any timer goroutines a moment to fire if they leaked.
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	after := runtime.NumGoroutine()

	// We allow a small slack (5 goroutines) for runtime noise.
	if after > before+5 {
		t.Errorf("possible goroutine leak: before=%d after=%d (delta=%d > 5)",
			before, after, after-before)
	}
}

// TestGate_PromptFunc_AllowAllPersistsAfterTimeout verifies that AllowAll
// decisions from a fast promptFunc are correctly stored in sessionAllowed.
func TestGate_PromptFunc_AllowAllPersistsAfterTimeout(t *testing.T) {
	g := NewGate(false, func(req PermissionRequest) Decision {
		return AllowAll
	})

	ok := g.Check(PermissionRequest{ToolName: "bash", Level: tools.PermWrite})
	if !ok {
		t.Fatal("AllowAll should return true")
	}

	g.mu.Lock()
	persisted := g.sessionAllowed["bash"]
	g.mu.Unlock()

	if !persisted {
		t.Error("AllowAll decision should be persisted in sessionAllowed")
	}
}
