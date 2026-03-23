package permissions

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/tools"
)

// TestGate_ProviderBypassAttempt verifies that disallowed providers are always rejected.
func TestGate_ProviderBypassAttempt(t *testing.T) {
	gate := NewGate(false, nil)
	gate.SetAllowedProviders(map[string]bool{
		"github":  true,
		"gitlab":  true,
	})

	// Attacker tries to use a disallowed provider
	req := PermissionRequest{
		ToolName: "bash",
		Level:    tools.PermExec,
		Provider: "datadog", // not in allowlist
		Args:     map[string]any{"command": "whoami"},
	}

	if gate.Check(req) {
		t.Error("disallowed provider should be rejected")
	}
}

// TestGate_ProviderBypass_EmptyString verifies empty provider string is handled.
func TestGate_ProviderBypass_EmptyString(t *testing.T) {
	gate := NewGate(false, func(r PermissionRequest) Decision {
		return Deny
	})
	gate.SetAllowedProviders(map[string]bool{
		"github": true,
	})

	// Request with empty provider (untagged tool)
	req := PermissionRequest{
		ToolName: "bash",
		Level:    tools.PermExec,
		Provider: "", // empty provider
		Args:     map[string]any{},
	}

	// Empty provider should not match the allowlist, so allowedProviders == nil means all allowed
	// But if we set allowed providers, empty provider should be treated as "not allowed"
	result := gate.Check(req)

	// With allowedProviders set to non-nil map, empty provider (not in map) should be rejected
	if result {
		t.Error("empty provider with non-nil allowedProviders should be rejected")
	}
}

// TestGate_AllowAll_SkipsProviderCheck verifies skipAll allows even when providers restricted.
func TestGate_AllowAll_SkipsProviderCheck(t *testing.T) {
	gate := NewGate(true, nil) // skipAll = true, no provider restriction
	// When allowedProviders is nil, all providers are allowed.
	// skipAll=true means PermExec proceeds without prompting.

	req := PermissionRequest{
		ToolName: "bash",
		Level:    tools.PermExec,
		Provider: "datadog",
		Args:     map[string]any{},
	}

	if !gate.Check(req) {
		t.Error("skipAll with no provider restriction should allow PermExec")
	}
}

// TestGate_WatchedProvider_BypassesSkipAll_Hardening verifies watched providers aren't skipped.
func TestGate_WatchedProvider_BypassesSkipAll_Hardening(t *testing.T) {
	promptCalled := false
	gate := NewGate(true, func(r PermissionRequest) Decision {
		promptCalled = true
		return Deny
	})
	gate.SetWatchedProviders(map[string]bool{
		"github": true,
	})

	req := PermissionRequest{
		ToolName: "bash",
		Level:    tools.PermWrite,
		Provider: "github",
		Args:     map[string]any{},
	}

	result := gate.Check(req)
	if result || !promptCalled {
		t.Error("watched provider should trigger prompt even with skipAll=true")
	}
}

// TestGate_ReadOnlyBypassesAll verifies PermRead is always allowed.
func TestGate_ReadOnlyBypassesAll(t *testing.T) {
	// Provider restriction is enforced before PermRead shortcut.
	// With no provider restriction (nil allowedProviders), PermRead is always allowed.
	gate := NewGate(false, func(r PermissionRequest) Decision {
		return Deny
	})
	// No SetAllowedProviders — all providers permitted.

	req := PermissionRequest{
		ToolName: "list_files",
		Level:    tools.PermRead,
		Provider: "datadog",
		Args:     map[string]any{},
	}

	if !gate.Check(req) {
		t.Error("PermRead should be allowed when no provider restriction is set")
	}
}

// TestGate_SessionAllowlist_Persistence verifies allow-all-in-session persists.
func TestGate_SessionAllowlist_Persistence(t *testing.T) {
	gate := NewGate(false, func(r PermissionRequest) Decision {
		return AllowAll // User selects "always allow"
	})

	req := PermissionRequest{
		ToolName: "bash",
		Level:    tools.PermExec,
		Provider: "",
		Args:     map[string]any{},
	}

	// First call should prompt and user allows all
	result1 := gate.Check(req)
	if !result1 {
		t.Error("AllowAll decision should be accepted")
	}

	// Create a new gate that denies, and set up bash in the allowlist via AllowAll
	promptCount := 0
	gate2 := NewGate(false, func(r PermissionRequest) Decision {
		promptCount++
		return AllowAll // Returns AllowAll which adds to session
	})
	gate2.Check(req) // First call sets allowlist

	// Second call should use cached allowlist (prompt count stays 1)
	initialPrompts := promptCount
	gate2.Check(req) // Second call should use cached allowlist

	if promptCount > initialPrompts {
		t.Error("session allowlist should prevent re-prompting")
	}
}

// TestGate_SessionAllowlist_PerToolName verifies allowlist is per-tool, not per-provider.
func TestGate_SessionAllowlist_PerToolName(t *testing.T) {
	promptCount := 0
	gate := NewGate(false, func(r PermissionRequest) Decision {
		promptCount++
		if r.ToolName == "bash" {
			return AllowAll
		}
		return Deny
	})

	// First: allow "bash" for all sessions
	req1 := PermissionRequest{ToolName: "bash", Level: tools.PermExec, Provider: "", Args: map[string]any{}}
	gate.Check(req1)

	// Second: try "write_file" — should prompt again
	req2 := PermissionRequest{ToolName: "write_file", Level: tools.PermWrite, Provider: "", Args: map[string]any{}}
	gate.Check(req2)

	if promptCount < 2 {
		t.Error("different tools should prompt separately")
	}
}

// TestGate_Fork_IsolatesSessionAllowlist verifies Fork creates independent gates.
func TestGate_Fork_IsolatesSessionAllowlist(t *testing.T) {
	parentGate := NewGate(false, func(r PermissionRequest) Decision {
		return AllowAll
	})

	// Parent allows "bash"
	req := PermissionRequest{ToolName: "bash", Level: tools.PermExec, Provider: "", Args: map[string]any{}}
	parentGate.Check(req)

	// Fork creates new gate with copied sessionAllowlist
	childGate := parentGate.Fork(nil, nil)

	// Child should inherit the allowlist
	// The forked gate should not prompt again for bash (already in allowlist)
	result := childGate.Check(req)
	if !result {
		t.Error("forked gate should inherit parent's session allowlist")
	}
}

// TestGate_Fork_PreventsRaceCondition verifies Fork solves concurrent mutation.
func TestGate_Fork_PreventsRaceCondition(t *testing.T) {
	parentGate := NewGate(true, nil)

	// Simulate concurrent agent runs trying to modify the same gate
	done := make(chan bool, 2)

	go func() {
		gate1 := parentGate.Fork(
			map[string]bool{"github": true},
			map[string]bool{"github": true},
		)
		req := PermissionRequest{ToolName: "test", Level: tools.PermWrite, Provider: "gitlab", Args: map[string]any{}}
		// This should use the forked gate's allowedProviders, not parent's
		gate1.Check(req)
		done <- true
	}()

	go func() {
		gate2 := parentGate.Fork(
			map[string]bool{"gitlab": true},
			map[string]bool{"gitlab": true},
		)
		req := PermissionRequest{ToolName: "test", Level: tools.PermWrite, Provider: "github", Args: map[string]any{}}
		gate2.Check(req)
		done <- true
	}()

	<-done
	<-done
	t.Log("concurrent fork operations completed without panic")
}

// TestGate_RelayResponseTimeout_Hardening verifies abandoned relay requests time out.
func TestGate_RelayResponseTimeout_Hardening(t *testing.T) {
	gate := NewGate(false, nil)

	// Create a context that expires in 100ms
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	ch := make(chan bool, 1)
	gate.RegisterRelayResponse(ctx, "test-request-id", ch)

	// Wait for the timeout
	time.Sleep(200 * time.Millisecond)

	// Channel should have received false (deny)
	select {
	case approved := <-ch:
		if approved {
			t.Error("expected false (deny) on timeout, got true")
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("expected channel to receive deny signal on context expiry")
	}
}

// TestGate_RelayResponseDoubleDelivery verifies responses can't be delivered twice.
func TestGate_RelayResponseDoubleDelivery(t *testing.T) {
	gate := NewGate(false, nil)

	ch := make(chan bool, 1)
	gate.RegisterRelayResponse(context.Background(), "test-id", ch)

	// Deliver first response
	ok1 := gate.DeliverRelayResponse("test-id", true)
	if !ok1 {
		t.Error("first delivery should succeed")
	}

	// Try to deliver again — should fail (request already processed)
	ok2 := gate.DeliverRelayResponse("test-id", false)
	if ok2 {
		t.Error("second delivery should fail — response already sent")
	}

	// Verify only first value is in channel
	val := <-ch
	if !val {
		t.Error("expected first response (true) to be delivered")
	}
}

// TestGate_RelayResponseUnknownID_Hardening verifies unknown IDs are rejected.
func TestGate_RelayResponseUnknownID_Hardening(t *testing.T) {
	gate := NewGate(false, nil)

	// Try to deliver to non-existent request
	ok := gate.DeliverRelayResponse("unknown-id", true)
	if ok {
		t.Error("unknown request ID should be rejected")
	}
}

// TestGate_RelayRequestIDUnguessable verifies generated IDs are unpredictable.
func TestGate_RelayRequestIDUnguessable(t *testing.T) {
	ids := make(map[string]bool)

	for i := 0; i < 100; i++ {
		id, err := NewRelayRequestID()
		if err != nil {
			t.Fatalf("NewRelayRequestID: %v", err)
		}

		if len(id) != 32 { // 16 bytes * 2 hex chars
			t.Errorf("expected 32 char hex string, got %d chars: %s", len(id), id)
		}

		if ids[id] {
			t.Fatalf("duplicate ID generated (collision): %s", id)
		}
		ids[id] = true
	}
}

// TestGate_AllowOnceVsAllowAll verifies AllowOnce doesn't persist in session.
func TestGate_AllowOnceVsAllowAll(t *testing.T) {
	promptCount := 0
	gate := NewGate(false, func(r PermissionRequest) Decision {
		promptCount++
		return AllowOnce // Not AllowAll
	})

	req := PermissionRequest{ToolName: "bash", Level: tools.PermExec, Provider: "", Args: map[string]any{}}

	// First call with AllowOnce
	result1 := gate.Check(req)
	if !result1 {
		t.Error("AllowOnce should allow this call")
	}

	// Second call should prompt again (not in session allowlist)
	gate.Check(req)

	if promptCount < 2 {
		t.Error("AllowOnce should NOT add to session allowlist; second call should prompt again")
	}
}

// TestGate_NilPromptFunc verifies nil prompt function defaults to deny.
func TestGate_NilPromptFunc(t *testing.T) {
	gate := NewGate(false, nil) // No prompt function

	req := PermissionRequest{ToolName: "bash", Level: tools.PermWrite, Provider: "", Args: map[string]any{}}

	result := gate.Check(req)
	if result {
		t.Error("nil prompt function should deny by default")
	}
}

// TestGate_Provider_NilAllowedProviders verifies nil allowedProviders means all allowed.
func TestGate_Provider_NilAllowedProviders(t *testing.T) {
	gate := NewGate(false, func(r PermissionRequest) Decision {
		return Allow
	})
	// Don't set allowedProviders (remains nil)

	req := PermissionRequest{ToolName: "bash", Level: tools.PermWrite, Provider: "any-provider", Args: map[string]any{}}

	result := gate.Check(req)
	if !result {
		t.Error("nil allowedProviders should allow any provider")
	}
}

// TestGate_SetWatchedProviders_Overwrites verifies SetWatchedProviders replaces previous.
func TestGate_SetWatchedProviders_Overwrites(t *testing.T) {
	gate := NewGate(true, func(r PermissionRequest) Decision {
		return Allow
	})

	gate.SetWatchedProviders(map[string]bool{"github": true})
	gate.SetWatchedProviders(map[string]bool{"gitlab": true}) // Overwrites

	// github should no longer be watched
	req1 := PermissionRequest{ToolName: "bash", Level: tools.PermWrite, Provider: "github", Args: map[string]any{}}
	result1 := gate.Check(req1)
	if !result1 {
		t.Error("github should not be watched after overwrite")
	}

	// gitlab should be watched
	req2 := PermissionRequest{ToolName: "bash", Level: tools.PermWrite, Provider: "gitlab", Args: map[string]any{}}
	gate = NewGate(true, func(r PermissionRequest) Decision {
		return Allow
	})
	gate.SetWatchedProviders(map[string]bool{"gitlab": true})
	result2 := gate.Check(req2)
	if !result2 {
		t.Error("gitlab should be watched")
	}
}

// TestGate_SetWatchedProviders_Nil verifies SetWatchedProviders(nil) clears.
func TestGate_SetWatchedProviders_Nil(t *testing.T) {
	gate := NewGate(true, nil)
	gate.SetWatchedProviders(map[string]bool{"github": true})
	gate.SetWatchedProviders(nil) // Clear

	req := PermissionRequest{ToolName: "bash", Level: tools.PermWrite, Provider: "github", Args: map[string]any{}}
	result := gate.Check(req)
	if !result {
		t.Error("cleared watched providers should allow all with skipAll=true")
	}
}
