package agent

// agent_isolation_test.go — integration tests for the full enforcement pipeline.
//
// Tests verify that skills isolation and connection (toolbelt) isolation work
// end-to-end using real production functions: applyToolbelt, skillsFragmentFor,
// and newBareOrchestrator.
//
// All helpers used below (stubSkill, newTestSkillsReg, agentRegWith,
// testTool, newTestToolsReg, agentWithToolbelt, newBareOrchestrator) are
// defined in sibling test files (orchestrator_skills_test.go,
// orchestrator_toolbelt_test.go, orchestrator_test.go) and are accessible
// because all files share package agent.

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/tools"
)

// ---------------------------------------------------------------------------
// Connection isolation (4 tests)
// ---------------------------------------------------------------------------

// TestAgentToolbelt_SchemasSentToModel_OnlyDeclaredProviders verifies that
// when an agent toolbelt lists only "github", applyToolbelt returns schemas
// exclusively for the github provider and omits slack tools.
func TestAgentToolbelt_SchemasSentToModel_OnlyDeclaredProviders(t *testing.T) {
	reg := newTestToolsReg()
	ag := agentWithToolbelt([]string{"github"}, false)

	schemas, _ := applyToolbelt(ag, reg, nil)

	if len(schemas) != 1 {
		t.Fatalf("expected 1 schema for github-only toolbelt, got %d", len(schemas))
	}
	if schemas[0].Function.Name != "github_list_prs" {
		t.Errorf("expected github_list_prs, got %q", schemas[0].Function.Name)
	}
	for _, s := range schemas {
		if s.Function.Name == "slack_post" {
			t.Errorf("slack_post must NOT be present when toolbelt is github-only")
		}
	}
}

// TestAgentToolbelt_NoToolbelt_NoSchemasAvailable verifies that an agent
// with no toolbelt entries and no local tools receives zero schemas (default-deny).
func TestAgentToolbelt_NoToolbelt_NoSchemasAvailable(t *testing.T) {
	reg := newTestToolsReg()
	ag := &agents.Agent{Name: "open-agent"} // no toolbelt, no local tools

	schemas, _ := applyToolbelt(ag, reg, nil)

	if len(schemas) != 0 {
		t.Errorf("expected 0 schemas with default-deny when no toolbelt and no local tools set, got %d", len(schemas))
	}
}

// TestAgentToolbelt_ApprovalGate_SetsWatchedProvider verifies that when an
// agent toolbelt entry has ApprovalGate:true, the gate's watched provider list
// includes that provider. We observe this indirectly: in skipAll mode a watched
// provider still triggers the promptFunc, whereas an unwatched one does not.
func TestAgentToolbelt_ApprovalGate_SetsWatchedProvider(t *testing.T) {
	reg := newTestToolsReg()
	ag := agentWithToolbelt([]string{"github"}, true /* ApprovalGate=true */)

	prompted := false
	gate := permissions.NewGate(true /* skipAll */, func(_ permissions.PermissionRequest) permissions.Decision {
		prompted = true
		return permissions.Deny
	})

	_, agentGate := applyToolbelt(ag, reg, gate)

	// A write-level call for the watched "github" provider should still prompt
	// even when skipAll=true.
	agentGate.Check(permissions.PermissionRequest{
		ToolName: "github_list_prs",
		Level:    tools.PermWrite,
		Provider: "github",
	})

	if !prompted {
		t.Error("expected gate to prompt for watched provider github (ApprovalGate=true), but promptFunc was not called")
	}
}

// TestAgentToolbelt_NoApprovalGate_ProviderNotWatched verifies that when
// ApprovalGate:false, the provider is NOT in the watched set: skipAll mode
// allows the call without invoking the promptFunc.
func TestAgentToolbelt_NoApprovalGate_ProviderNotWatched(t *testing.T) {
	reg := newTestToolsReg()
	ag := agentWithToolbelt([]string{"github"}, false /* ApprovalGate=false */)

	prompted := false
	gate := permissions.NewGate(true /* skipAll */, func(_ permissions.PermissionRequest) permissions.Decision {
		prompted = true
		return permissions.Deny
	})

	_, agentGate := applyToolbelt(ag, reg, gate)

	allowed := agentGate.Check(permissions.PermissionRequest{
		ToolName: "github_list_prs",
		Level:    tools.PermWrite,
		Provider: "github",
	})

	if prompted {
		t.Error("expected no prompt when ApprovalGate=false (provider not watched), but promptFunc was called")
	}
	if !allowed {
		t.Error("expected tool to be allowed in skipAll mode when provider is not watched, but it was denied")
	}
}

// ---------------------------------------------------------------------------
// Skills isolation (4 tests)
// ---------------------------------------------------------------------------

// TestAgentSkills_DeclaredSkillsInjectedInPrompt verifies that when an agent
// lists skills:["code"], the fragment contains the code skill content and does
// NOT contain the plan skill content.
func TestAgentSkills_DeclaredSkillsInjectedInPrompt(t *testing.T) {
	o := newBareOrchestrator()
	o.skillsReg = newTestSkillsReg()

	got := o.skillsFragmentFor(agentRegWith([]string{"code"}))

	if !strings.Contains(got, "Code Skill") {
		t.Errorf("expected 'Code Skill' in fragment when skills=[code], got: %q", got)
	}
	if strings.Contains(got, "Plan Skill") {
		t.Errorf("'Plan Skill' must NOT be present when skills=[code], got: %q", got)
	}
}

// TestAgentSkills_EmptyListInjectsNothing verifies that skills:[] results in
// an empty fragment — no skills content is injected into the system prompt.
func TestAgentSkills_EmptyListInjectsNothing(t *testing.T) {
	o := newBareOrchestrator()
	o.skillsReg = newTestSkillsReg()

	got := o.skillsFragmentFor(agentRegWith([]string{}))

	if got != "" {
		t.Errorf("expected empty fragment for skills=[], got: %q", got)
	}
}

// TestAgentSkills_NilListUsesGlobal verifies that skills:nil (not set) falls
// back to the global skills registry, injecting all registered skills.
func TestAgentSkills_NilListUsesGlobal(t *testing.T) {
	o := newBareOrchestrator()
	o.skillsReg = newTestSkillsReg()

	got := o.skillsFragmentFor(agentRegWith(nil))

	if !strings.Contains(got, "Code Skill") {
		t.Errorf("expected 'Code Skill' in global fallback, got: %q", got)
	}
	if !strings.Contains(got, "Plan Skill") {
		t.Errorf("expected 'Plan Skill' in global fallback, got: %q", got)
	}
}

// TestAgentSkills_UnknownSkillWarnsAndSkips verifies that requesting a skill
// name that is not registered results in an empty fragment — the orchestrator
// skips unknown skills without panicking.
func TestAgentSkills_UnknownSkillWarnsAndSkips(t *testing.T) {
	o := newBareOrchestrator()
	o.skillsReg = newTestSkillsReg()

	got := o.skillsFragmentFor(agentRegWith([]string{"nonexistent"}))

	if got != "" {
		t.Errorf("expected empty fragment for unknown skill 'nonexistent', got: %q", got)
	}
}

// ---------------------------------------------------------------------------
// Cross-agent isolation (2 tests)
// ---------------------------------------------------------------------------

// TestCrossAgent_SkillsNotShared verifies that two orchestrators with different
// per-agent skill lists do not bleed skills into each other's fragments.
// Orchestrator A is configured for agent with skills=["code"] only.
// Orchestrator B is configured for agent with skills=["plan"] only.
func TestCrossAgent_SkillsNotShared(t *testing.T) {
	oA := newBareOrchestrator()
	oA.skillsReg = newTestSkillsReg()

	oB := newBareOrchestrator()
	oB.skillsReg = newTestSkillsReg()

	fragA := oA.skillsFragmentFor(agentRegWith([]string{"code"}))
	fragB := oB.skillsFragmentFor(agentRegWith([]string{"plan"}))

	// Agent A must have code, not plan.
	if !strings.Contains(fragA, "Code Skill") {
		t.Errorf("agent A: expected 'Code Skill', got: %q", fragA)
	}
	if strings.Contains(fragA, "Plan Skill") {
		t.Errorf("agent A: 'Plan Skill' leaked into agent A's fragment: %q", fragA)
	}

	// Agent B must have plan, not code.
	if !strings.Contains(fragB, "Plan Skill") {
		t.Errorf("agent B: expected 'Plan Skill', got: %q", fragB)
	}
	if strings.Contains(fragB, "Code Skill") {
		t.Errorf("agent B: 'Code Skill' leaked into agent B's fragment: %q", fragB)
	}
}

// TestCrossAgent_ConnectionsNotShared verifies that two separate applyToolbelt
// calls — one for an agent with only the github provider and one for an agent
// with only the slack provider — do not leak tools across agents.
func TestCrossAgent_ConnectionsNotShared(t *testing.T) {
	reg := newTestToolsReg()

	agA := agentWithToolbelt([]string{"github"}, false)
	agB := agentWithToolbelt([]string{"slack"}, false)

	schemasA, _ := applyToolbelt(agA, reg, nil)
	schemasB, _ := applyToolbelt(agB, reg, nil)

	// Agent A: must only contain github tools.
	if len(schemasA) != 1 {
		t.Errorf("agent A: expected 1 schema (github only), got %d", len(schemasA))
	}
	for _, s := range schemasA {
		if s.Function.Name == "slack_post" {
			t.Errorf("agent A: slack_post must not appear in a github-only toolbelt")
		}
	}

	// Agent B: must only contain slack tools.
	if len(schemasB) != 1 {
		t.Errorf("agent B: expected 1 schema (slack only), got %d", len(schemasB))
	}
	for _, s := range schemasB {
		if s.Function.Name == "github_list_prs" {
			t.Errorf("agent B: github_list_prs must not appear in a slack-only toolbelt")
		}
	}
}

// ---------------------------------------------------------------------------
// End-to-end pipeline tests (2 tests)
// ---------------------------------------------------------------------------

// TestAgentToolbelt_ChatWithAgent_SchemasFiltered verifies that, when RunLoop
// is configured with schemas produced by applyToolbelt for a github-only agent,
// the tool schemas sent to the LLM backend never include slack_post.
//
// This is an end-to-end test of the schema-filtering pipeline:
//
//	applyToolbelt → ToolSchemas in RunLoopConfig → ChatRequest.Tools sent to backend
//
// The mockBackend (defined in loop_test.go) captures every ChatRequest, so we
// can assert on the exact schemas the model would have seen.
func TestAgentToolbelt_ChatWithAgent_SchemasFiltered(t *testing.T) {
	// Build a registry with github and slack tools (both registered + tagged).
	reg := newTestToolsReg()

	// Build a github-only agent and compute the filtered schema list.
	ag := agentWithToolbelt([]string{"github"}, false)
	filteredSchemas, _ := applyToolbelt(ag, reg, nil)

	// Sanity: applyToolbelt must have returned exactly the github tool.
	if len(filteredSchemas) != 1 {
		t.Fatalf("pre-condition failed: expected 1 schema from applyToolbelt, got %d", len(filteredSchemas))
	}

	// Wire up a mockBackend that returns a single stop response so the loop
	// terminates after one turn without needing real tool execution.
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			stopResponse("done"),
		},
	}

	// Run the loop with the filtered schemas — this is exactly what
	// ChatWithAgent does after calling applyToolbelt.
	_, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns:    5,
		Backend:     mb,
		Tools:       reg,
		ToolSchemas: filteredSchemas,
		Messages:    []backend.Message{{Role: "user", Content: "list prs"}},
	})
	if err != nil {
		t.Fatalf("RunLoop returned unexpected error: %v", err)
	}

	// The backend must have been called at least once.
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) == 0 {
		t.Fatal("mockBackend received no requests — loop did not call backend")
	}

	// Inspect every ChatRequest sent to the backend and assert slack_post was
	// never included in the tool schemas.
	for i, req := range mb.lastRequests {
		for _, schema := range req.Tools {
			if schema.Function.Name == "slack_post" {
				t.Errorf("turn %d: slack_post was sent to the LLM backend but must be absent for a github-only agent", i+1)
			}
		}
	}

	// Additionally verify the github tool IS present in the first request.
	firstReqTools := mb.lastRequests[0].Tools
	found := false
	for _, s := range firstReqTools {
		if s.Function.Name == "github_list_prs" {
			found = true
			break
		}
	}
	if !found {
		t.Error("github_list_prs was missing from the schemas sent to the LLM backend")
	}
}

// TestAgentToolbelt_RuntimeRejection_UnauthorizedProvider verifies the runtime
// enforcement layer in executeSingle: even if the LLM returns a tool_call for
// a provider that is not in the agent's toolbelt, the gate blocks execution and
// returns "error: permission denied" without ever calling the tool's Execute().
//
// Enforcement model note:
// The primary enforcement boundary is schema-level (applyToolbelt filters what
// the LLM sees). This test exercises the secondary, defence-in-depth boundary:
// the permissions Gate checked inside executeSingle at call time. The gate
// blocks the call when:
//   - cfg.Gate is non-nil, AND
//   - gate.Check() returns false for the tool call
//
// We configure the gate with skipAll=false and no promptFunc, which causes
// gate.Check() to deny any PermWrite call (read-only tools are always allowed).
// This models the scenario where a toolbelt-restricted agent's gate is
// configured to deny providers not in the toolbelt.
//
// Note: mockTool (loop_test.go) returns PermRead, which the gate always allows.
// writeMockTool (defined below this test) returns PermWrite so the gate's
// write-level check fires.
func TestAgentToolbelt_RuntimeRejection_UnauthorizedProvider(t *testing.T) {
	// slack_post is a PermWrite tool so the gate's write-level check applies.
	slackTool := &writeMockTool{
		name:   "slack_post",
		result: tools.ToolResult{Output: "posted to slack"},
	}

	// Build a registry that contains slack_post (so executeSingle can look it
	// up) and tag it with the "slack" provider so the gate receives the
	// correct provider string.
	reg := tools.NewRegistry()
	reg.Register(slackTool)
	reg.TagTools([]string{"slack_post"}, "slack")

	// Mock LLM: first response is a tool_call for slack_post, second is a stop.
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("slack_post", "call-slack-1"),
			stopResponse("done"),
		},
	}

	// Gate with skipAll=false and NO promptFunc — gate.Check() for a PermWrite
	// tool will always return false (deny by default; see permissions.go:
	// "No prompt function — deny by default").
	//
	// This is the runtime equivalent of "provider not in agent's toolbelt":
	// the gate was configured without granting access to the slack provider.
	gate := permissions.NewGate(false /* skipAll=false */, nil /* no promptFunc */)

	permDeniedCalled := false

	_, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Gate:     gate,
		Messages: []backend.Message{{Role: "user", Content: "post to slack"}},
		OnPermissionDenied: func(name string) {
			if name == "slack_post" {
				permDeniedCalled = true
			}
		},
	})
	if err != nil {
		t.Fatalf("RunLoop returned unexpected error: %v", err)
	}

	// The gate must have blocked slack_post — Execute() must never have been called.
	slackTool.mu.Lock()
	executeCalls := slackTool.callCount
	slackTool.mu.Unlock()

	if executeCalls != 0 {
		t.Errorf("slack_post.Execute() was called %d time(s); expected 0 — gate must block execution", executeCalls)
	}

	// OnPermissionDenied callback must have fired.
	if !permDeniedCalled {
		t.Error("OnPermissionDenied was not called for slack_post; expected gate to trigger it")
	}

	// Verify that the "error: permission denied" string was fed back to the
	// model as the tool result message (it must appear in the second request).
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) < 2 {
		t.Fatalf("expected at least 2 backend calls (tool call + follow-up), got %d", len(mb.lastRequests))
	}
	secondReqMsgs := mb.lastRequests[1].Messages
	found := false
	for _, msg := range secondReqMsgs {
		if msg.Role == "tool" && msg.ToolName == "slack_post" && strings.Contains(msg.Content, "permission denied") {
			found = true
			break
		}
	}
	if !found {
		t.Error(`expected a tool message with content "error: permission denied" for slack_post in the follow-up request`)
	}
}

// TestAgentToolbelt_RuntimeRejection_ReadToolUnauthorizedProvider proves that
// Gate.Check rejects PermRead tools from providers outside the toolbelt.
// This closes the gap where Layer 1 (schema filtering) is the primary defence
// but Layer 2 (gate runtime check) previously allowed PermRead through regardless of provider.
func TestAgentToolbelt_RuntimeRejection_ReadToolUnauthorizedProvider(t *testing.T) {
	g := permissions.NewGate(false, nil)
	g.SetAllowedProviders(map[string]bool{"github": true})

	// slack_read is a PermRead tool from "slack" — not in the toolbelt
	slackRead := &readMockTool{name: "slack_read"}
	reg := tools.NewRegistry()
	reg.Register(slackRead)
	reg.TagTools([]string{"slack_read"}, "slack")

	req := permissions.PermissionRequest{
		ToolName: "slack_read",
		Level:    tools.PermRead,
		Provider: reg.ProviderFor("slack_read"),
	}
	if g.Check(req) {
		t.Error("expected Gate.Check to reject PermRead tool from unauthorized provider")
	}
}

// readMockTool is a test double that reports PermRead. Used to verify that
// Gate.Check rejects PermRead tools from providers not in the toolbelt.
// (Prior to the provider enforcement fix, PermRead always returned true.)
type readMockTool struct {
	name string
}

func (t *readMockTool) Name() string                      { return t.name }
func (t *readMockTool) Description() string               { return "" }
func (t *readMockTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *readMockTool) Schema() backend.Tool {
	return backend.Tool{Type: "function", Function: backend.ToolFunction{Name: t.name}}
}
func (t *readMockTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	return tools.ToolResult{}
}

// writeMockTool is a test double that reports PermWrite (unlike mockTool which
// reports PermRead). This is required for the gate's write-level enforcement to
// fire: permissions.Gate.Check() always allows PermRead tools, so tests that
// want to exercise denial must use a PermWrite tool.
type writeMockTool struct {
	name      string
	result    tools.ToolResult
	callCount int
	mu        sync.Mutex
}

func (t *writeMockTool) Name() string                      { return t.name }
func (t *writeMockTool) Description() string               { return "" }
func (t *writeMockTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (t *writeMockTool) Schema() backend.Tool {
	return backend.Tool{Type: "function", Function: backend.ToolFunction{Name: t.name}}
}
func (t *writeMockTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.callCount++
	return t.result
}
