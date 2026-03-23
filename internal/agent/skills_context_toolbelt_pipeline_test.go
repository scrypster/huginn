package agent

// hardening_pipeline_test.go — Runtime enforcement pipeline integration tests.
//
// Verifies the full data flow for skills isolation, context notes injection,
// and connection (toolbelt) enforcement through the real production paths:
// AgentChat, ChatWithAgent, and CodeWithAgent.
//
// Gaps addressed (previously only unit-tested or tested via RunLoop directly):
//   1. Skills content reaching the system prompt via AgentChat
//   2. Context notes injected into the system prompt when ContextNotesEnabled=true
//   3. Context notes NOT injected when ContextNotesEnabled=false
//   4. Toolbelt schema filtering enforced inside ChatWithAgent (E2E, not just RunLoop)
//   5. Default-deny: zero schemas when agent has no toolbelt/LocalTools
//   6. LocalTools=["*"] gives all builtin-tagged schemas
//   7. LocalTools=[named] restricts to only those named schemas

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/tools"
)

// ---------------------------------------------------------------------------
// Skills → system prompt (AgentChat path)
// ---------------------------------------------------------------------------

// TestAgentChat_SkillsFragmentInSystemPrompt verifies that when an agent's
// skill list is ["code"], only "Code Skill" content — not "Plan Skill" — appears
// in messages[0] (the system prompt) sent to the LLM backend.
//
// Enforcement pipeline:
//
//	skillsReg → skillsFragmentFor(agReg) → buildAgentSystemPrompt → messages[0]
func TestAgentChat_SkillsFragmentInSystemPrompt(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{stopResponse("done")},
	}
	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)

	// Wire an empty tool registry so AgentChat uses the agentic loop (not plain Chat).
	o.SetTools(tools.NewRegistry(), permissions.NewGate(true, nil))

	// Wire a skills registry with "code" and "plan" skills.
	o.SetSkillsRegistry(newTestSkillsReg())

	// Default agent requests only "code".
	o.SetAgentRegistry(agentRegWith([]string{"code"}))

	if err := o.AgentChat(context.Background(), "hello", 5, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("AgentChat: %v", err)
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) == 0 {
		t.Fatal("backend received no requests")
	}
	systemPrompt := mb.lastRequests[0].Messages[0].Content

	if !strings.Contains(systemPrompt, "Code Skill") {
		t.Errorf("system prompt must contain 'Code Skill' for code-only agent; got:\n%s", systemPrompt)
	}
	if strings.Contains(systemPrompt, "Plan Skill") {
		t.Errorf("'Plan Skill' must NOT appear for code-only agent; got:\n%s", systemPrompt)
	}
}

// TestAgentChat_NoSkills_EmptyFragment verifies that an agent with an empty
// skill list (skills:[]) receives no skill content in the system prompt.
func TestAgentChat_NoSkills_EmptyFragment(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{stopResponse("done")},
	}
	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)
	o.SetTools(tools.NewRegistry(), permissions.NewGate(true, nil))
	o.SetSkillsRegistry(newTestSkillsReg())
	o.SetAgentRegistry(agentRegWith([]string{})) // empty skill list

	if err := o.AgentChat(context.Background(), "hi", 5, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("AgentChat: %v", err)
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) == 0 {
		t.Fatal("backend received no requests")
	}
	systemPrompt := mb.lastRequests[0].Messages[0].Content

	if strings.Contains(systemPrompt, "Code Skill") || strings.Contains(systemPrompt, "Plan Skill") {
		t.Errorf("no skill content expected for skills=[], got:\n%s", systemPrompt)
	}
}

// ---------------------------------------------------------------------------
// Context notes → system prompt (AgentChat path)
// ---------------------------------------------------------------------------

// TestAgentChat_ContextNotesInSystemPrompt verifies that when an agent has
// ContextNotesEnabled=true and the huginn home is configured, the contents of
// the agent's memory file appear in the system prompt sent to the backend.
//
// Notes file path convention: {huginnHome}/agents/{agentName}.memory.md
func TestAgentChat_ContextNotesInSystemPrompt(t *testing.T) {
	huginnHome := t.TempDir()

	// Write the agent's memory file.
	agentsDir := filepath.Join(huginnHome, "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	const notesContent = "pipeline-hardening: remember this specific note"
	notesPath := filepath.Join(agentsDir, "notes-agent.memory.md")
	if err := os.WriteFile(notesPath, []byte(notesContent), 0644); err != nil {
		t.Fatal(err)
	}

	mb := &mockBackend{responses: []*backend.ChatResponse{stopResponse("done")}}
	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)
	o.SetTools(tools.NewRegistry(), permissions.NewGate(true, nil))
	o.SetHuginnHome(huginnHome)

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:                "notes-agent",
		IsDefault:           true,
		ContextNotesEnabled: true,
	})
	o.SetAgentRegistry(reg)

	if err := o.AgentChat(context.Background(), "hi", 5, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("AgentChat: %v", err)
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) == 0 {
		t.Fatal("backend received no requests")
	}
	systemPrompt := mb.lastRequests[0].Messages[0].Content
	if !strings.Contains(systemPrompt, notesContent) {
		t.Errorf("system prompt must contain context notes; got:\n%s", systemPrompt)
	}
}

// TestAgentChat_ContextNotesDisabled verifies that when ContextNotesEnabled=false,
// the notes file contents do NOT appear in the system prompt.
func TestAgentChat_ContextNotesDisabled(t *testing.T) {
	huginnHome := t.TempDir()

	agentsDir := filepath.Join(huginnHome, "agents")
	os.MkdirAll(agentsDir, 0755)
	const secretContent = "secret-should-not-appear-in-prompt"
	os.WriteFile(filepath.Join(agentsDir, "no-notes.memory.md"), []byte(secretContent), 0644)

	mb := &mockBackend{responses: []*backend.ChatResponse{stopResponse("done")}}
	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)
	o.SetTools(tools.NewRegistry(), permissions.NewGate(true, nil))
	o.SetHuginnHome(huginnHome)

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:                "no-notes",
		IsDefault:           true,
		ContextNotesEnabled: false, // explicitly disabled
	})
	o.SetAgentRegistry(reg)

	if err := o.AgentChat(context.Background(), "hi", 5, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("AgentChat: %v", err)
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) == 0 {
		t.Fatal("backend received no requests")
	}
	systemPrompt := mb.lastRequests[0].Messages[0].Content
	if strings.Contains(systemPrompt, secretContent) {
		t.Errorf("notes content must NOT appear when ContextNotesEnabled=false; got:\n%s", systemPrompt)
	}
}

// ---------------------------------------------------------------------------
// Toolbelt → schema filtering via ChatWithAgent (E2E path)
// ---------------------------------------------------------------------------

// TestChatWithAgent_ToolbeltFilteredAtBackend verifies that when ChatWithAgent
// is called with a github-only agent, only the github tool schema reaches the
// LLM backend. slack_post must never appear.
//
// This is an end-to-end test of the toolbelt enforcement pipeline through the
// real ChatWithAgent production path (which internally calls applyToolbelt),
// not just RunLoop with pre-filtered schemas.
func TestChatWithAgent_ToolbeltFilteredAtBackend(t *testing.T) {
	mb := &mockBackend{responses: []*backend.ChatResponse{stopResponse("done")}}
	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)

	// Registry has both github and slack tools.
	reg := newTestToolsReg()
	gate := permissions.NewGate(true, nil)
	o.SetTools(reg, gate)

	// Agent is restricted to github only.
	ag := agentWithToolbelt([]string{"github"}, false)
	ag.ModelID = "test-model"

	if err := o.ChatWithAgent(context.Background(), ag, "list my prs", "", nil, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) == 0 {
		t.Fatal("backend received no requests")
	}

	for i, req := range mb.lastRequests {
		for _, schema := range req.Tools {
			if schema.Function.Name == "slack_post" {
				t.Errorf("turn %d: slack_post was sent to backend but agent is github-only", i+1)
			}
		}
	}

	found := false
	for _, schema := range mb.lastRequests[0].Tools {
		if schema.Function.Name == "github_list_prs" {
			found = true
			break
		}
	}
	if !found {
		t.Error("github_list_prs must appear in backend request for github-only agent")
	}
}

// TestChatWithAgent_NoToolbelt_NoSchemasAtBackend verifies the default-deny
// principle: an agent with no toolbelt entries and no LocalTools results in
// zero schemas being sent to the backend.
func TestChatWithAgent_NoToolbelt_NoSchemasAtBackend(t *testing.T) {
	mb := &mockBackend{responses: []*backend.ChatResponse{stopResponse("done")}}
	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)

	reg := newTestToolsReg()
	o.SetTools(reg, permissions.NewGate(true, nil))

	// Agent has neither Toolbelt nor LocalTools — default-deny.
	ag := &agents.Agent{Name: "locked-agent", ModelID: "test-model"}

	if err := o.ChatWithAgent(context.Background(), ag, "do something", "", nil, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) == 0 {
		t.Fatal("backend received no requests")
	}
	if n := len(mb.lastRequests[0].Tools); n != 0 {
		t.Errorf("expected 0 tool schemas for agent with no toolbelt, got %d: %v",
			n, mb.lastRequests[0].Tools)
	}
}

// ---------------------------------------------------------------------------
// LocalTools enforcement via ChatWithAgent
// ---------------------------------------------------------------------------

// TestChatWithAgent_LocalTools_StarGivesBuiltins verifies that LocalTools=["*"]
// causes all tools tagged as "builtin" to appear in the backend request.
//
// Uses a minimal registry with a single test tool tagged as "builtin" to avoid
// real filesystem dependencies.
func TestChatWithAgent_LocalTools_StarGivesBuiltins(t *testing.T) {
	builtinTool := &testTool{
		schema: backend.Tool{Type: "function", Function: backend.ToolFunction{Name: "hardening_builtin_echo"}},
	}
	reg := tools.NewRegistry()
	reg.Register(builtinTool)
	reg.TagTools([]string{"hardening_builtin_echo"}, "builtin")

	mb := &mockBackend{responses: []*backend.ChatResponse{stopResponse("done")}}
	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)
	o.SetTools(reg, permissions.NewGate(true, nil))

	ag := &agents.Agent{
		Name:       "builtin-user",
		ModelID:    "test-model",
		LocalTools: []string{"*"}, // all builtins
	}

	if err := o.ChatWithAgent(context.Background(), ag, "use tools", "", nil, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) == 0 {
		t.Fatal("backend received no requests")
	}

	found := false
	for _, schema := range mb.lastRequests[0].Tools {
		if schema.Function.Name == "hardening_builtin_echo" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("hardening_builtin_echo must appear for LocalTools=[*]; got: %v",
			mb.lastRequests[0].Tools)
	}
}

// TestChatWithAgent_LocalTools_Named_RestrictsToListed verifies that when
// LocalTools names specific tools (not "*"), only those tools appear and
// other registered builtins are excluded.
func TestChatWithAgent_LocalTools_Named_RestrictsToListed(t *testing.T) {
	toolAlpha := &testTool{schema: backend.Tool{Type: "function", Function: backend.ToolFunction{Name: "tool_alpha"}}}
	toolBeta := &testTool{schema: backend.Tool{Type: "function", Function: backend.ToolFunction{Name: "tool_beta"}}}

	reg := tools.NewRegistry()
	reg.Register(toolAlpha)
	reg.Register(toolBeta)
	reg.TagTools([]string{"tool_alpha", "tool_beta"}, "builtin")

	mb := &mockBackend{responses: []*backend.ChatResponse{stopResponse("done")}}
	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)
	o.SetTools(reg, permissions.NewGate(true, nil))

	ag := &agents.Agent{
		Name:       "restricted",
		ModelID:    "test-model",
		LocalTools: []string{"tool_alpha"}, // only alpha allowed
	}

	if err := o.ChatWithAgent(context.Background(), ag, "go", "", nil, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) == 0 {
		t.Fatal("backend received no requests")
	}

	for _, schema := range mb.lastRequests[0].Tools {
		if schema.Function.Name == "tool_beta" {
			t.Errorf("tool_beta must NOT appear when LocalTools=[tool_alpha]; schemas: %v",
				mb.lastRequests[0].Tools)
		}
	}

	found := false
	for _, schema := range mb.lastRequests[0].Tools {
		if schema.Function.Name == "tool_alpha" {
			found = true
		}
	}
	if !found {
		t.Errorf("tool_alpha must appear when listed in LocalTools; schemas: %v",
			mb.lastRequests[0].Tools)
	}
}

// ---------------------------------------------------------------------------
// Cross-path: CodeWithAgent enforces toolbelt (parallel to ChatWithAgent)
// ---------------------------------------------------------------------------

// TestCodeWithAgent_ToolbeltFilteredAtBackend verifies that CodeWithAgent
// also applies applyToolbelt, excluding unauthorized provider schemas.
func TestCodeWithAgent_ToolbeltFilteredAtBackend(t *testing.T) {
	mb := &mockBackend{responses: []*backend.ChatResponse{stopResponse("done")}}
	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)

	reg := newTestToolsReg()
	o.SetTools(reg, permissions.NewGate(true, nil))

	// github-only agent; slack must be absent.
	ag := agentWithToolbelt([]string{"github"}, false)
	ag.ModelID = "test-model"

	if err := o.CodeWithAgent(context.Background(), ag, "list prs", 5, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("CodeWithAgent: %v", err)
	}

	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) == 0 {
		t.Fatal("backend received no requests")
	}

	for i, req := range mb.lastRequests {
		for _, schema := range req.Tools {
			if schema.Function.Name == "slack_post" {
				t.Errorf("turn %d: slack_post sent to backend via CodeWithAgent for github-only agent", i+1)
			}
		}
	}
}
