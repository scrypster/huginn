package permissions

import (
	"testing"

	"github.com/scrypster/huginn/internal/tools"
)

// TestCheck_SingleLockAcquisition verifies that Check works correctly with
// the single-lock-acquisition pattern for all code paths.
func TestCheck_SingleLockAcquisition(t *testing.T) {
	t.Run("provider blocked by toolbelt", func(t *testing.T) {
		g := NewGate(false, nil)
		g.SetAllowedProviders(map[string]bool{"github": true})

		ok := g.Check(PermissionRequest{
			ToolName: "jira_create",
			Level:    tools.PermWrite,
			Provider: "jira",
		})
		if ok {
			t.Error("expected deny for provider not in allowed set")
		}
	})

	t.Run("provider allowed by toolbelt", func(t *testing.T) {
		g := NewGate(true, nil)
		g.SetAllowedProviders(map[string]bool{"github": true})

		ok := g.Check(PermissionRequest{
			ToolName: "github_push",
			Level:    tools.PermWrite,
			Provider: "github",
		})
		if !ok {
			t.Error("expected allow for skipAll + allowed provider")
		}
	})

	t.Run("skipAll bypasses for non-watched", func(t *testing.T) {
		g := NewGate(true, nil)
		ok := g.Check(PermissionRequest{
			ToolName: "bash",
			Level:    tools.PermExec,
		})
		if !ok {
			t.Error("expected allow for skipAll")
		}
	})

	t.Run("skipAll still prompts for watched provider", func(t *testing.T) {
		prompted := false
		g := NewGate(true, func(req PermissionRequest) Decision {
			prompted = true
			return Allow
		})
		g.SetWatchedProviders(map[string]bool{"slack": true})

		ok := g.Check(PermissionRequest{
			ToolName: "slack_send",
			Level:    tools.PermWrite,
			Provider: "slack",
		})
		if !ok {
			t.Error("expected allow after prompt")
		}
		if !prompted {
			t.Error("expected prompt for watched provider")
		}
	})

	t.Run("session allowed skips prompt", func(t *testing.T) {
		prompted := false
		g := NewGate(false, func(req PermissionRequest) Decision {
			prompted = true
			return Allow
		})
		// Pre-allow via AllowAll
		g.Check(PermissionRequest{
			ToolName: "bash",
			Level:    tools.PermExec,
		})
		// Now it should be session-allowed; the first call returned Allow not AllowAll
		// so it won't be cached. Let's do it with AllowAll.
		g2 := NewGate(false, func(req PermissionRequest) Decision {
			return AllowAll
		})
		g2.Check(PermissionRequest{ToolName: "bash", Level: tools.PermExec})
		// Second call should use session cache.
		prompted = false
		g2.promptFunc = func(req PermissionRequest) Decision {
			prompted = true
			return Allow
		}
		ok := g2.Check(PermissionRequest{ToolName: "bash", Level: tools.PermExec})
		if !ok {
			t.Error("expected allow from session cache")
		}
		if prompted {
			t.Error("expected no prompt for session-allowed tool")
		}
	})
}

// BenchmarkCheck_SingleLock benchmarks the Check path to verify single-lock
// performance characteristics.
func BenchmarkCheck_SingleLock(b *testing.B) {
	g := NewGate(true, nil)
	g.SetAllowedProviders(map[string]bool{"github": true, "slack": true})
	g.SetWatchedProviders(map[string]bool{"slack": true})

	req := PermissionRequest{
		ToolName: "github_read",
		Level:    tools.PermRead,
		Provider: "github",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.Check(req)
	}
}
