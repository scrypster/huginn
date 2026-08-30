package server

// Integration tests for the space context injection pipeline.
//
// These tests verify the critical wiring that makes agents aware of their
// team members in channel contexts. Specifically:
//
// 1. InjectSpaceContext builds a proper Team Context block for channel sessions
// 2. InjectSpaceContext is a no-op for DM sessions and non-space sessions
// 3. ResolveAgentForSpace prefers the space's lead agent over fallbacks
// 4. Agent descriptions from config are included in the space context
// 5. Channel-recent messages are injected into the context
// 6. BuildSpaceContextBlock produces correct output for lead vs member agents

import (
	"context"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/workforce"
)

// makeSessionStore creates a filesystem-backed session store in a temp dir.
func makeSessionStore(t *testing.T) session.StoreInterface {
	t.Helper()
	return session.NewStore(t.TempDir())
}

// ── InjectSpaceContext tests ────────────────────────────────────────────────

func TestInjectSpaceContext_ChannelSession_InjectsTeamContext(t *testing.T) {
	srv, _ := newTestServer(t)
	db := openSpaceDB(t)
	spaceStore := spaces.NewSQLiteSpaceStore(db)
	sessStore := makeSessionStore(t)
	srv.SetSpaceStore(spaceStore)
	srv.store = sessStore

	// Create a channel with lead + members.
	ch, err := spaceStore.CreateChannel("Engineering", "Tom", []string{"Sam", "Dave"}, "", "")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	// Create a session assigned to that channel.
	sess := sessStore.New("test", "/workspace", "model")
	sess.Manifest.SpaceID = ch.ID
	sess.Manifest.Agent = "Tom"
	if err := sessStore.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	// Wire agent loader with descriptions.
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{
			Agents: []agents.AgentDef{
				{Name: "Tom", Description: "Team lead. Routes tasks and synthesizes results."},
				{Name: "Sam", Description: "Backend engineer. Writes Go code and reviews PRs."},
				{Name: "Dave", Description: "DevOps specialist. Manages deployments and CI/CD."},
			},
		}, nil
	}

	ag := &agents.Agent{Name: "Tom"}
	ctx := context.Background()
	enrichedCtx := srv.InjectSpaceContext(ctx, sess.ID, ag)

	// Verify space context was injected.
	spaceCtx := workforce.GetSpaceContext(enrichedCtx)
	if spaceCtx == "" {
		t.Fatal("expected space context to be injected, got empty string")
	}

	// Verify it contains Team Context header.
	if !strings.Contains(spaceCtx, "[Team Context]") {
		t.Errorf("expected [Team Context] in space context, got:\n%s", spaceCtx)
	}

	// Verify it identifies Tom as lead agent.
	if !strings.Contains(spaceCtx, "You are Tom, the lead agent") {
		t.Errorf("expected lead agent identification, got:\n%s", spaceCtx)
	}

	// Verify team member descriptions are included.
	if !strings.Contains(spaceCtx, "Sam") || !strings.Contains(spaceCtx, "Backend engineer") {
		t.Errorf("expected Sam's description in space context, got:\n%s", spaceCtx)
	}
	if !strings.Contains(spaceCtx, "Dave") || !strings.Contains(spaceCtx, "DevOps specialist") {
		t.Errorf("expected Dave's description in space context, got:\n%s", spaceCtx)
	}

	// Verify delegation protocol instructions.
	if !strings.Contains(spaceCtx, "Delegation protocol") {
		t.Errorf("expected delegation protocol instructions, got:\n%s", spaceCtx)
	}

	gotNames := workforce.GetChannelMembers(enrichedCtx)
	joined := strings.Join(gotNames, ",")
	for _, want := range []string{"Tom", "Sam", "Dave"} {
		found := false
		for _, n := range gotNames {
			if strings.EqualFold(n, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("channel members missing %s: %s", want, joined)
		}
	}
}

func TestInjectSpaceContext_AttachesCompanyRosters(t *testing.T) {
	srv, _ := newTestServer(t)
	db := openSpaceDB(t)
	spaceStore := spaces.NewSQLiteSpaceStore(db)
	sessStore := makeSessionStore(t)
	srv.SetSpaceStore(spaceStore)
	srv.store = sessStore

	if _, err := spaceStore.CreateCompany("Lab", "", []string{"Sam", "Winston"}, "", ""); err != nil {
		t.Fatalf("create Lab: %v", err)
	}
	ch, err := spaceStore.CreateChannel("Huginn", "Winston", []string{"Reggie", "Steve"}, "", "")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	sess := sessStore.New("test-lab-roster", "/workspace", "model")
	sess.Manifest.SpaceID = ch.ID
	sess.Manifest.Agent = "Winston"
	if err := sessStore.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	ag := &agents.Agent{Name: "Winston"}
	enriched := srv.InjectSpaceContext(context.Background(), sess.ID, ag)
	got := workforce.GetCompanyRoster(enriched, "Lab")
	joined := strings.Join(got, ",")
	for _, want := range []string{"Sam", "Winston"} {
		found := false
		for _, n := range got {
			if strings.EqualFold(n, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Lab roster missing %s: %s", want, joined)
		}
	}
}

func TestInjectSpaceContext_DMSession_NoTeamContext(t *testing.T) {
	srv, _ := newTestServer(t)
	db := openSpaceDB(t)
	spaceStore := spaces.NewSQLiteSpaceStore(db)
	sessStore := makeSessionStore(t)
	srv.SetSpaceStore(spaceStore)
	srv.store = sessStore

	// Create a DM space.
	dm, err := spaceStore.OpenDM("Sam")
	if err != nil {
		t.Fatalf("open DM: %v", err)
	}

	sess := sessStore.New("test-dm", "/workspace", "model")
	sess.Manifest.SpaceID = dm.ID
	sess.Manifest.Agent = "Sam"
	if err := sessStore.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	ag := &agents.Agent{Name: "Sam"}
	ctx := context.Background()
	enrichedCtx := srv.InjectSpaceContext(ctx, sess.ID, ag)

	// DMs should NOT get team context (they're 1:1).
	spaceCtx := workforce.GetSpaceContext(enrichedCtx)
	if spaceCtx != "" {
		t.Errorf("expected empty space context for DM, got:\n%s", spaceCtx)
	}
}

func TestInjectSpaceContext_NoSpaceSession_Noop(t *testing.T) {
	srv, _ := newTestServer(t)
	sessStore := makeSessionStore(t)
	srv.store = sessStore

	// Session with no SpaceID.
	sess := sessStore.New("plain", "/workspace", "model")
	if err := sessStore.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	ctx := context.Background()
	enrichedCtx := srv.InjectSpaceContext(ctx, sess.ID, nil)

	spaceCtx := workforce.GetSpaceContext(enrichedCtx)
	if spaceCtx != "" {
		t.Errorf("expected empty space context for non-space session, got:\n%s", spaceCtx)
	}
}

func TestInjectSpaceContext_NilSpaceStore_Noop(t *testing.T) {
	srv, _ := newTestServer(t)
	// Don't set space store.
	ctx := context.Background()
	enrichedCtx := srv.InjectSpaceContext(ctx, "sess-1", nil)

	spaceCtx := workforce.GetSpaceContext(enrichedCtx)
	if spaceCtx != "" {
		t.Errorf("expected empty space context when spaceStore is nil")
	}
}

func TestInjectSpaceContext_MemberAgent_GetsNonLeadContext(t *testing.T) {
	srv, _ := newTestServer(t)
	db := openSpaceDB(t)
	spaceStore := spaces.NewSQLiteSpaceStore(db)
	sessStore := makeSessionStore(t)
	srv.SetSpaceStore(spaceStore)
	srv.store = sessStore

	ch, err := spaceStore.CreateChannel("Engineering", "Tom", []string{"Sam", "Dave"}, "", "")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	sess := sessStore.New("test-member", "/workspace", "model")
	sess.Manifest.SpaceID = ch.ID
	if err := sessStore.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{
			Agents: []agents.AgentDef{
				{Name: "Tom", Description: "Lead"},
				{Name: "Sam", Description: "Backend"},
				{Name: "Dave", Description: "DevOps"},
			},
		}, nil
	}

	// When Sam is the active agent, context should NOT say "You are the lead agent".
	ag := &agents.Agent{Name: "Sam"}
	ctx := context.Background()
	enrichedCtx := srv.InjectSpaceContext(ctx, sess.ID, ag)

	spaceCtx := workforce.GetSpaceContext(enrichedCtx)
	if strings.Contains(spaceCtx, "You are Sam, the lead agent") {
		t.Errorf("Sam should NOT be identified as lead agent, got:\n%s", spaceCtx)
	}
	if !strings.Contains(spaceCtx, "Lead Agent") || !strings.Contains(spaceCtx, "Tom") {
		t.Errorf("expected Tom identified as lead agent in Sam's context, got:\n%s", spaceCtx)
	}
}

func TestInjectSpaceContext_MissingDescriptions_FallsBackToSpecialist(t *testing.T) {
	srv, _ := newTestServer(t)
	db := openSpaceDB(t)
	spaceStore := spaces.NewSQLiteSpaceStore(db)
	sessStore := makeSessionStore(t)
	srv.SetSpaceStore(spaceStore)
	srv.store = sessStore

	ch, err := spaceStore.CreateChannel("Engineering", "Tom", []string{"Sam"}, "", "")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	sess := sessStore.New("test-nondesc", "/workspace", "model")
	sess.Manifest.SpaceID = ch.ID
	if err := sessStore.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	// No descriptions configured — agents have only a name.
	// BuildCapabilityCard still produces a minimal card (name + memory mode),
	// so "specialist agent" fallback no longer appears; instead each agent is
	// listed by name with at least their memory mode.
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{
			Agents: []agents.AgentDef{
				{Name: "Tom"},
				{Name: "Sam"},
			},
		}, nil
	}

	ag := &agents.Agent{Name: "Tom"}
	ctx := context.Background()
	enrichedCtx := srv.InjectSpaceContext(ctx, sess.ID, ag)

	spaceCtx := workforce.GetSpaceContext(enrichedCtx)
	if !strings.Contains(spaceCtx, "Tom") || !strings.Contains(spaceCtx, "Sam") {
		t.Errorf("expected both agents to appear in space context, got:\n%s", spaceCtx)
	}
	// Minimal capability card always includes memory mode even with no description.
	if !strings.Contains(spaceCtx, "Memory:") {
		t.Errorf("expected capability card memory mode annotation in space context, got:\n%s", spaceCtx)
	}
}

// ── ResolveAgentForSpace tests ──────────────────────────────────────────────

func TestResolveAgentForSpace_UsesLeadAgent(t *testing.T) {
	srv, _ := newTestServer(t)
	db := openSpaceDB(t)
	spaceStore := spaces.NewSQLiteSpaceStore(db)
	sessStore := makeSessionStore(t)
	srv.SetSpaceStore(spaceStore)
	srv.store = sessStore

	ch, err := spaceStore.CreateChannel("Eng", "Tom", []string{"Sam"}, "", "")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{
			Agents: []agents.AgentDef{
				{Name: "Alice", IsDefault: true},
				{Name: "Tom"},
				{Name: "Sam"},
			},
		}, nil
	}

	ag := srv.ResolveAgentForSpace("any-session", ch.ID)
	if ag == nil {
		t.Fatal("expected agent, got nil")
	}
	if ag.Name != "Tom" {
		t.Errorf("expected lead agent Tom, got %q", ag.Name)
	}
}

func TestResolveAgentForSpace_FallsBackWhenNoSpaceStore(t *testing.T) {
	srv, _ := newTestServer(t)
	// No space store configured.
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{
			Agents: []agents.AgentDef{
				{Name: "Alice", IsDefault: true},
			},
		}, nil
	}
	ag := srv.ResolveAgentForSpace("nonexistent", "nonexistent-space")
	// Should fall through to resolveAgent which returns Alice (default).
	if ag == nil {
		t.Fatal("expected fallback agent, got nil")
	}
	if ag.Name != "Alice" {
		t.Errorf("expected fallback to Alice, got %q", ag.Name)
	}
}

// ── BuildSpaceContextBlock tests ────────────────────────────────────────────

func TestBuildSpaceContextBlock_LeadAgent_ContainsAllElements(t *testing.T) {
	// Descriptions use capability-card format (as produced by BuildCapabilityCard in ws.go).
	members := []agent.SpaceMember{
		{Name: "Sam", Description: "- Sam [capable, tools: yes]\n  Role: Backend engineer\n  Memory: conversational\n"},
		{Name: "Dave", Description: "- Dave [capable, tools: yes]\n  Role: DevOps specialist\n  Memory: conversational\n"},
	}
	result := agent.BuildSpaceContextBlock("Engineering", "channel", "Tom", "Tom", members)

	checks := []struct {
		name    string
		content string
	}{
		{"Team Context header", "[Team Context]"},
		{"Lead agent identity", "You are Tom, the lead agent"},
		{"Channel name", "Engineering"},
		{"Delegation protocol", "Delegation protocol"},
		{"Delegation tool instruction", "delegate_to_agent"},
		{"GOOD example with Sam", "GOOD: Call delegate_to_agent with agent=\"Sam\""},
		{"GOOD example with Dave", "GOOD: Call delegate_to_agent with agent=\"Dave\""},
		{"Sam listed", "Sam"},
		{"Sam description", "Backend engineer"},
		{"Dave listed", "Dave"},
		{"Dave description", "DevOps specialist"},
		{"Main channel discipline", "Main channel discipline"},
		{"User @mention is a real address", "user @mention addresses that agent"},
	}

	for _, check := range checks {
		if !strings.Contains(result, check.content) {
			t.Errorf("%s: expected %q in result:\n%s", check.name, check.content, result)
		}
	}
}

func TestBuildSpaceContextBlock_MemberAgent_NoLeadIdentity(t *testing.T) {
	members := []agent.SpaceMember{
		{Name: "Tom", Description: "Lead"},
		{Name: "Sam", Description: "Backend"},
	}
	result := agent.BuildSpaceContextBlock("Engineering", "channel", "Sam", "Tom", members)

	if strings.Contains(result, "You are Sam") {
		t.Errorf("member agent should NOT get 'You are Sam' identity")
	}
	if !strings.Contains(result, "Lead Agent") {
		t.Errorf("member agent should see Lead Agent label")
	}
}

func TestBuildSpaceContextBlock_DM_ReturnsEmpty(t *testing.T) {
	members := []agent.SpaceMember{{Name: "Sam"}}
	result := agent.BuildSpaceContextBlock("Sam", "dm", "Sam", "Sam", members)
	if result != "" {
		t.Errorf("expected empty for DM, got:\n%s", result)
	}
}

func TestBuildSpaceContextBlock_NoMembers_ReturnsEmpty(t *testing.T) {
	result := agent.BuildSpaceContextBlock("Eng", "channel", "Tom", "Tom", nil)
	if result != "" {
		t.Errorf("expected empty for no members, got:\n%s", result)
	}
}

// ── Space membership tests ──────────────────────────────────────────────────

func TestSpaceMembers_IncludesLeadAgent(t *testing.T) {
	db := openSpaceDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	ch, err := store.CreateChannel("Eng", "Tom", []string{"Sam", "Dave"}, "", "")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	members, err := store.SpaceMembers(ch.ID)
	if err != nil {
		t.Fatalf("SpaceMembers: %v", err)
	}

	found := false
	for _, m := range members {
		if m == "Tom" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected lead agent 'Tom' in members, got %v", members)
	}
}

func TestSpaceMembers_ArchivedSpace_ReturnsNil(t *testing.T) {
	db := openSpaceDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	ch, _ := store.CreateChannel("Eng", "Tom", []string{"Sam"}, "", "")
	_ = store.ArchiveSpace(ch.ID)

	members, err := store.SpaceMembers(ch.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if members != nil {
		t.Errorf("expected nil members for archived space, got %v", members)
	}
}

func TestSpaceMembers_DMSpace_IncludesLeadAgent(t *testing.T) {
	db := openSpaceDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	dm, _ := store.OpenDM("Sam")
	members, err := store.SpaceMembers(dm.ID)
	if err != nil {
		t.Fatalf("SpaceMembers: %v", err)
	}

	if len(members) != 1 || members[0] != "Sam" {
		t.Errorf("expected [Sam] for DM, got %v", members)
	}
}

// ── Agent routing tests ─────────────────────────────────────────────────────

func TestResolveAgentForMessage_ChannelLeadAgent(t *testing.T) {
	srv, _ := newTestServer(t)
	db := openSpaceDB(t)
	spaceStore := spaces.NewSQLiteSpaceStore(db)
	sessStore := makeSessionStore(t)
	srv.SetSpaceStore(spaceStore)
	srv.store = sessStore

	ch, _ := spaceStore.CreateChannel("Eng", "Tom", []string{"Sam", "Dave"}, "", "")

	sess := sessStore.New("test-route", "/workspace", "model")
	sess.Manifest.SpaceID = ch.ID
	// Intentionally DO NOT set Agent — test that lead agent resolution works.
	_ = sessStore.SaveManifest(sess)

	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{
			Agents: []agents.AgentDef{
				{Name: "Alice", IsDefault: true},
				{Name: "Tom"},
				{Name: "Sam"},
				{Name: "Dave"},
			},
		}, nil
	}

	ag := srv.resolveAgentForMessage(sess.ID, "what's the deployment status?")
	if ag == nil {
		t.Fatal("expected agent, got nil")
	}
	if ag.Name != "Tom" {
		t.Errorf("expected lead agent Tom for channel message, got %q", ag.Name)
	}
}

func TestResolveAgentForMessage_ChannelAtMention(t *testing.T) {
	srv, _ := newTestServer(t)
	db := openSpaceDB(t)
	spaceStore := spaces.NewSQLiteSpaceStore(db)
	sessStore := makeSessionStore(t)
	srv.SetSpaceStore(spaceStore)
	srv.store = sessStore

	ch, _ := spaceStore.CreateChannel("Eng", "Tom", []string{"Sam", "Dave"}, "", "")

	sess := sessStore.New("test-mention", "/workspace", "model")
	sess.Manifest.SpaceID = ch.ID
	// Stamp the lead the way handleCreateSession does. @Sam must still win.
	sess.Manifest.Agent = "Tom"
	_ = sessStore.SaveManifest(sess)

	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{
			Agents: []agents.AgentDef{
				{Name: "Tom"},
				{Name: "Sam"},
				{Name: "Dave"},
			},
		}, nil
	}

	// @Sam mention should route to Sam, not Tom.
	ag := srv.resolveAgentForMessage(sess.ID, "@Sam can you review this PR?")
	if ag == nil {
		t.Fatal("expected agent, got nil")
	}
	if ag.Name != "Sam" {
		t.Errorf("expected @mention to route to Sam, got %q", ag.Name)
	}
}

func TestResolveAgentForMessage_ChannelAtMention_NonMember_FallsToLead(t *testing.T) {
	srv, _ := newTestServer(t)
	db := openSpaceDB(t)
	spaceStore := spaces.NewSQLiteSpaceStore(db)
	sessStore := makeSessionStore(t)
	srv.SetSpaceStore(spaceStore)
	srv.store = sessStore

	ch, _ := spaceStore.CreateChannel("Eng", "Tom", []string{"Sam"}, "", "")

	sess := sessStore.New("test-nonmember", "/workspace", "model")
	sess.Manifest.SpaceID = ch.ID
	sess.Manifest.Agent = "Tom"
	_ = sessStore.SaveManifest(sess)

	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{
			Agents: []agents.AgentDef{
				{Name: "Tom"},
				{Name: "Sam"},
				{Name: "Dave"}, // Dave is in config but NOT a channel member.
			},
		}, nil
	}

	// @Dave is a known agent but not a channel member — stay with lead Tom.
	ag := srv.resolveAgentForMessage(sess.ID, "@Dave deploy to staging")
	if ag == nil {
		t.Fatal("expected agent, got nil")
	}
	if ag.Name != "Tom" {
		t.Errorf("expected fallback to lead agent Tom for non-member mention, got %q", ag.Name)
	}

	ag = srv.resolveAgentForMessage(sess.ID, "please ask @Dave about hostname")
	if ag == nil {
		t.Fatal("expected agent for mid-text non-member, got nil")
	}
	if ag.Name != "Tom" {
		t.Errorf("expected mid-text @Dave in a Tom/Sam channel to stay with Tom, got %q", ag.Name)
	}
}

func TestResolveAgentForMessage_DM_AlwaysLeadAgent(t *testing.T) {
	srv, _ := newTestServer(t)
	db := openSpaceDB(t)
	spaceStore := spaces.NewSQLiteSpaceStore(db)
	sessStore := makeSessionStore(t)
	srv.SetSpaceStore(spaceStore)
	srv.store = sessStore

	dm, _ := spaceStore.OpenDM("Sam")

	sess := sessStore.New("test-dm-route", "/workspace", "model")
	sess.Manifest.SpaceID = dm.ID
	sess.Manifest.Agent = "Sam"
	_ = sessStore.SaveManifest(sess)

	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{
			Agents: []agents.AgentDef{
				{Name: "Tom", IsDefault: true},
				{Name: "Sam"},
			},
		}, nil
	}

	ag := srv.resolveAgentForMessage(sess.ID, "hello!")
	if ag == nil {
		t.Fatal("expected agent, got nil")
	}
	if ag.Name != "Sam" {
		t.Errorf("expected DM lead agent Sam, got %q", ag.Name)
	}
}

// ── extractLeadMention tests ────────────────────────────────────────────────

func TestExtractLeadMention(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"@Sam can you review?", "Sam"},
		{"@Dave-ops deploy to staging", "Dave-ops"},
		{"@Tom_lead what's the plan?", "Tom_lead"},
		{"hello @Sam", "Sam"},            // first @ anywhere
		{"@", ""},                        // bare @
		{"@123invalid", ""},              // starts with digit
		{"no mention here", ""},          // no @
		{"", ""},                         // empty
		{"  @Sam leading spaces", "Sam"}, // trimmed
		{"alice@Bob", ""},                // email-style, not a mention
	}

	for _, tt := range tests {
		got := extractLeadMention(tt.input)
		if got != tt.expected {
			t.Errorf("extractLeadMention(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestInjectSpaceContext_DeskDM_ListsDeskPeers(t *testing.T) {
	srv, _ := newTestServer(t)
	db := openSpaceDB(t)
	spaceStore := spaces.NewSQLiteSpaceStore(db)
	sessStore := makeSessionStore(t)
	srv.SetSpaceStore(spaceStore)
	srv.store = sessStore

	steveDM, err := spaceStore.OpenDM("Steve")
	if err != nil {
		t.Fatalf("OpenDM Steve: %v", err)
	}
	if _, err := spaceStore.OpenDM("Winston"); err != nil {
		t.Fatalf("OpenDM Winston: %v", err)
	}

	sess := sessStore.New("steve-dm", "/workspace", "model")
	sess.Manifest.SpaceID = steveDM.ID
	sess.Manifest.Agent = "Steve"
	if err := sessStore.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	ag := &agents.Agent{Name: "Steve"}
	enriched := srv.InjectSpaceContext(context.Background(), sess.ID, ag)
	spaceCtx := workforce.GetSpaceContext(enriched)
	if !strings.Contains(spaceCtx, "[Desk Floor]") {
		t.Fatalf("expected desk floor context, got:\n%s", spaceCtx)
	}
	if !strings.Contains(spaceCtx, "Winston") {
		t.Fatalf("expected Winston in desk floor context, got:\n%s", spaceCtx)
	}
	if !strings.Contains(spaceCtx, "delegate_to_agent") {
		t.Fatalf("expected delegate_to_agent in desk floor context, got:\n%s", spaceCtx)
	}
}

func TestInjectSpaceContext_CapabilityCardsIncludeTier(t *testing.T) {
	srv, _ := newTestServer(t)
	db := openSpaceDB(t)
	spaceStore := spaces.NewSQLiteSpaceStore(db)
	sessStore := makeSessionStore(t)
	srv.SetSpaceStore(spaceStore)
	srv.store = sessStore

	ch, err := spaceStore.CreateChannel("Engineering", "Tom", []string{"Sam"}, "", "")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	sess := sessStore.New("tier-cards", "/workspace", "model")
	sess.Manifest.SpaceID = ch.ID
	sess.Manifest.Agent = "Tom"
	if err := sessStore.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{
			Agents: []agents.AgentDef{
				{Name: "Tom", Description: "Team lead.", Model: "claude-sonnet-4"},
				{Name: "Sam", Description: "Backend engineer.", Model: "qwen2.5-coder:7b"},
			},
		}, nil
	}

	ag := &agents.Agent{Name: "Tom", ModelID: "claude-sonnet-4"}
	spaceCtx := workforce.GetSpaceContext(srv.InjectSpaceContext(context.Background(), sess.ID, ag))
	if spaceCtx == "" {
		t.Fatal("expected space context")
	}
	if !strings.Contains(spaceCtx, "tools:") {
		t.Fatalf("expected infoFn tools annotation on capability cards, got:\n%s", spaceCtx)
	}
	if !strings.Contains(spaceCtx, "low") {
		t.Fatalf("expected 7b Sam card to show low tier, got:\n%s", spaceCtx)
	}
}

func TestInjectSpaceContext_ChannelNamesCompany(t *testing.T) {
	srv, _ := newTestServer(t)
	db := openSpaceDB(t)
	spaceStore := spaces.NewSQLiteSpaceStore(db)
	sessStore := makeSessionStore(t)
	srv.SetSpaceStore(spaceStore)
	srv.store = sessStore

	co, err := spaceStore.CreateCompany("Huginn", "", []string{"Winston"}, "", "")
	if err != nil {
		t.Fatalf("create company: %v", err)
	}
	ch, err := spaceStore.CreateChannelForCompany("Huginn", "Winston", []string{"Steve"}, "", "", co.ID)
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	sess := sessStore.New("test", "/workspace", "model")
	sess.Manifest.SpaceID = ch.ID
	sess.Manifest.Agent = "Winston"
	if err := sessStore.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{{Name: "Winston"}, {Name: "Steve"}}}, nil
	}
	ctx := srv.InjectSpaceContext(context.Background(), sess.ID, &agents.Agent{Name: "Winston"})
	spaceCtx := workforce.GetSpaceContext(ctx)
	if !strings.Contains(spaceCtx, "**Company:** Huginn") {
		t.Fatalf("expected company name in space context, got:\n%s", spaceCtx)
	}
	if !strings.Contains(spaceCtx, "This channel is in the Huginn company") {
		t.Fatalf("expected company sentence, got:\n%s", spaceCtx)
	}
}
