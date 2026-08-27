package threadmgr

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/spaces"
)

// rosterAwareDelegateFn mirrors the server delegate_to_agent Create path:
// validate the agent exists, then tm.Create with the session's SpaceID so the
// membership checker can deny outsiders before a thread is spawned.
func rosterAwareDelegateFn(sessionID, spaceID string, tm *ThreadManager, reg *agents.AgentRegistry) DelegateFn {
	return func(_ context.Context, p DelegateParams) DelegateResult {
		if _, found := reg.ByName(p.AgentName); !found {
			return DelegateResult{Err: errors.New("unknown agent " + p.AgentName)}
		}
		th, err := tm.Create(CreateParams{
			SessionID: sessionID,
			AgentID:   p.AgentName,
			Task:      p.Task,
			SpaceID:   spaceID,
		})
		if err != nil {
			return DelegateResult{Err: err}
		}
		return DelegateResult{ThreadID: th.ID, Spawned: true}
	}
}

func tessSteveChrisRegistry() *agents.AgentRegistry {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Tess", ModelID: "claude-haiku-4"})
	reg.Register(&agents.Agent{Name: "Steve", ModelID: "claude-haiku-4"})
	reg.Register(&agents.Agent{Name: "Chris", ModelID: "claude-haiku-4"})
	return reg
}

func TestDelegateToAgentTool_TessDM_SteveDeskPeerSpawns(t *testing.T) {
	tm := New()
	// Desk mesh: Tess's DM is 1:1 for the human, but Steve also has a desk DM.
	tm.SetMembershipChecker(&stubChecker{
		members:   []string{"Tess"},
		deskPeers: []string{"Tess", "Steve"},
		deskDM:    true,
	})
	reg := tessSteveChrisRegistry()
	tool := &DelegateToAgentTool{
		Fn: rosterAwareDelegateFn("tess-dm", "dm-tess", tm, reg),
	}

	result := tool.Execute(context.Background(), map[string]any{
		"agent": "Steve",
		"task":  "pong",
	})
	if result.IsError {
		t.Fatalf("desk DM Tess must be able to delegate to desk-peer Steve, got: %s", result.Error)
	}
	threads := tm.ListBySession("tess-dm")
	if len(threads) != 1 || threads[0].AgentID != "Steve" {
		t.Fatalf("expected a Steve thread, got %+v", threads)
	}
}

func TestDelegateToAgentTool_TessDM_StrangerNoDeskDM_Fails(t *testing.T) {
	tm := New()
	// Chris is registered but has no desk DM — not on the desk floor.
	tm.SetMembershipChecker(&stubChecker{
		members:   []string{"Tess"},
		deskPeers: []string{"Tess", "Steve"},
		deskDM:    true,
	})
	reg := tessSteveChrisRegistry()
	tool := &DelegateToAgentTool{
		Fn: rosterAwareDelegateFn("tess-dm", "dm-tess", tm, reg),
	}

	result := tool.Execute(context.Background(), map[string]any{
		"agent": "Chris",
		"task":  "pong",
	})
	if !result.IsError {
		t.Fatal("expected stranger with no desk DM to fail visibly")
	}
	if !strings.Contains(result.Error, "DELEGATE_FAIL") {
		t.Errorf("expected DELEGATE_FAIL, got: %s", result.Error)
	}
	if threads := tm.ListBySession("tess-dm"); len(threads) != 0 {
		t.Fatalf("stranger must not spawn, got %d threads", len(threads))
	}
}

func TestDelegateToAgentTool_ChannelRoster_SteveStillDelegates(t *testing.T) {
	tm := New()
	// #mention-proof: Chris lead, Steve is a member.
	tm.SetMembershipChecker(&stubChecker{members: []string{"Chris", "Steve", "Sam"}})
	reg := tessSteveChrisRegistry()
	tool := &DelegateToAgentTool{
		Fn: rosterAwareDelegateFn("mention-proof", "channel-mention-proof", tm, reg),
	}

	result := tool.Execute(context.Background(), map[string]any{
		"agent": "Steve",
		"task":  "look at hostname",
	})
	if result.IsError {
		t.Fatalf("channel roster member Steve must still be delegatable, got: %s", result.Error)
	}
	threads := tm.ListBySession("mention-proof")
	if len(threads) != 1 || threads[0].AgentID != "Steve" {
		t.Fatalf("expected a Steve thread, got %+v", threads)
	}
}

func TestDelegateToAgentTool_StandaloneNoRoster_SteveStillDelegates(t *testing.T) {
	tm := New()
	// Standalone session-mode: no SpaceID, checker is irrelevant.
	tm.SetMembershipChecker(&stubChecker{members: []string{"Tess"}})
	reg := tessSteveChrisRegistry()
	tool := &DelegateToAgentTool{
		Fn: rosterAwareDelegateFn("standalone", "", tm, reg),
	}

	result := tool.Execute(context.Background(), map[string]any{
		"agent": "Steve",
		"task":  "hello",
	})
	if result.IsError {
		t.Fatalf("standalone no-roster must keep all-agents behavior, got: %s", result.Error)
	}
	threads := tm.ListBySession("standalone")
	if len(threads) != 1 || threads[0].AgentID != "Steve" {
		t.Fatalf("expected a Steve thread, got %+v", threads)
	}
}

func TestDelegateToAgentTool_ChannelNonMember_FailsVisibly(t *testing.T) {
	tm := New()
	tm.SetMembershipChecker(&stubChecker{members: []string{"Chris", "Steve"}})
	reg := tessSteveChrisRegistry()
	tool := &DelegateToAgentTool{
		Fn: rosterAwareDelegateFn("mention-proof", "channel-mention-proof", tm, reg),
	}

	result := tool.Execute(context.Background(), map[string]any{
		"agent": "Tess",
		"task":  "outsider work",
	})
	if !result.IsError {
		t.Fatal("expected channel non-member Tess to fail visibly")
	}
	if !strings.Contains(result.Error, "DELEGATE_FAIL") {
		t.Errorf("expected DELEGATE_FAIL, got: %s", result.Error)
	}
	if threads := tm.ListBySession("mention-proof"); len(threads) != 0 {
		t.Fatalf("non-member must not spawn, got %d threads", len(threads))
	}
}

func TestDelegateToAgentTool_LiveSpaceStore_TessDMChannelStandalone(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("spaces migrate: %v", err)
	}
	store := spaces.NewSQLiteSpaceStore(db)
	dm, err := store.OpenDM("Tess")
	if err != nil {
		t.Fatalf("OpenDM: %v", err)
	}
	ch, err := store.CreateChannel("mention-proof", "Chris", []string{"Steve", "Sam"}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	tm := New()
	tm.SetMembershipChecker(store)
	reg := tessSteveChrisRegistry()

	if _, err := store.OpenDM("Steve"); err != nil {
		t.Fatalf("OpenDM Steve: %v", err)
	}

	tessDM := &DelegateToAgentTool{Fn: rosterAwareDelegateFn("tess-dm", dm.ID, tm, reg)}
	okSteve := tessDM.Execute(context.Background(), map[string]any{"agent": "Steve", "task": "pong"})
	if okSteve.IsError {
		t.Fatalf("desk DM Tess must delegate to desk-peer Steve, got: %s", okSteve.Error)
	}
	if threads := tm.ListBySession("tess-dm"); len(threads) != 1 || threads[0].AgentID != "Steve" {
		t.Fatalf("expected Steve thread in Tess DM, got %+v", threads)
	}

	// Chris is registered but has no desk DM — still denied.
	deniedChris := tessDM.Execute(context.Background(), map[string]any{"agent": "Chris", "task": "nope"})
	if !deniedChris.IsError || !strings.Contains(deniedChris.Error, "DELEGATE_FAIL") {
		t.Fatalf("stranger with no desk DM must DELEGATE_FAIL, got isError=%v err=%q", deniedChris.IsError, deniedChris.Error)
	}

	channel := &DelegateToAgentTool{Fn: rosterAwareDelegateFn("mention-proof", ch.ID, tm, reg)}
	ok := channel.Execute(context.Background(), map[string]any{"agent": "Steve", "task": "hostname"})
	if ok.IsError {
		t.Fatalf("channel member Steve must still delegate, got: %s", ok.Error)
	}
	if threads := tm.ListBySession("mention-proof"); len(threads) != 1 || threads[0].AgentID != "Steve" {
		t.Fatalf("expected Steve thread in channel, got %+v", threads)
	}

	standalone := &DelegateToAgentTool{Fn: rosterAwareDelegateFn("standalone", "", tm, reg)}
	free := standalone.Execute(context.Background(), map[string]any{"agent": "Steve", "task": "hello"})
	if free.IsError {
		t.Fatalf("standalone no-roster must keep all-agents, got: %s", free.Error)
	}
	if threads := tm.ListBySession("standalone"); len(threads) != 1 || threads[0].AgentID != "Steve" {
		t.Fatalf("expected Steve thread standalone, got %+v", threads)
	}
}
