package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/models"
)

// promptBudgetRegistry builds a roster large enough that AppendTeamRoster's
// output is unmistakably present or absent in the assembled system prompt.
// Winston is given create_agent (agents.AgentIsCoS) so AppendAvailableModels
// actually emits content for it below — without that, the models-block
// argument to AppendAvailableModels is a no-op for every non-CoS agent and
// TestChatWithAgent_NonTrivial_PromptUnchangedByPromptBudget's guard on that
// block is untested regardless of what it's given (see D9).
func promptBudgetRegistry() *agents.AgentRegistry {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:         "Winston",
		ModelID:      "claude-sonnet-4",
		SystemPrompt: "You are Winston, the Chief of Staff.",
		LocalTools:   []string{"*", "create_agent"},
		IsDefault:    true,
	})
	reg.Register(&agents.Agent{Name: "Steve", ModelID: "qwen2.5-coder:14b", SystemPrompt: "You are Steve, the coder."})
	reg.Register(&agents.Agent{Name: "Reggie", ModelID: "qwen2.5-coder:14b", SystemPrompt: "You are Reggie, the researcher."})
	return reg
}

// TestChatWithAgent_TrivialAck_GetsSkeletonPrompt verifies the perf-wave 2a
// prompt budget: a trivial-classified turn that doesn't need the roster
// (a plain "thanks") gets a skeleton system prompt — no team roster, no
// capabilities block — and the assembled prompt is materially smaller than
// what the same agent/registry would produce for a non-trivial turn.
func TestChatWithAgent_TrivialAck_GetsSkeletonPrompt(t *testing.T) {
	mb := &mockBackend{responses: []*backend.ChatResponse{stopResponse("You're welcome.")}}
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	reg := promptBudgetRegistry()
	o.SetAgentRegistry(reg)
	ag, _ := reg.ByName("Winston")

	// "thanks" is a trivial ack answered by backend.TrivialAckSpeech without
	// ever reaching the model — use a message that is trivial (skips
	// delegation/tools) but still short-circuits before the roster is
	// relevant: "ok" also resolves via TrivialAckSpeech, so use a trivial
	// time ask instead, which does reach the model via completeTrivialAsk.
	if err := o.ChatWithAgent(context.Background(), ag, "what time is it", "sess-skeleton", nil, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) == 0 {
		t.Fatal("backend received no requests")
	}
	sys := mb.lastRequests[0].Messages[0].Content

	for _, roster := range []string{"Steve", "Reggie", "Available team members"} {
		if strings.Contains(sys, roster) {
			t.Errorf("skeleton prompt for a trivial time ask must not contain the team roster, found %q:\n%s", roster, sys)
		}
	}
	// The capability addendum stays (D3): it's a handful of lines, and it's
	// the deterministic backstop for a trivial-ask misroute that still needs
	// to know what the agent can't do (e.g. image generation) — only the
	// ~2KB team roster and available-models block are cut.
	if !strings.Contains(sys, "## Your capabilities") {
		t.Errorf("skeleton prompt must keep the capability addendum:\n%s", sys)
	}
	if !strings.Contains(sys, "Local time now:") {
		t.Errorf("skeleton prompt must still carry the local clock:\n%s", sys)
	}
	if len(sys) > 600 {
		t.Errorf("skeleton prompt for a trivial ask should be small, got %d bytes:\n%s", len(sys), sys)
	}
}

// TestChatWithAgent_NonTrivial_PromptUnchangedByPromptBudget guards against
// the prompt-budget change (2a) accidentally trimming content from
// non-trivial turns. It snapshots the assembled system prompt for a
// non-trivial ask against a hand-built expectation using the same
// pre-existing helpers (BuildPersonaPromptWithMemory + AppendTeamRoster +
// AppendLocalClock) the dispatcher used before this change — any content
// loss on the non-trivial path fails this test byte-for-byte.
func TestChatWithAgent_NonTrivial_PromptUnchangedByPromptBudget(t *testing.T) {
	mb := &mockBackend{responses: []*backend.ChatResponse{stopResponse("Sure, I can help with that.")}}
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	reg := promptBudgetRegistry()
	o.SetAgentRegistry(reg)
	ag, _ := reg.ByName("Winston")

	userMsg := "please review the auth module and summarize any concerns"
	if err := o.ChatWithAgent(context.Background(), ag, userMsg, "sess-full", nil, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) == 0 {
		t.Fatal("backend received no requests")
	}
	sys := mb.lastRequests[0].Messages[0].Content

	// Reconstruct the expected prompt using the exact pre-change assembly
	// order (BuildPersonaPromptWithMemory -> AppendTeamRoster ->
	// AppendAvailableModels -> [skills/space/channel/conn omitted: none
	// configured in this test] -> AppendLocalClock). The clock line is
	// excluded from the comparison (and checked separately) since it stamps
	// a fresh time.Now() on each call — the point of this test is that the
	// non-clock content is untouched by the prompt-budget change.
	//
	// D9: the models-block argument uses the real production input
	// (models.GlobalProviderCatalog().AvailableModelsBlock()) rather than a
	// literal "" — the previous version of this test passed "" here, which
	// happened to match production's output for a non-CoS agent regardless
	// of whether AppendAvailableModels was even wired correctly, so a
	// regression that dropped the real catalog block from the dispatcher
	// would NOT have failed this test. Winston is CoS (see
	// promptBudgetRegistry) specifically so this path is actually exercised.
	// What this test still CANNOT catch: ctxText and recentSummaries are
	// faked as "" / nil rather than o.contextBuilder.Build(...) /
	// o.loadAgentSummaries(...)'s real output — a regression isolated to
	// those two inputs would need its own test.
	roster := agents.BuildRoster(reg, o.ModelInfoFn(), ag.Name)
	want := agents.BuildPersonaPromptWithMemory(ag, "", nil)
	want = agents.AppendTeamRoster(want, roster, agents.AgentSupportsDelegation(ag))
	want = agents.AppendAvailableModels(want, ag, models.GlobalProviderCatalog().AvailableModelsBlock())

	gotNoClock := stripLocalClockLine(sys)
	wantNoClock := stripLocalClockLine(backend.AppendLocalClock(want, time.Now()))
	if gotNoClock != wantNoClock {
		t.Fatalf("non-trivial system prompt changed by prompt-budget work:\ngot:\n%s\n\nwant:\n%s", gotNoClock, wantNoClock)
	}
	if !strings.Contains(sys, "Local time now:") {
		t.Errorf("non-trivial prompt must still carry the local clock:\n%s", sys)
	}
	for _, roster := range []string{"Steve", "Reggie"} {
		if !strings.Contains(sys, roster) {
			t.Errorf("non-trivial prompt must keep the full team roster, missing %q:\n%s", roster, sys)
		}
	}
}

// stripLocalClockLine removes the trailing "Local time now: ..." line so a
// byte-comparison of two prompts assembled at slightly different instants
// (both within the same test) never flakes on a minute boundary.
func stripLocalClockLine(s string) string {
	idx := strings.LastIndex(s, "Local time now:")
	if idx < 0 {
		return s
	}
	return strings.TrimRight(s[:idx], "\n")
}
