package permissions

import (
	"testing"

	"github.com/scrypster/huginn/internal/tools"
)

// ── Fork isolation ────────────────────────────────────────────────────────────

func TestFork_InheritsSkipAll(t *testing.T) {
	parent := NewGate(true, nil)
	child := parent.Fork(nil, nil)
	if !child.skipAll {
		t.Error("fork should inherit skipAll=true from parent")
	}
}

func TestFork_DoesNotShareSessionAllowed(t *testing.T) {
	promptCalls := 0
	parent := NewGate(false, func(req PermissionRequest) Decision {
		promptCalls++
		return AllowAll
	})

	// Allow a tool in the parent via AllowAll.
	parent.Check(PermissionRequest{ToolName: "write_file", Level: tools.PermWrite})

	// Fork AFTER the AllowAll decision — fork should get a snapshot copy.
	child := parent.Fork(nil, nil)

	// Now reset the parent's sessionAllowed by re-creating the map to verify isolation.
	parent.mu.Lock()
	parent.sessionAllowed = make(map[string]bool)
	parent.mu.Unlock()

	// Child should still have the allow (snapshot).
	child.mu.Lock()
	childAllowed := child.sessionAllowed["write_file"]
	child.mu.Unlock()
	if !childAllowed {
		t.Error("fork should have snapshotted parent sessionAllowed at fork time")
	}
}

func TestFork_WritesToChildDoNotAffectParent(t *testing.T) {
	parent := NewGate(false, func(req PermissionRequest) Decision { return AllowAll })
	child := parent.Fork(nil, nil)

	// Allow a new tool via the child.
	child.Check(PermissionRequest{ToolName: "bash", Level: tools.PermExec})

	parent.mu.Lock()
	parentAllowed := parent.sessionAllowed["bash"]
	parent.mu.Unlock()
	if parentAllowed {
		t.Error("child AllowAll should not propagate back to parent")
	}
}

func TestFork_UsesProvidedWatchedProviders(t *testing.T) {
	parent := NewGate(true, func(req PermissionRequest) Decision { return Deny })
	watched := map[string]bool{"github": true}
	child := parent.Fork(watched, nil)

	// With skipAll=true and provider in watchedProviders, should prompt (return Deny here).
	allowed := child.Check(PermissionRequest{
		ToolName: "github_create_pr",
		Level:    tools.PermWrite,
		Provider: "github",
	})
	if allowed {
		t.Error("expected deny from promptFunc for watched provider")
	}
}

func TestFork_UsesProvidedAllowedProviders(t *testing.T) {
	parent := NewGate(true, nil)
	// Only allow "slack" provider.
	child := parent.Fork(nil, map[string]bool{"slack": true})

	// github is NOT in allowedProviders — should be blocked.
	allowed := child.Check(PermissionRequest{
		ToolName: "github_create_pr",
		Level:    tools.PermRead, // even read is blocked by provider restriction
		Provider: "github",
	})
	if allowed {
		t.Error("expected github to be blocked by allowedProviders restriction")
	}

	// slack IS in allowedProviders — read should be allowed.
	allowed = child.Check(PermissionRequest{
		ToolName: "slack_send",
		Level:    tools.PermRead,
		Provider: "slack",
	})
	if !allowed {
		t.Error("expected slack read to be allowed")
	}
}

// ── FormatRequest ─────────────────────────────────────────────────────────────

func TestFormatRequest_UsesSummaryWhenSet(t *testing.T) {
	req := PermissionRequest{
		ToolName: "bash",
		Summary:  "run tests",
		Level:    tools.PermExec,
	}
	got := FormatRequest(req)
	if got != "run tests" {
		t.Errorf("expected 'run tests', got %q", got)
	}
}

func TestFormatRequest_FallbackFormat(t *testing.T) {
	req := PermissionRequest{
		ToolName: "custom_tool",
		Args:     map[string]any{"key": "value"},
		Level:    tools.PermWrite,
	}
	got := FormatRequest(req)
	if got == "" {
		t.Error("expected non-empty fallback format")
	}
}
