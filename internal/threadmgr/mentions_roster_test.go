package threadmgr

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
)

func TestRestrictMentionNames_EmptyRosterKeepsAllAgents(t *testing.T) {
	got := restrictMentionNames([]string{"Tess", "Steve"}, nil)
	if len(got) != 2 || got[0] != "Tess" || got[1] != "Steve" {
		t.Errorf("empty roster should keep all agents, got %v", got)
	}
	got = restrictMentionNames([]string{"Tess", "Steve"}, []string{})
	if len(got) != 2 {
		t.Errorf("empty slice roster should keep all agents, got %v", got)
	}
}

func TestRestrictMentionNames_DMRosterDropsOutsider(t *testing.T) {
	got := restrictMentionNames([]string{"Tess", "Steve"}, []string{"Tess"})
	if len(got) != 1 || got[0] != "Tess" {
		t.Errorf("Tess-only DM roster should keep Tess only, got %v", got)
	}
}

func TestRestrictMentionNames_ChannelRosterKeepsMember(t *testing.T) {
	got := restrictMentionNames([]string{"Chris", "Steve", "Tess"}, []string{"Chris", "Steve"})
	if len(got) != 2 || got[0] != "Chris" || got[1] != "Steve" {
		t.Errorf("channel roster should keep Chris and Steve, got %v", got)
	}
}

func TestParseMentions_TessDMRosterIgnoresSteve(t *testing.T) {
	// Same agentNames CreateFromMentions will pass after roster restrict.
	reqs := ParseMentions("Delegated to @Steve: pong", []string{"Tess"})
	if len(reqs) != 0 {
		t.Errorf("Tess-only roster must not parse @Steve, got %v", reqs)
	}
	reqs = ParseMentions("please ask @Steve about hostname", []string{"Tess"})
	if len(reqs) != 0 {
		t.Errorf("user-text @Steve in a Tess-only roster must not parse, got %v", reqs)
	}
}

func TestParseMentions_ChannelRosterStillMatchesSteve(t *testing.T) {
	reqs := ParseMentions("@Steve please look at hostname", []string{"Chris", "Steve", "Sam"})
	if len(reqs) != 1 || reqs[0].AgentName != "Steve" {
		t.Errorf("channel roster member @Steve should parse, got %v", reqs)
	}
}

func finishImmediatelyBackend() *fakeBackend {
	return &fakeBackend{
		response: &backend.ChatResponse{
			ToolCalls: []backend.ToolCall{
				{
					ID: "tc-1",
					Function: backend.ToolCallFunction{
						Name:      "finish",
						Arguments: map[string]any{"summary": "done", "status": "completed"},
					},
				},
			},
			DoneReason: "tool_calls",
		},
	}
}

func tessSteveRegistry() *agents.AgentRegistry {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Tess", ModelID: "claude-haiku-4"})
	reg.Register(&agents.Agent{Name: "Steve", ModelID: "claude-haiku-4"})
	reg.Register(&agents.Agent{Name: "Chris", ModelID: "claude-haiku-4"})
	return reg
}

func waitForAgentThread(t *testing.T, tm *ThreadManager, sessionID, agent string, want bool) *Thread {
	t.Helper()
	wait := 2 * time.Second
	if !want {
		wait = 150 * time.Millisecond
	}
	deadline := time.Now().Add(wait)
	var found *Thread
	for time.Now().Before(deadline) {
		found = nil
		for _, thr := range tm.ListBySession(sessionID) {
			if thr.AgentID == agent {
				found = thr
				break
			}
		}
		if want && found != nil {
			return found
		}
		if !want && found != nil {
			t.Fatalf("expected no thread for %s, got %s", agent, found.ID)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if want && found == nil {
		t.Fatalf("expected a thread for %s, got none", agent)
	}
	return found
}

func drainThreads(t *testing.T, tm *ThreadManager, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		allDone := true
		threads := tm.ListBySession(sessionID)
		if len(threads) == 0 {
			return
		}
		for _, thr := range threads {
			if thr.Status != StatusDone && thr.Status != StatusError {
				allDone = false
				break
			}
		}
		if allDone {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCreateFromMentions_TessDM_AssistantAtSteveDoesNotSpawn(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("tess-dm", "/tmp", "claude-haiku-4")
	reg := tessSteveRegistry()

	CreateFromMentions(
		context.Background(),
		sess.ID,
		"Delegated to @Steve: pong",
		"",
		reg,
		store,
		sess,
		finishImmediatelyBackend(),
		func(string, string, map[string]any) {},
		NewCostAccumulator(0),
		tm,
		"Tess",
		[]string{"Tess"},
	)

	waitForAgentThread(t, tm, sess.ID, "Steve", false)
}

func TestCreateFromMentions_TessDM_UserAtSteveDoesNotSpawn(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("tess-dm-user", "/tmp", "claude-haiku-4")
	reg := tessSteveRegistry()

	CreateFromMentions(
		context.Background(),
		sess.ID,
		"please ask @Steve about hostname",
		"",
		reg,
		store,
		sess,
		finishImmediatelyBackend(),
		func(string, string, map[string]any) {},
		NewCostAccumulator(0),
		tm,
		"Tess",
		[]string{"Tess"},
	)

	waitForAgentThread(t, tm, sess.ID, "Steve", false)
}

func TestCreateFromMentions_ChannelRoster_SteveStillSpawns(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("mention-proof", "/tmp", "claude-haiku-4")
	reg := tessSteveRegistry()

	CreateFromMentions(
		context.Background(),
		sess.ID,
		"@Steve please look at hostname",
		"",
		reg,
		store,
		sess,
		finishImmediatelyBackend(),
		func(string, string, map[string]any) {},
		NewCostAccumulator(0),
		tm,
		"Chris",
		[]string{"Chris", "Steve", "Sam"},
	)

	waitForAgentThread(t, tm, sess.ID, "Steve", true)
	drainThreads(t, tm, sess.ID)
}

func TestCreateFromMentions_StandaloneNoRoster_SteveStillSpawns(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("standalone", "/tmp", "claude-haiku-4")
	reg := tessSteveRegistry()

	CreateFromMentions(
		context.Background(),
		sess.ID,
		"@Steve hello",
		"",
		reg,
		store,
		sess,
		finishImmediatelyBackend(),
		func(string, string, map[string]any) {},
		NewCostAccumulator(0),
		tm,
		"",
		nil,
	)

	waitForAgentThread(t, tm, sess.ID, "Steve", true)
	drainThreads(t, tm, sess.ID)
}
