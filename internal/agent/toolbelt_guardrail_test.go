package agent

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/tools"
)

// newAWSSlackReg registers a fake aws_* write tool and a slack write tool.
// Used to prove the empty-toolbelt skipAll hole is closed at execution time:
// a hallucinated aws_* name in the global registry must not run unless the
// agent's toolbelt grants AWS (or "*").
func newAWSSlackReg() (*tools.Registry, *writeMockTool, *writeMockTool) {
	awsTool := &writeMockTool{
		name:   "aws_ec2_terminate_instance",
		result: tools.ToolResult{Output: "terminated"},
	}
	slackTool := &writeMockTool{
		name:   "slack_post",
		result: tools.ToolResult{Output: "posted"},
	}
	reg := tools.NewRegistry()
	reg.Register(awsTool)
	reg.Register(slackTool)
	reg.TagTools([]string{"aws_ec2_terminate_instance"}, "aws")
	reg.TagTools([]string{"slack_post"}, "slack")
	return reg, awsTool, slackTool
}

func runHallucinatedCall(t *testing.T, ag *agents.Agent, reg *tools.Registry, toolName string) {
	t.Helper()
	// Server oneshot/WS uses NewGate(true, nil): auto-approve, not allow-all.
	gate := permissions.NewGate(true, nil)
	schemas, agentGate := applyToolbelt(ag, reg, gate, nil)

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse(toolName, "call-1"),
			stopResponse("done"),
		},
	}
	_, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns:    5,
		Backend:     mb,
		Tools:       reg,
		ToolSchemas: schemas,
		Gate:        agentGate,
		Messages:    []backend.Message{{Role: "user", Content: "do it"}},
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
}

func callCount(t *writeMockTool) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.callCount
}

// TestEmptyToolbelt_CannotExecuteHallucinatedAWS is the skipAll hole:
// empty toolbelt + NewGate(true, nil) used to mean AllowedProviders=nil
// (every provider). A model that names aws_ec2_terminate_instance must
// still be denied.
func TestEmptyToolbelt_CannotExecuteHallucinatedAWS(t *testing.T) {
	reg, awsTool, _ := newAWSSlackReg()
	ag := &agents.Agent{Name: "locked"} // empty toolbelt, no local tools

	runHallucinatedCall(t, ag, reg, "aws_ec2_terminate_instance")

	if n := callCount(awsTool); n != 0 {
		t.Fatalf("empty toolbelt executed aws_* %d time(s); expected 0", n)
	}
}

// TestWildcardToolbelt_CanExecuteAWS verifies provider/connection_id "*"
// remains explicit allow-all (Astra on MJ Mac).
func TestWildcardToolbelt_CanExecuteAWS(t *testing.T) {
	reg, awsTool, _ := newAWSSlackReg()
	ag := &agents.Agent{
		Name: "astra",
		Toolbelt: []agents.ToolbeltEntry{
			{ConnectionID: "*", Provider: "*"},
		},
	}

	runHallucinatedCall(t, ag, reg, "aws_ec2_terminate_instance")

	if n := callCount(awsTool); n != 1 {
		t.Fatalf("\"*\" toolbelt should execute aws_*, got %d calls", n)
	}
}

// TestAWSOnlyToolbelt_CanExecuteOnlyAWS verifies a named AWS grant cannot
// reach other providers.
func TestAWSOnlyToolbelt_CanExecuteOnlyAWS(t *testing.T) {
	reg, awsTool, slackTool := newAWSSlackReg()
	ag := agentWithToolbelt([]string{"aws"}, false)

	runHallucinatedCall(t, ag, reg, "aws_ec2_terminate_instance")
	if n := callCount(awsTool); n != 1 {
		t.Fatalf("aws-only toolbelt should execute aws_*, got %d calls", n)
	}

	runHallucinatedCall(t, ag, reg, "slack_post")
	if n := callCount(slackTool); n != 0 {
		t.Fatalf("aws-only toolbelt executed slack_* %d time(s); expected 0", n)
	}
}

func TestApplyToolbelt_EmptyGateDeniesAWSEvenWithSkipAll(t *testing.T) {
	reg, _, _ := newAWSSlackReg()
	ag := &agents.Agent{Name: "locked"}
	gate := permissions.NewGate(true, nil)
	_, agentGate := applyToolbelt(ag, reg, gate, nil)

	if agentGate.Check(permissions.PermissionRequest{
		ToolName: "aws_ec2_terminate_instance",
		Level:    tools.PermWrite,
		Provider: "aws",
	}) {
		t.Fatal("empty toolbelt forked gate must deny aws; skipAll is not allow-all-providers")
	}
}

func TestApplyToolbelt_WildcardGateAllowsAWS(t *testing.T) {
	reg, _, _ := newAWSSlackReg()
	ag := &agents.Agent{
		Name:     "astra",
		Toolbelt: []agents.ToolbeltEntry{{ConnectionID: "*", Provider: "*"}},
	}
	gate := permissions.NewGate(true, nil)
	_, agentGate := applyToolbelt(ag, reg, gate, nil)

	if !agentGate.Check(permissions.PermissionRequest{
		ToolName: "aws_ec2_terminate_instance",
		Level:    tools.PermWrite,
		Provider: "aws",
	}) {
		t.Fatal("\"*\" toolbelt forked gate must allow aws")
	}
}
