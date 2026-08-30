package agent

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/tools"
)

// TestApplyToolbelt_ConfiguredMCPProvider_ReachesPrompt is the V3 fix: a
// narrow-toolbelt agent (Toolbelt has an unrelated provider, not the
// configured MCP server) must still reach the base-watch prompt for a
// configured MCP tool call — not a silent provider_not_allowed deny. Drives
// the actual applyToolbelt-built fork against a production-shape gate
// (NewGate(true, nil) + SetPromptFuncCtx), per V10.
func TestApplyToolbelt_ConfiguredMCPProvider_ReachesPrompt(t *testing.T) {
	reg := buildLocalTestRegistry()

	prompted := false
	gate := permissions.NewGate(true, nil)
	t.Cleanup(gate.Close)
	gate.SetPromptFuncCtx(func(_ context.Context, req permissions.PermissionRequest) permissions.Decision {
		prompted = true
		if req.ToolName != "browser_navigate" {
			t.Errorf("expected prompt for browser_navigate, got %s", req.ToolName)
		}
		return permissions.Deny
	})
	// Base-watch: same set main.go sets up via SetBaseWatchedProviders for
	// configured MCP servers.
	gate.SetBaseWatchedProviders(map[string]bool{"playwright": true})

	// Narrow toolbelt: only "slack", NOT "playwright".
	ag := &agents.Agent{
		Name:     "narrow",
		Toolbelt: []agents.ToolbeltEntry{{Provider: "slack"}},
	}

	_, agentGate := applyToolbelt(ag, reg, gate, map[string]bool{"playwright": true})
	if agentGate == nil {
		t.Fatal("expected a forked gate")
	}
	t.Cleanup(agentGate.Close)

	result := agentGate.CheckDetailedCtx(context.Background(), permissions.PermissionRequest{
		ToolName: "browser_navigate",
		Level:    tools.PermWrite,
		Provider: "playwright",
	})
	if result.ReasonCode == permissions.ReasonProviderNotAllowed {
		t.Error("expected the configured MCP provider to pass the provider gate, got provider_not_allowed")
	}
	if !prompted {
		t.Error("expected the base-watch prompt to fire for the configured MCP provider")
	}
}

// TestApplyToolbelt_UnconfiguredProvider_StillDenied verifies the V3 policy
// does not weaken the default-deny: a provider that is neither in the
// agent's toolbelt nor in the configured-MCP-server set must still be
// rejected with provider_not_allowed, and the prompt must never fire.
func TestApplyToolbelt_UnconfiguredProvider_StillDenied(t *testing.T) {
	reg := buildLocalTestRegistry()

	prompted := false
	gate := permissions.NewGate(true, nil)
	t.Cleanup(gate.Close)
	gate.SetPromptFuncCtx(func(_ context.Context, _ permissions.PermissionRequest) permissions.Decision {
		prompted = true
		return permissions.Deny
	})
	gate.SetBaseWatchedProviders(map[string]bool{"playwright": true})

	ag := &agents.Agent{
		Name:     "narrow",
		Toolbelt: []agents.ToolbeltEntry{{Provider: "slack"}},
	}

	// "some-other-mcp" is neither in the toolbelt nor in the configured
	// MCP server set passed to applyToolbelt.
	_, agentGate := applyToolbelt(ag, reg, gate, map[string]bool{"playwright": true})
	if agentGate == nil {
		t.Fatal("expected a forked gate")
	}
	t.Cleanup(agentGate.Close)

	result := agentGate.CheckDetailedCtx(context.Background(), permissions.PermissionRequest{
		ToolName: "some_tool",
		Level:    tools.PermWrite,
		Provider: "some-other-mcp",
	})
	if result.Allowed {
		t.Error("expected an unconfigured, non-toolbelt provider to be denied")
	}
	if result.ReasonCode != permissions.ReasonProviderNotAllowed {
		t.Errorf("expected ReasonProviderNotAllowed, got %q", result.ReasonCode)
	}
	if prompted {
		t.Error("prompt must not fire for a provider that stays denied")
	}
}

// TestApplyToolbelt_ConfiguredMCPProvider_PermRead_SamePolicy verifies a
// PermRead MCP tool from a configured server gets the same treatment as a
// PermWrite one: it passes the provider gate instead of being denied
// outright (read-only tools are then auto-allowed downstream).
func TestApplyToolbelt_ConfiguredMCPProvider_PermRead_SamePolicy(t *testing.T) {
	reg := buildLocalTestRegistry()

	gate := permissions.NewGate(true, nil)
	t.Cleanup(gate.Close)
	gate.SetBaseWatchedProviders(map[string]bool{"playwright": true})

	ag := &agents.Agent{
		Name:     "narrow",
		Toolbelt: []agents.ToolbeltEntry{{Provider: "slack"}},
	}

	_, agentGate := applyToolbelt(ag, reg, gate, map[string]bool{"playwright": true})
	if agentGate == nil {
		t.Fatal("expected a forked gate")
	}
	t.Cleanup(agentGate.Close)

	result := agentGate.CheckDetailedCtx(context.Background(), permissions.PermissionRequest{
		ToolName: "browser_get_console",
		Level:    tools.PermRead,
		Provider: "playwright",
	})
	if result.ReasonCode == permissions.ReasonProviderNotAllowed {
		t.Error("expected the configured MCP provider to pass the provider gate for a PermRead tool too")
	}
	if !result.Allowed {
		t.Error("expected PermRead tool from a configured MCP provider to be allowed (read-only tools are always allowed once past the provider gate)")
	}
}
