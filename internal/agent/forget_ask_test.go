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

func TestIsForgetAsk_ImperativeForms(t *testing.T) {
	yes := []string{
		"forget my dog's name",
		"forget what I told you about the staging server",
		"delete what I said about the deploy window",
		"remove the note about valkyrie",
		"@Winston forget my dog's name",
		"please forget my dog's name",
	}
	for _, s := range yes {
		if !isForgetAsk(s) {
			t.Errorf("isForgetAsk(%q) = false, want true", s)
		}
	}
}

func TestIsForgetAsk_ExcludesQuestions(t *testing.T) {
	no := []string{
		"did you forget my dog's name?",
		"why did you forget what I told you about the server?",
		"what did I tell you about the server",
		"remember my dog's name",
		"",
	}
	for _, s := range no {
		if isForgetAsk(s) {
			t.Errorf("isForgetAsk(%q) = true, want false", s)
		}
	}
}

func TestForgetSubject_ExtractsCoreNounPhrase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"forget my dog's name", "my dog"},
		{"forget what I told you about the staging server", "the staging server"},
		{"delete what I said about valkyrie", "valkyrie"},
		{"remove my dog's name", "my dog"},
	}
	for _, tc := range cases {
		got, ok := forgetSubject(tc.in)
		if !ok {
			t.Fatalf("forgetSubject(%q) not ok", tc.in)
		}
		if got != tc.want {
			t.Errorf("forgetSubject(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestChatWithAgent_ForgetFastPath_MatchFound_Forgets(t *testing.T) {
	mb := &mockBackend{responses: []*backend.ChatResponse{stopResponse("should not run")}}
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	reg := tools.NewRegistry()
	recall := &gateRecallStubTool{name: "muninn_recall", output: `{"results":[{"id":"mem_odin","content":"my dog is named Odin","score":0.9}]}`}
	forget := &gateStubTool{name: "muninn_forget"}
	reg.Register(recall)
	reg.Register(forget)
	o.SetTools(reg, permissions.NewGate(true, nil))
	ag := &agents.Agent{
		Name:       "Winston",
		ModelID:    "qwen2.5-coder:14b",
		VaultName:  "huginn",
		MemoryMode: "conversational",
	}
	var tokens []string
	if err := o.ChatWithAgent(context.Background(), ag, "forget my dog's name", "sess-forget-1", func(tok string) {
		tokens = append(tokens, tok)
	}, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}
	joined := strings.ToLower(strings.Join(tokens, ""))
	if !strings.Contains(joined, "forgotten") {
		t.Fatalf("speech %q, want acknowledgement of forgetting", joined)
	}
	if forget.callCount() != 1 {
		t.Fatalf("forget called %d times, want 1", forget.callCount())
	}
	id, _ := forget.calls[0]["id"].(string)
	if id != "mem_odin" {
		t.Fatalf("forget id = %q, want mem_odin", id)
	}
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) != 0 {
		t.Fatalf("forget fast path must not call the model; got %d requests", len(mb.lastRequests))
	}
}

func TestChatWithAgent_ForgetFastPath_NoMatch_HonestReply(t *testing.T) {
	mb := &mockBackend{responses: []*backend.ChatResponse{stopResponse("should not run")}}
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	reg := tools.NewRegistry()
	recall := &gateRecallStubTool{name: "muninn_recall", output: `{"results":[]}`}
	forget := &gateStubTool{name: "muninn_forget"}
	reg.Register(recall)
	reg.Register(forget)
	o.SetTools(reg, permissions.NewGate(true, nil))
	ag := &agents.Agent{
		Name:       "Winston",
		ModelID:    "qwen2.5-coder:14b",
		VaultName:  "huginn",
		MemoryMode: "conversational",
	}
	var tokens []string
	if err := o.ChatWithAgent(context.Background(), ag, "forget my dog's name", "sess-forget-2", func(tok string) {
		tokens = append(tokens, tok)
	}, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}
	joined := strings.ToLower(strings.Join(tokens, ""))
	if !strings.Contains(joined, "don't have anything stored") {
		t.Fatalf("speech %q, want honest no-match reply", joined)
	}
	if forget.callCount() != 0 {
		t.Fatalf("forget called %d times, want 0 when nothing matched", forget.callCount())
	}
}

// Opus-vet BLOCK findings 2026-08-28: bare delete/remove are roster/channel
// commands, never memory ops; wildcard subjects must not fire.
func TestForgetSubject_NeverHijacksCommands(t *testing.T) {
	for _, ask := range []string{
		"delete the channel",
		"remove Steve from the company",
		"delete the smoke-test channel",
		"remove Codey",
		"forget it",
		"forget about it",
		"forget everything",
		"forget that",
	} {
		if _, ok := forgetSubject(ask); ok {
			t.Errorf("forget classifier hijacked %q", ask)
		}
	}
}

func TestForgetSubject_MemoryFramedDeleteStillWorks(t *testing.T) {
	cases := map[string]string{
		"delete what I told you about the staging server": "the staging server",
		"remove my old address from your memory":          "my old address",
		"forget my dog's name":                            "my dog",
		"can you forget where I live":                     "where i live",
	}
	for ask, want := range cases {
		got, ok := forgetSubject(ask)
		if !ok {
			t.Errorf("%q should classify as forget", ask)
			continue
		}
		if !strings.EqualFold(got, want) {
			t.Errorf("%q subject = %q, want %q", ask, got, want)
		}
	}
}
