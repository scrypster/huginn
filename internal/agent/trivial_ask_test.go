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

func delegationTestRegistry() *tools.Registry {
	reg := tools.NewRegistry()
	for _, name := range []string{"delegate_to_agent", "wait_for_threads", "consult_agent", "list_team_status", "bash"} {
		reg.Register(&mockTool{name: name})
	}
	reg.TagTools([]string{"delegate_to_agent", "wait_for_threads", "consult_agent", "list_team_status", "bash"}, "builtin")
	return reg
}

func TestChatWithAgent_TrivialAsk_NoDelegationSchemas(t *testing.T) {
	mb := &mockBackend{responses: []*backend.ChatResponse{stopResponse("9")}}
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	o.SetTools(delegationTestRegistry(), permissions.NewGate(true, nil))
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:         "Winston",
		ModelID:      "claude-sonnet-4",
		SystemPrompt: "You are Winston, the Chief of Staff.",
		LocalTools:   []string{"*"},
		IsDefault:    true,
	})
	reg.Register(&agents.Agent{Name: "Steve", ModelID: "qwen2.5-coder:14b", SystemPrompt: "You are Steve."})
	o.SetAgentRegistry(reg)
	ag, _ := reg.ByName("Winston")

	if err := o.ChatWithAgent(context.Background(), ag, "how many people are in this channel", "", nil, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) == 0 {
		t.Fatal("backend received no request")
	}
	req := mb.lastRequests[0]
	for _, schema := range req.Tools {
		switch schema.Function.Name {
		case "wait_for_threads", "delegate_to_agent", "consult_agent":
			t.Errorf("trivial ask offered %s", schema.Function.Name)
		}
	}
	if len(req.Tools) != 0 {
		t.Errorf("trivial ask must be tools-free, got %d schemas", len(req.Tools))
	}
	sys := req.Messages[0].Content
	if !strings.Contains(sys, "Local time now:") {
		t.Fatalf("trivial ask dropped local clock:\n%s", sys)
	}
	if !strings.Contains(sys, "Steve") {
		t.Fatalf("trivial ask dropped roster:\n%s", sys)
	}
}

func TestChatWithAgent_HireStaysFullPath(t *testing.T) {
	mb := &mockBackend{responses: []*backend.ChatResponse{stopResponse("ok")}}
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	reg := delegationTestRegistry()
	reg.Register(&mockTool{name: "create_agent"})
	o.SetTools(reg, permissions.NewGate(true, nil))
	ag := &agents.Agent{
		Name:         "Winston",
		ModelID:      "claude-sonnet-4",
		SystemPrompt: "You are Winston.",
		LocalTools:   []string{"create_agent"},
	}
	if err := o.ChatWithAgent(context.Background(), ag, "hire Steve", "", nil, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) == 0 {
		t.Fatal("backend received no request")
	}
	found := map[string]bool{}
	for _, schema := range mb.lastRequests[0].Tools {
		found[schema.Function.Name] = true
	}
	for _, name := range []string{"delegate_to_agent", "wait_for_threads"} {
		if found[name] {
			t.Errorf("hire ask must not offer 14b %s", name)
		}
	}
	if !found["create_agent"] {
		t.Errorf("hire ask missing create_agent")
	}
}

func TestIsTrivialAsk_True(t *testing.T) {
	for _, ask := range []string{
		"what time is it",
		"@Winston what time is it",
		"time is it",
		"What time is it?",
		"current date",
		"@Winston what day is it?",
		"ping",
		"pong",
		"@Winston ping",
		"@Winston ping!",
		"thanks",
		"thank you",
		"ok",
		"okay",
		"got it",
		"who is here",
		"who's here",
		"who is on the team",
		"who's on the team",
		"roster",
		"how many people are in this channel",
		"who is in this channel",
		"who's in this channel",
		"how many people",
	} {
		if !IsTrivialAsk(ask) {
			t.Errorf("IsTrivialAsk(%q) = false, want true", ask)
		}
	}
}

func TestIsTrivialAsk_FalseFullPath(t *testing.T) {
	for _, ask := range []string{
		"hire Steve",
		"create a teammate",
		"add a teammate",
		"create an agent",
		"create_agent",
		"mesh the hallway",
		"@Winston @Reggie pong",
		"Ask Steve for the hostname",
		"ask Steve",
		"company wall",
		"hello",
		"hello team",
		"list my prs",
		"use tools",
	} {
		if IsTrivialAsk(ask) {
			t.Errorf("IsTrivialAsk(%q) = true, want false (full path)", ask)
		}
	}
}

func TestStripTrivialAskDelegationTools(t *testing.T) {
	in := []backend.Tool{
		{Function: backend.ToolFunction{Name: "bash"}},
		{Function: backend.ToolFunction{Name: "wait_for_threads"}},
		{Function: backend.ToolFunction{Name: "delegate_to_agent"}},
		{Function: backend.ToolFunction{Name: "consult_agent"}},
		{Function: backend.ToolFunction{Name: "list_team_status"}},
	}
	got := stripTrivialAskDelegationTools(in)
	names := map[string]bool{}
	for _, s := range got {
		names[s.Function.Name] = true
	}
	for _, deny := range []string{"wait_for_threads", "delegate_to_agent", "consult_agent"} {
		if names[deny] {
			t.Errorf("last-chance left %s on the trivial belt", deny)
		}
	}
	if !names["bash"] || !names["list_team_status"] {
		t.Fatalf("stripped non-delegation tools: %v", names)
	}
	if stripTrivialAskDelegationTools(nil) != nil {
		t.Fatal("nil schemas should stay nil")
	}
}

func TestChatWithAgent_PingShortCircuitNoLLM(t *testing.T) {
	mb := &mockBackend{responses: []*backend.ChatResponse{stopResponse("should-not-run")}}
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	ag := &agents.Agent{Name: "Winston", ModelID: "claude-sonnet-4", SystemPrompt: "You are Winston."}
	var tokens strings.Builder
	if err := o.ChatWithAgent(context.Background(), ag, "@Winston ping", "", func(tkn string) { tokens.WriteString(tkn) }, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) != 0 {
		t.Fatalf("ping must not call the LLM, got %d requests", len(mb.lastRequests))
	}
	if tokens.String() != "Pong." {
		t.Fatalf("ping tokens %q, want Pong.", tokens.String())
	}
}

func TestIsTrivialPingAsk(t *testing.T) {
	for _, ask := range []string{"ping", "pong", "@Winston ping", "@Winston ping one", "ping two", "ping three", "ping 2"} {
		if !IsTrivialPingAsk(ask) {
			t.Errorf("IsTrivialPingAsk(%q) = false, want true", ask)
		}
		if !IsTrivialAsk(ask) {
			t.Errorf("IsTrivialAsk(%q) = false, want true", ask)
		}
	}
	for _, ask := range []string{"how many people", "what time is it", "hire Steve", "thanks", "hello"} {
		if IsTrivialPingAsk(ask) {
			t.Errorf("IsTrivialPingAsk(%q) = true, want false", ask)
		}
	}
}

func TestChannelMembersLine_HeadcountAndWhoIsHere(t *testing.T) {
	names := []string{"Winston", "Reggie", "Steve"}
	for _, ask := range []string{
		"how many people are in this channel?",
		"who is here",
		"@Winston who is here",
		"who's on the team",
	} {
		got := channelMembersLine(ask, names)
		if got != "this channel members: Winston, Reggie, Steve" {
			t.Errorf("ask %q: %q", ask, got)
		}
	}
	if got := channelMembersLine("ping", names); got != "" {
		t.Fatalf("ping must not get roster line: %q", got)
	}
	if got := channelMembersLine("what company is this channel in?", names); got != "" {
		t.Fatalf("company ask must not get roster line: %q", got)
	}
	if got := channelMembersLine("how many people", nil); got != "" {
		t.Fatalf("empty names: %q", got)
	}
}
