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

func leadWithDelegationTools(t *testing.T) (*Orchestrator, *mockBackend, *agents.Agent) {
	t.Helper()
	mb := &mockBackend{responses: []*backend.ChatResponse{stopResponse("It's 3:24 PM ET.")}}
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	reg := tools.NewRegistry()
	for _, name := range []string{"delegate_to_agent", "wait_for_threads", "list_team_status", "recall_thread_result", "create_agent", "bash"} {
		reg.Register(&mockTool{name: name, result: tools.ToolResult{Output: "ok"}})
	}
	reg.TagTools([]string{"delegate_to_agent", "wait_for_threads", "list_team_status", "recall_thread_result", "create_agent", "bash"}, "builtin")
	o.SetTools(reg, permissions.NewGate(true, nil))
	ag := &agents.Agent{
		Name:         "Winston",
		ModelID:      "qwen2.5-coder:14b",
		SystemPrompt: "You are Winston, the Chief of Staff.",
		LocalTools:   []string{"*"},
	}
	return o, mb, ag
}

func toolNames(req backend.ChatRequest) []string {
	var names []string
	for _, s := range req.Tools {
		names = append(names, s.Function.Name)
	}
	return names
}

func TestChatWithAgent_TrivialAskSkipsDelegationPlan(t *testing.T) {
	o, mb, ag := leadWithDelegationTools(t)
	if err := o.ChatWithAgent(context.Background(), ag, "@Winston what time is it", "sess-clock", nil, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) == 0 {
		t.Fatal("backend received no requests")
	}
	req := mb.lastRequests[0]
	if len(req.Tools) != 0 {
		t.Fatalf("trivial time ask must not send tool schemas (skip wait_for_threads / toolbelt); got %v", toolNames(req))
	}
	sys := ""
	if len(req.Messages) > 0 {
		sys = req.Messages[0].Content
	}
	if !strings.Contains(sys, "Local time now:") {
		t.Fatalf("clock must still be injected:\n%s", sys)
	}
	for _, banned := range []string{"wait_for_threads", "delegate_to_agent", "create_agent"} {
		for _, name := range toolNames(req) {
			if name == banned {
				t.Errorf("trivial ask leaked %s into tool schemas", banned)
			}
		}
	}
}

func TestChatWithAgent_HireStillGetsDelegationTools(t *testing.T) {
	o, mb, ag := leadWithDelegationTools(t)
	if err := o.ChatWithAgent(context.Background(), ag, "hire a teammate named Nova who researches", "sess-hire", nil, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) == 0 {
		t.Fatal("backend received no requests")
	}
	names := map[string]bool{}
	for _, n := range toolNames(mb.lastRequests[0]) {
		names[n] = true
	}
	if !names["delegate_to_agent"] && !names["create_agent"] && !names["wait_for_threads"] {
		t.Fatalf("hire ask must keep the toolbelt / delegation plan; got %v", toolNames(mb.lastRequests[0]))
	}
}

func TestChatWithAgent_TrivialAskEmitsThinkingStatus(t *testing.T) {
	o, _, ag := leadWithDelegationTools(t)
	var evs []backend.StreamEvent
	err := o.ChatWithAgent(context.Background(), ag, "what time is it", "sess-think", nil, nil, func(ev backend.StreamEvent) {
		evs = append(evs, ev)
	})
	if err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}
	found := false
	for _, ev := range evs {
		if ev.Type == backend.StreamStatus && ev.Content == "thinking" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("trivial ask must emit StreamStatus thinking immediately; got %+v", evs)
	}
}

func TestChatWithAgent_HeadcountAskSkipsDelegationPlan(t *testing.T) {
	o, mb, ag := leadWithDelegationTools(t)
	if err := o.ChatWithAgent(context.Background(), ag, "@Winston how many people are in this channel?", "sess-headcount", nil, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) == 0 {
		t.Fatal("backend received no requests")
	}
	req := mb.lastRequests[0]
	if len(req.Tools) != 0 {
		t.Fatalf("headcount ask must not send tool schemas; got %v", toolNames(req))
	}
	for _, banned := range []string{"wait_for_threads", "delegate_to_agent", "consult_agent", "create_agent"} {
		for _, name := range toolNames(req) {
			if name == banned {
				t.Errorf("headcount ask leaked %s into tool schemas", banned)
			}
		}
	}
}

func TestStripTrivialDelegationTools(t *testing.T) {
	t.Parallel()
	in := []backend.Tool{
		{Function: backend.ToolFunction{Name: "bash"}},
		{Function: backend.ToolFunction{Name: "delegate_to_agent"}},
		{Function: backend.ToolFunction{Name: "wait_for_threads"}},
		{Function: backend.ToolFunction{Name: "consult_agent"}},
		{Function: backend.ToolFunction{Name: "list_team_status"}},
	}
	out := stripTrivialAskDelegationTools(in)
	got := toolNames(backend.ChatRequest{Tools: out})
	if len(got) != 2 || got[0] != "bash" || got[1] != "list_team_status" {
		t.Fatalf("strip leftover belt = %v, want [bash list_team_status]", got)
	}
	if stripTrivialAskDelegationTools(nil) != nil && len(stripTrivialAskDelegationTools(nil)) != 0 {
		t.Fatalf("nil belt should stay empty")
	}
}
