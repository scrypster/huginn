package permissions

import (
	"testing"

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

func TestBaseWatchedProviders_PromptsUnderSkipAll(t *testing.T) {
	called := false
	g := NewGate(true, func(req PermissionRequest) Decision {
		called = true
		if req.ToolName != "browser_navigate" {
			t.Errorf("expected browser_navigate, got %s", req.ToolName)
		}
		return Deny
	})
	t.Cleanup(g.Close)
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
	g := NewGate(true, func(req PermissionRequest) Decision {
		called = true
		return Deny
	})
	t.Cleanup(g.Close)
	g.SetBaseWatchedProviders(map[string]bool{"playwright": true})

	req := PermissionRequest{ToolName: "github_list_repos", Level: tools.PermWrite, Provider: "github"}
	if !g.Check(req) {
		t.Error("expected github_list_repos to still be auto-approved under skipAll")
	}
	if called {
		t.Error("promptFunc should not be called for a provider not in the base-watched set")
	}
}

// TestBaseWatchedProviders_SurvivesFork verifies that base-watched providers
// are copied forward by Fork even though Fork replaces the per-agent
// watchedProviders wholesale with agents.WatchedProviders(ag.Toolbelt).
// Without this, an agent whose toolbelt entry never set approval_gate: true
// for "playwright" would silently auto-run browser tools once
// applyToolbelt forks the gate.
func TestBaseWatchedProviders_SurvivesFork(t *testing.T) {
	called := false
	g := NewGate(true, func(req PermissionRequest) Decision {
		called = true
		return Deny
	})
	t.Cleanup(g.Close)
	g.SetBaseWatchedProviders(map[string]bool{"playwright": true})

	// Simulate applyToolbelt: the agent's toolbelt grants "playwright" but
	// does NOT set ApprovalGate, so the per-agent watchedProviders passed to
	// Fork is empty.
	child := g.Fork(map[string]bool{}, map[string]bool{"playwright": true})
	t.Cleanup(child.Close)

	req := PermissionRequest{ToolName: "browser_navigate", Level: tools.PermWrite, Provider: "playwright"}
	if child.Check(req) {
		t.Error("expected forked gate to still prompt for a base-watched provider")
	}
	if !called {
		t.Error("expected forked gate's promptFunc to be invoked for a base-watched provider")
	}
}

func TestBaseWatchedProviders_ChildMutationDoesNotAffectParent(t *testing.T) {
	g := NewGate(true, func(PermissionRequest) Decision { return Deny })
	t.Cleanup(g.Close)
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
	g := NewGate(true, func(PermissionRequest) Decision { return Deny })
	t.Cleanup(g.Close)
	g.SetBaseWatchedProviders(map[string]bool{"playwright": true})
	g.SetBaseWatchedProviders(nil)

	called := false
	g.SetPromptFunc(func(PermissionRequest) Decision {
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
