package agent

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/tools"
)

type approvedExecTool struct{}

func (t *approvedExecTool) Name() string                      { return "bash" }
func (t *approvedExecTool) Description() string               { return "" }
func (t *approvedExecTool) Permission() tools.PermissionLevel { return tools.PermExec }
func (t *approvedExecTool) Schema() backend.Tool {
	return backend.Tool{Type: "function", Function: backend.ToolFunction{Name: "bash"}}
}
func (t *approvedExecTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	return tools.ToolResult{}
}

// TestApplyToolbelt_SeedsApprovedToolsIntoForkedGate verifies that an
// agent's persisted ApprovedTools grant is pre-seeded into the per-run
// forked gate, so a previously-approved tool doesn't re-prompt.
func TestApplyToolbelt_SeedsApprovedToolsIntoForkedGate(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&approvedExecTool{})
	reg.TagTools([]string{"bash"}, "builtin")

	promptCalled := false
	parentGate := permissions.NewGate(true, func(permissions.PermissionRequest) permissions.Decision {
		promptCalled = true
		return permissions.Deny
	})
	parentGate.SetExecRequiresPrompt(true)

	ag := &agents.Agent{
		Name:          "codey",
		LocalTools:    []string{"bash"},
		ApprovedTools: []string{"bash"},
	}

	_, agentGate := applyToolbelt(ag, reg, parentGate, nil)
	if agentGate == nil {
		t.Fatal("expected forked gate")
	}

	if !agentGate.Check(permissions.PermissionRequest{
		ToolName: "bash",
		Level:    tools.PermExec,
		Provider: reg.ProviderFor("bash"),
	}) {
		t.Error("expected bash to be allowed via seeded ApprovedTools")
	}
	if promptCalled {
		t.Error("promptFunc should not be called for a pre-approved tool")
	}
}

// TestApplyToolbelt_NoApprovedToolsStillPrompts confirms the baseline: without
// a persisted grant, exec tools still fall through to the prompt.
func TestApplyToolbelt_NoApprovedToolsStillPrompts(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&approvedExecTool{})
	reg.TagTools([]string{"bash"}, "builtin")

	promptCalled := false
	parentGate := permissions.NewGate(true, func(permissions.PermissionRequest) permissions.Decision {
		promptCalled = true
		return permissions.Deny
	})
	parentGate.SetExecRequiresPrompt(true)

	ag := &agents.Agent{Name: "codey", LocalTools: []string{"bash"}}

	_, agentGate := applyToolbelt(ag, reg, parentGate, nil)
	if agentGate.Check(permissions.PermissionRequest{
		ToolName: "bash",
		Level:    tools.PermExec,
		Provider: reg.ProviderFor("bash"),
	}) {
		t.Error("expected bash to be denied (promptFunc returns Deny)")
	}
	if !promptCalled {
		t.Error("expected promptFunc to be called without a persisted grant")
	}
}

// TestApplyToolbelt_AlwaysAllowDoesNotLeakToOtherAgents is the core scoping
// guarantee of per-agent approval: session-allow inside the gate is keyed by
// TOOL NAME, not by agent, so if an "always allow" decision landed on the
// shared parent gate it would silently grant bash to every other agent that
// forks from it afterwards. It must land on the per-run fork instead.
//
// Sequence: agent A is prompted for bash and the human answers "always" →
// A's own gate stops asking, and a fresh fork for agent B still asks.
func TestApplyToolbelt_AlwaysAllowDoesNotLeakToOtherAgents(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&approvedExecTool{})
	reg.TagTools([]string{"bash"}, "builtin")

	var prompts []string
	parentGate := permissions.NewGate(true, func(req permissions.PermissionRequest) permissions.Decision {
		prompts = append(prompts, req.AgentName)
		return permissions.AllowAll // the human clicks "Always allow for <Agent>"
	})
	defer parentGate.Close()
	parentGate.SetExecRequiresPrompt(true)

	bashReq := func(agentName string) permissions.PermissionRequest {
		return permissions.PermissionRequest{
			ToolName:  "bash",
			Level:     tools.PermExec,
			Provider:  reg.ProviderFor("bash"),
			AgentName: agentName,
			SessionID: "sess-shared",
		}
	}

	agentA := &agents.Agent{Name: "alice", LocalTools: []string{"bash"}}
	_, gateA := applyToolbelt(agentA, reg, parentGate, nil)
	if gateA == nil {
		t.Fatal("expected forked gate for alice")
	}
	defer gateA.Close()

	if !gateA.Check(bashReq("alice")) {
		t.Fatal("alice's bash call should be allowed after AllowAll")
	}
	// Second call for the same agent is covered by the in-run grant.
	if !gateA.Check(bashReq("alice")) {
		t.Fatal("alice's second bash call should be allowed by the session grant")
	}
	if len(prompts) != 1 {
		t.Fatalf("expected exactly 1 prompt for alice, got %d (%v)", len(prompts), prompts)
	}

	// Agent B forks from the same parent gate in the same session. It has no
	// persisted grant, so it must be prompted on its own.
	agentB := &agents.Agent{Name: "bob", LocalTools: []string{"bash"}}
	_, gateB := applyToolbelt(agentB, reg, parentGate, nil)
	if gateB == nil {
		t.Fatal("expected forked gate for bob")
	}
	defer gateB.Close()

	if !gateB.Check(bashReq("bob")) {
		t.Fatal("bob's bash call should be allowed (promptFunc returns AllowAll)")
	}
	if len(prompts) != 2 || prompts[1] != "bob" {
		t.Fatalf("bob must be prompted separately; prompts=%v", prompts)
	}

	// And the shared parent gate itself must still be clean — a leak here
	// would grant bash to every future fork.
	if parentGate.Check(permissions.PermissionRequest{
		ToolName: "bash", Level: tools.PermExec, Provider: reg.ProviderFor("bash"),
	}) != true {
		// promptFunc returns AllowAll, so this call is allowed — but it must
		// have gone through the prompt, not through a leaked session grant.
		t.Fatal("unexpected deny from parent gate")
	}
	if len(prompts) != 3 {
		t.Errorf("parent gate should have prompted rather than reusing a leaked grant; prompts=%v", prompts)
	}
}

// TestApplyToolbelt_ApprovedToolsAreAgentScoped verifies a persisted grant on
// one agent does not seed another agent's forked gate.
func TestApplyToolbelt_ApprovedToolsAreAgentScoped(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&approvedExecTool{})
	reg.TagTools([]string{"bash"}, "builtin")

	prompted := 0
	parentGate := permissions.NewGate(true, func(permissions.PermissionRequest) permissions.Decision {
		prompted++
		return permissions.Deny
	})
	defer parentGate.Close()
	parentGate.SetExecRequiresPrompt(true)

	granted := &agents.Agent{Name: "alice", LocalTools: []string{"bash"}, ApprovedTools: []string{"bash"}}
	_, gateGranted := applyToolbelt(granted, reg, parentGate, nil)
	defer gateGranted.Close()

	ungranted := &agents.Agent{Name: "bob", LocalTools: []string{"bash"}}
	_, gateUngranted := applyToolbelt(ungranted, reg, parentGate, nil)
	defer gateUngranted.Close()

	req := permissions.PermissionRequest{ToolName: "bash", Level: tools.PermExec, Provider: reg.ProviderFor("bash")}
	if !gateGranted.Check(req) {
		t.Error("alice has a persisted grant and should not be prompted")
	}
	if gateUngranted.Check(req) {
		t.Error("bob has no grant and must be denied by the prompt")
	}
	if prompted != 1 {
		t.Errorf("expected exactly one prompt (bob's), got %d", prompted)
	}
}
