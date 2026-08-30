package permissions

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/tools"
)

// These tests cover the server-mode approval gap for MCP tools: browser-class
// MCP tools (and any other outward-facing MCP server) register with
// Permission() == tools.PermWrite (see MCPToolAdapter.Permission), which under
// serve mode's skipAll=true gate is auto-approved unless the provider is
// explicitly watched. SetBaseWatchedProviders lets server startup mark MCP
// server names as always-watched, independent of any per-agent toolbelt
// ApprovalGate flag — closing the "Auto-approve all tools in server mode" gap
// for outward-facing tools without touching bash's own PermExec handling.
//
// V10: these tests drive the PRODUCTION shape instead of hand-built
// gate/fork args — NewGate(true, nil) + SetPromptFuncCtx (matching real
// server-mode wiring, not the legacy SetPromptFunc), and a fork built
// exactly the way applyToolbelt (internal/agent/agent_dispatcher.go) builds
// it. internal/permissions cannot import internal/agent directly (agent
// imports permissions — that would cycle), so buildApplyToolbeltFork below
// mirrors applyToolbelt's step 5 construction verbatim. Each test's
// "configuredMCPProviders" argument is the pre-V3 shape when nil (the MCP
// server name is absent from allowedProviders) and the post-V3-fix shape
// when it names the provider.

// buildApplyToolbeltFork mirrors applyToolbelt's step 5 (agent_dispatcher.go)
// exactly: allowed providers = agents.AllowedProviders(toolbelt) plus
// muninndb/builtin plus (V3 policy) any configured MCP server names, forked
// with agents.WatchedProviders(toolbelt) as the per-agent watched set.
func buildApplyToolbeltFork(g *Gate, toolbelt []agents.ToolbeltEntry, configuredMCPProviders map[string]bool) *Gate {
	allowed := agents.AllowedProviders(toolbelt)
	if allowed != nil && !allowed["*"] {
		allowed["muninndb"] = true
		allowed["builtin"] = true
		for name := range configuredMCPProviders {
			allowed[name] = true
		}
	}
	return g.Fork(agents.WatchedProviders(toolbelt), allowed)
}

func TestBaseWatchedProviders_PromptsUnderSkipAll(t *testing.T) {
	called := false
	g := NewGate(true, nil)
	t.Cleanup(g.Close)
	g.SetPromptFuncCtx(func(_ context.Context, req PermissionRequest) Decision {
		called = true
		if req.ToolName != "browser_navigate" {
			t.Errorf("expected browser_navigate, got %s", req.ToolName)
		}
		return Deny
	})
	g.SetBaseWatchedProviders(map[string]bool{"playwright": true})

	req := PermissionRequest{ToolName: "browser_navigate", Level: tools.PermWrite, Provider: "playwright"}
	if g.Check(req) {
		t.Error("expected browser_navigate to be denied (promptFunc returned Deny)")
	}
	if !called {
		t.Error("expected promptFunc to be called for a base-watched provider under skipAll")
	}
}

func TestBaseWatchedProviders_UnwatchedProviderStillAutoApproved(t *testing.T) {
	called := false
	g := NewGate(true, nil)
	t.Cleanup(g.Close)
	g.SetPromptFuncCtx(func(_ context.Context, _ PermissionRequest) Decision {
		called = true
		return Deny
	})
	g.SetBaseWatchedProviders(map[string]bool{"playwright": true})

	req := PermissionRequest{ToolName: "github_list_repos", Level: tools.PermWrite, Provider: "github"}
	if !g.Check(req) {
		t.Error("expected github_list_repos to still be auto-approved under skipAll")
	}
	if called {
		t.Error("promptFunc should not be called for a provider not in the base-watched set")
	}
}

// TestBaseWatchedProviders_SurvivesFork_ProductionShape verifies that
// base-watched providers are copied forward by the exact fork
// applyToolbelt builds, using the V3 policy shape: the agent's toolbelt
// does NOT grant "playwright" and does NOT set ApprovalGate — only the
// configured-MCP-server set (V3) puts "playwright" in allowedProviders.
// Without V3, this fork would deny with provider_not_allowed before ever
// reaching the base-watch prompt below.
func TestBaseWatchedProviders_SurvivesFork_ProductionShape(t *testing.T) {
	called := false
	g := NewGate(true, nil)
	t.Cleanup(g.Close)
	g.SetPromptFuncCtx(func(_ context.Context, req PermissionRequest) Decision {
		called = true
		return Deny
	})
	g.SetBaseWatchedProviders(map[string]bool{"playwright": true})

	toolbelt := []agents.ToolbeltEntry{{Provider: "slack"}} // narrow — no playwright
	child := buildApplyToolbeltFork(g, toolbelt, map[string]bool{"playwright": true})
	t.Cleanup(child.Close)

	result := child.CheckDetailedCtx(context.Background(), PermissionRequest{
		ToolName: "browser_navigate", Level: tools.PermWrite, Provider: "playwright",
	})
	if result.ReasonCode == ReasonProviderNotAllowed {
		t.Fatal("expected the V3 policy to let a configured MCP provider past the provider gate")
	}
	if result.Allowed {
		t.Error("expected forked gate to still prompt (not auto-allow) for a base-watched provider")
	}
	if !called {
		t.Error("expected forked gate's promptFunc to be invoked for a base-watched provider")
	}
}

// TestBaseWatchedProviders_WithoutV3Policy_DeniedBeforePrompt documents the
// pre-V3 shape: when the configured-MCP-server set is NOT plumbed into the
// fork (nil), a narrow toolbelt without "playwright" is denied at the
// provider gate and the base-watch prompt never fires — the bug V3 fixes.
func TestBaseWatchedProviders_WithoutV3Policy_DeniedBeforePrompt(t *testing.T) {
	called := false
	g := NewGate(true, nil)
	t.Cleanup(g.Close)
	g.SetPromptFuncCtx(func(_ context.Context, _ PermissionRequest) Decision {
		called = true
		return Deny
	})
	g.SetBaseWatchedProviders(map[string]bool{"playwright": true})

	toolbelt := []agents.ToolbeltEntry{{Provider: "slack"}}
	child := buildApplyToolbeltFork(g, toolbelt, nil) // pre-V3 shape: no configured MCP providers plumbed
	t.Cleanup(child.Close)

	result := child.CheckDetailedCtx(context.Background(), PermissionRequest{
		ToolName: "browser_navigate", Level: tools.PermWrite, Provider: "playwright",
	})
	if result.Allowed {
		t.Error("expected pre-V3 shape to deny")
	}
	if result.ReasonCode != ReasonProviderNotAllowed {
		t.Errorf("expected ReasonProviderNotAllowed (the bug V3 fixes), got %q", result.ReasonCode)
	}
	if called {
		t.Error("promptFunc must not fire when the provider gate already denied")
	}
}

func TestBaseWatchedProviders_ChildMutationDoesNotAffectParent(t *testing.T) {
	g := NewGate(true, nil)
	t.Cleanup(g.Close)
	g.SetPromptFuncCtx(func(context.Context, PermissionRequest) Decision { return Deny })
	g.SetBaseWatchedProviders(map[string]bool{"playwright": true})

	child := g.Fork(nil, nil)
	t.Cleanup(child.Close)
	child.SetBaseWatchedProviders(map[string]bool{"other": true})

	g.mu.Lock()
	_, stillPlaywright := g.baseWatchedProviders["playwright"]
	g.mu.Unlock()
	if !stillPlaywright {
		t.Error("mutating the child's base-watched providers must not affect the parent")
	}
}

func TestBaseWatchedProviders_NilClears(t *testing.T) {
	g := NewGate(true, nil)
	t.Cleanup(g.Close)
	g.SetPromptFuncCtx(func(context.Context, PermissionRequest) Decision { return Deny })
	g.SetBaseWatchedProviders(map[string]bool{"playwright": true})
	g.SetBaseWatchedProviders(nil)

	called := false
	g.SetPromptFuncCtx(func(_ context.Context, _ PermissionRequest) Decision {
		called = true
		return Deny
	})

	req := PermissionRequest{ToolName: "browser_navigate", Level: tools.PermWrite, Provider: "playwright"}
	if !g.Check(req) {
		t.Error("expected auto-approve after clearing base-watched providers")
	}
	if called {
		t.Error("promptFunc should not be called once base-watched providers is cleared")
	}
}
