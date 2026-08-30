package agent

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/tools"
)

// TestApplyToolbelt_ApprovedTool_HeadlessNoPrompt is V4(a): an agent with
// browser_navigate in ApprovedTools (a prior "Always allow" grant) must
// execute headless — no promptFunc call — even though the provider is
// base-watched and no WS client is attached (promptFunc/promptFuncCtx both
// nil, the fail-closed default). SeedSessionAllowed must be checked before
// the gate ever reaches "no prompt function — deny by default".
func TestApplyToolbelt_ApprovedTool_HeadlessNoPrompt(t *testing.T) {
	reg := buildLocalTestRegistry()

	// No promptFunc / promptFuncCtx registered at all — simulates an
	// unattended run with no WS client attached.
	gate := permissions.NewGate(true, nil)
	t.Cleanup(gate.Close)
	gate.SetBaseWatchedProviders(map[string]bool{"playwright": true})

	ag := &agents.Agent{
		Name:          "unattended",
		Toolbelt:      []agents.ToolbeltEntry{{Provider: "slack"}},
		ApprovedTools: []string{"browser_navigate"},
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
	if !result.Allowed {
		t.Errorf("expected a previously-approved tool to run headless without a prompt, got denied: %+v", result)
	}
}

// TestApplyToolbelt_ApprovedTool_HeadlessNoPrompt_WithoutApproval is the
// control: the same setup WITHOUT the ApprovedTools grant must fail closed
// (deny — no WS client to prompt), proving the previous test's pass comes
// from SeedSessionAllowed and not from some other bypass.
func TestApplyToolbelt_ApprovedTool_HeadlessNoPrompt_WithoutApproval(t *testing.T) {
	reg := buildLocalTestRegistry()

	gate := permissions.NewGate(true, nil)
	t.Cleanup(gate.Close)
	gate.SetBaseWatchedProviders(map[string]bool{"playwright": true})

	ag := &agents.Agent{
		Name:     "unattended",
		Toolbelt: []agents.ToolbeltEntry{{Provider: "slack"}},
		// No ApprovedTools grant this time.
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
	if result.Allowed {
		t.Error("expected fail-closed deny with no WS client and no prior approval")
	}
	if result.ReasonCode != permissions.ReasonPromptUnavailable {
		t.Errorf("expected ReasonPromptUnavailable, got %q", result.ReasonCode)
	}
}
