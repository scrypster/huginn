package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/tools"
)

// TestChatWithAgent_InjectsLocalClockET verifies hallway ChatWithAgent puts
// the machine clock (America/New_York, labeled ET) on the system prompt.
// Mock backend only — does not load a model.
func TestChatWithAgent_InjectsLocalClockET(t *testing.T) {
	mb := &mockBackend{responses: []*backend.ChatResponse{stopResponse("done")}}
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	o.SetTools(tools.NewRegistry(), permissions.NewGate(true, nil))

	ag := &agents.Agent{
		Name:         "Steve",
		ModelID:      "test-model",
		SystemPrompt: "You are Steve.",
	}
	if err := o.ChatWithAgent(context.Background(), ag, "ask Winston what time it is", "", nil, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) == 0 || len(mb.lastRequests[0].Messages) == 0 {
		t.Fatal("backend received no system message")
	}
	sys := mb.lastRequests[0].Messages[0].Content
	if !strings.Contains(sys, "Local time now:") {
		t.Fatalf("missing Local time now: in system prompt:\n%s", sys)
	}
	if !strings.Contains(sys, " ET") {
		t.Fatalf("clock must be timezone-labeled ET:\n%s", sys)
	}
	if strings.Contains(sys, "EDT") || strings.Contains(sys, "EST") {
		t.Fatalf("label must be ET, not EDT/EST:\n%s", sys)
	}
}

func TestChatWithAgent_InjectsRosterCards(t *testing.T) {
	mb := &mockBackend{responses: []*backend.ChatResponse{stopResponse("done")}}
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	o.SetTools(tools.NewRegistry(), permissions.NewGate(true, nil))
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Winston", ModelID: "claude-sonnet-4", SystemPrompt: "You are Winston, the Chief of Staff."})
	reg.Register(&agents.Agent{Name: "Steve", ModelID: "qwen2.5-coder:14b", SystemPrompt: "You are Steve, a coder.", LocalTools: []string{"bash"}})
	o.SetAgentRegistry(reg)

	ag, ok := reg.ByName("Winston")
	if !ok {
		t.Fatal("Winston missing")
	}
	if err := o.ChatWithAgent(context.Background(), ag, "hello team", "", nil, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) == 0 || len(mb.lastRequests[0].Messages) == 0 {
		t.Fatal("backend received no system message")
	}
	sys := mb.lastRequests[0].Messages[0].Content
	if !strings.Contains(sys, "## Your Team") || !strings.Contains(sys, "Steve") {
		t.Fatalf("expected roster cards in ChatWithAgent prompt:\n%s", sys)
	}
	if !strings.Contains(sys, "tools:") {
		t.Fatalf("expected infoFn tier/tools annotation on roster card:\n%s", sys)
	}
	if !strings.Contains(sys, "delegate_to_agent") {
		t.Fatalf("high-tier lead should get delegate instruction:\n%s", sys)
	}
}
