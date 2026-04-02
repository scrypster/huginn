package server

// Regression tests for GitHub issue #33:
// "Direct message to one Agent is recognized by a different Agent"
//
// Root cause: handleCreateSession stamped SpaceID on the session manifest but
// never set Manifest.Agent, so resolveAgent fell through to the default/first
// agent instead of the space's lead agent.
//
// Fix 1 (handlers.go): stamp Manifest.Agent from the space's LeadAgent during
//   session creation.
// Fix 2 (ws.go): runtime fallback in resolveAgent (step 1b) that heals
//   sessions created before the fix without any DB migration.
// Fix 3 (ws.go): block set_primary_agent in DM spaces (fail-closed).
// Fix 4 (ws.go): channel @mention routing — @Name at start of message routes
//   to that agent if they are a member of the channel (stateless per-message).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// errSpaceStore is a spaces.StoreInterface that always returns an error from GetSpace.
// Used to test fail-closed behavior in the DM guard.
type errSpaceStore struct{}

func (e *errSpaceStore) OpenDM(_ string) (*spaces.Space, error) { return nil, fmt.Errorf("err") }
func (e *errSpaceStore) CreateChannel(_, _ string, _ []string, _, _ string) (*spaces.Space, error) {
	return nil, fmt.Errorf("err")
}
func (e *errSpaceStore) GetSpace(_ string) (*spaces.Space, error) {
	return nil, fmt.Errorf("space store unavailable")
}
func (e *errSpaceStore) ListSpaces(_ spaces.ListOpts) (spaces.ListSpacesResult, error) {
	return spaces.ListSpacesResult{}, fmt.Errorf("err")
}
func (e *errSpaceStore) UpdateSpace(_ string, _ spaces.SpaceUpdates) (*spaces.Space, error) {
	return nil, fmt.Errorf("err")
}
func (e *errSpaceStore) ArchiveSpace(_ string) error                    { return fmt.Errorf("err") }
func (e *errSpaceStore) MarkRead(_ string) error                        { return fmt.Errorf("err") }
func (e *errSpaceStore) UnseenCount(_ string) (int, error)              { return 0, fmt.Errorf("err") }
func (e *errSpaceStore) ListSessionsForSpace(_ string) ([]spaces.SessionRef, error) {
	return nil, fmt.Errorf("err")
}
func (e *errSpaceStore) RemoveAgentFromAllSpaces(_ string) (*spaces.SpaceCascadeResult, error) {
	return nil, fmt.Errorf("err")
}
func (e *errSpaceStore) ListSpaceMessages(_ string, _ *spaces.SpaceMsgCursor, _ int) (spaces.SpaceMessagesResult, error) {
	return spaces.SpaceMessagesResult{}, fmt.Errorf("err")
}
func (e *errSpaceStore) GetChannelsForAgent(_ string) ([]*spaces.Space, error) {
	return nil, fmt.Errorf("err")
}

// openSpaceDB creates a fresh in-memory SQLite DB with both session and space
// migrations applied, suitable for DM-routing regression tests.
func openSpaceDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("openSpaceDB: spaces migration: %v", err)
	}
	return db
}

// TestHandleCreateSession_DM_SetsAgentFromSpaceLeadAgent verifies Fix 1:
// when a session is created with a space_id, the session manifest's Agent
// field is set to the space's LeadAgent so that resolveAgent selects the
// correct agent immediately without falling through to the default.
func TestHandleCreateSession_DM_SetsAgentFromSpaceLeadAgent(t *testing.T) {
	srv, _ := newTestServer(t)
	db := openSpaceDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	srv.SetSpaceStore(store)

	// Create a DM space for agent "Mark".
	dm, err := store.OpenDM("Mark")
	if err != nil {
		t.Fatalf("OpenDM: %v", err)
	}

	// POST /api/v1/sessions with the DM's space_id.
	body, _ := json.Marshal(map[string]string{"space_id": dm.ID})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	srv.handleCreateSession(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	sessID := resp["session_id"]
	if sessID == "" {
		t.Fatal("expected session_id in response")
	}

	// Load the persisted session and verify Agent was stamped.
	loaded, loadErr := srv.store.Load(sessID)
	if loadErr != nil {
		t.Fatalf("store.Load(%q): %v", sessID, loadErr)
	}
	if loaded.Manifest.Agent != "Mark" {
		t.Errorf("expected Manifest.Agent = %q, got %q", "Mark", loaded.Manifest.Agent)
	}
}

// TestHandleCreateSession_WithInvalidSpaceID_SessionStillCreated verifies that
// when space lookup fails (unknown space_id), session creation still succeeds —
// we don't block the user just because we couldn't stamp the agent. The session
// is created without an Agent stamp and will fall through to the default agent.
func TestHandleCreateSession_WithInvalidSpaceID_SessionStillCreated(t *testing.T) {
	srv, _ := newTestServer(t)
	db := openSpaceDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	srv.SetSpaceStore(store)

	body, _ := json.Marshal(map[string]string{"space_id": "nonexistent-space-id"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	srv.handleCreateSession(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even with invalid space_id, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp) //nolint:errcheck
	if resp["session_id"] == "" {
		t.Error("expected a valid session_id even when space lookup fails")
	}
}

// TestResolveAgent_SpaceSession_UsesLeadAgent verifies Fix 2 (the runtime
// fallback, step 1b in resolveAgent): a session that has SpaceID set but no
// PrimaryAgentID resolves to the space's LeadAgent rather than the default.
// This heals pre-fix sessions without any DB migration.
func TestResolveAgent_SpaceSession_UsesLeadAgent(t *testing.T) {
	dir := t.TempDir()
	sessStore := session.NewStore(dir)
	db := openSpaceDB(t)
	spaceStore := spaces.NewSQLiteSpaceStore(db)

	// Create the DM space for "Kimi".
	dm, err := spaceStore.OpenDM("Kimi")
	if err != nil {
		t.Fatalf("OpenDM: %v", err)
	}

	// Create a session with SpaceID set but NO PrimaryAgentID (simulates a
	// pre-fix session or a session where stamp failed silently).
	sess := sessStore.New("test", "/workspace", "model")
	sess.Manifest.SpaceID = dm.ID
	// Explicitly do NOT call SetPrimaryAgent — leave Manifest.Agent empty.
	if err := sessStore.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Two agents: "Mark" (default) and "Kimi".
	cfg := testAgentConfig(
		testAgent("Mark", "model-a", "#fff", "M", true), // is_default
		testAgent("Kimi", "model-b", "#000", "K", false),
	)
	s := &Server{
		store:      sessStore,
		spaceStore: spaceStore,
		agentLoader: func() (*agents.AgentsConfig, error) {
			return cfg, nil
		},
	}

	ag := s.resolveAgent(sess.ID)
	if ag == nil {
		t.Fatal("expected non-nil agent from space lead agent fallback")
	}
	if ag.Name != "Kimi" {
		t.Errorf("expected space lead agent %q, got %q (issue #33 regression)", "Kimi", ag.Name)
	}
}

// TestResolveAgent_SpaceSession_PrefersPrimaryAgentOverLeadAgent verifies that
// when a session has both a PrimaryAgentID and a SpaceID, the PrimaryAgentID
// wins (step 1 before step 1b). This preserves "switch agent" functionality
// for users who explicitly change agents in a space.
func TestResolveAgent_SpaceSession_PrefersPrimaryAgentOverLeadAgent(t *testing.T) {
	dir := t.TempDir()
	sessStore := session.NewStore(dir)
	db := openSpaceDB(t)
	spaceStore := spaces.NewSQLiteSpaceStore(db)

	dm, err := spaceStore.OpenDM("Mark")
	if err != nil {
		t.Fatalf("OpenDM: %v", err)
	}

	// Session is linked to Mark's DM but user explicitly switched to Kimi.
	sess := sessStore.New("test", "/workspace", "model")
	sess.Manifest.SpaceID = dm.ID
	sess.SetPrimaryAgent("Kimi") // user chose a different agent
	if err := sessStore.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	cfg := testAgentConfig(
		testAgent("Mark", "model-a", "#fff", "M", true),
		testAgent("Kimi", "model-b", "#000", "K", false),
	)
	s := &Server{
		store:      sessStore,
		spaceStore: spaceStore,
		agentLoader: func() (*agents.AgentsConfig, error) {
			return cfg, nil
		},
	}

	ag := s.resolveAgent(sess.ID)
	if ag == nil {
		t.Fatal("expected non-nil agent")
	}
	if ag.Name != "Kimi" {
		t.Errorf("expected explicitly-set primary agent %q, got %q", "Kimi", ag.Name)
	}
}

// TestResolveAgent_LegacySpaceSession_AgentNotInConfig_FallsThrough verifies
// that if the space's LeadAgent is not present in the current agent config
// (e.g. the agent was deleted), resolveAgent gracefully falls through to the
// default agent rather than returning nil or panicking.
func TestResolveAgent_LegacySpaceSession_AgentNotInConfig_FallsThrough(t *testing.T) {
	dir := t.TempDir()
	sessStore := session.NewStore(dir)
	db := openSpaceDB(t)
	spaceStore := spaces.NewSQLiteSpaceStore(db)

	dm, err := spaceStore.OpenDM("DeletedAgent")
	if err != nil {
		t.Fatalf("OpenDM: %v", err)
	}

	sess := sessStore.New("test", "/workspace", "model")
	sess.Manifest.SpaceID = dm.ID
	if err := sessStore.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Config no longer contains "DeletedAgent" — it was removed.
	cfg := testAgentConfig(
		testAgent("Chris", "model-c", "#58A6FF", "C", true), // is_default
	)
	s := &Server{
		store:      sessStore,
		spaceStore: spaceStore,
		agentLoader: func() (*agents.AgentsConfig, error) {
			return cfg, nil
		},
	}

	ag := s.resolveAgent(sess.ID)
	if ag == nil {
		t.Fatal("expected fallback to default agent, got nil")
	}
	if ag.Name != "Chris" {
		t.Errorf("expected fallback to default agent %q, got %q", "Chris", ag.Name)
	}
}

// TestResolveAgent_NilSpaceStore_FallsThrough verifies that resolveAgent is
// safe when spaceStore is nil — it skips step 1b and falls through to the
// default agent without panicking. This covers servers running without space
// support configured.
func TestResolveAgent_NilSpaceStore_FallsThrough(t *testing.T) {
	dir := t.TempDir()
	sessStore := session.NewStore(dir)

	// Session has a SpaceID but there's no spaceStore wired up.
	sess := sessStore.New("test", "/workspace", "model")
	sess.Manifest.SpaceID = "some-space-id"
	if err := sessStore.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	cfg := testAgentConfig(
		testAgent("DefaultAgent", "model-d", "#fff", "D", true),
	)
	s := &Server{
		store:      sessStore,
		spaceStore: nil, // no space store
		agentLoader: func() (*agents.AgentsConfig, error) {
			return cfg, nil
		},
	}

	ag := s.resolveAgent(sess.ID)
	if ag == nil {
		t.Fatal("expected fallback to default agent, got nil")
	}
	if ag.Name != "DefaultAgent" {
		t.Errorf("expected default agent %q, got %q", "DefaultAgent", ag.Name)
	}
}

// ── set_primary_agent DM guard ────────────────────────────────────────────────

// TestSetPrimaryAgent_InDMSpace_ReturnsError verifies that set_primary_agent
// is blocked in a DM space — DMs are strictly 1:1 between user and lead agent.
func TestSetPrimaryAgent_InDMSpace_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	sessStore := session.NewStore(dir)
	db := openSpaceDB(t)
	spaceStore := spaces.NewSQLiteSpaceStore(db)

	dm, err := spaceStore.OpenDM("Mark")
	if err != nil {
		t.Fatalf("OpenDM: %v", err)
	}

	sess := sessStore.New("test", "/workspace", "model")
	sess.Manifest.SpaceID = dm.ID
	if err := sessStore.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	hub := newWSHub()
	go hub.run()

	s := &Server{store: sessStore, spaceStore: spaceStore, wsHub: hub}
	c := &wsClient{send: make(chan WSMessage, 4), ctx: context.Background()}

	s.handleWSMessage(c, WSMessage{
		Type:      "set_primary_agent",
		SessionID: sess.ID,
		Payload:   map[string]any{"agent": "Kimi"},
	})

	select {
	case msg := <-c.send:
		if msg.Type != "error" {
			t.Errorf("expected error message type, got %q", msg.Type)
		}
		if msg.Content != "cannot change agent in a DM" {
			t.Errorf("unexpected error content: %q", msg.Content)
		}
	default:
		t.Error("expected an error message to be sent to client, got none")
	}

	// Session manifest must NOT have been changed.
	loaded, loadErr := sessStore.Load(sess.ID)
	if loadErr != nil {
		t.Fatalf("Load: %v", loadErr)
	}
	if loaded.PrimaryAgentID() == "Kimi" {
		t.Error("PrimaryAgentID must not change in a DM space")
	}
}

// TestSetPrimaryAgent_InDMSpace_SpaceLookupFails_ReturnsError verifies the
// fail-closed behavior: if the space cannot be looked up, the switch is blocked
// and an error is returned rather than silently allowing the change.
func TestSetPrimaryAgent_InDMSpace_SpaceLookupFails_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	sessStore := session.NewStore(dir)

	sess := sessStore.New("test", "/workspace", "model")
	sess.Manifest.SpaceID = "some-space-id"
	if err := sessStore.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	hub := newWSHub()
	go hub.run()

	s := &Server{store: sessStore, spaceStore: &errSpaceStore{}, wsHub: hub}
	c := &wsClient{send: make(chan WSMessage, 4), ctx: context.Background()}

	s.handleWSMessage(c, WSMessage{
		Type:      "set_primary_agent",
		SessionID: sess.ID,
		Payload:   map[string]any{"agent": "Kimi"},
	})

	select {
	case msg := <-c.send:
		if msg.Type != "error" {
			t.Errorf("expected error message type, got %q", msg.Type)
		}
	default:
		t.Error("expected an error message on space lookup failure, got none")
	}

	// Agent must NOT have been persisted.
	loaded, loadErr := sessStore.Load(sess.ID)
	if loadErr != nil {
		t.Fatalf("Load: %v", loadErr)
	}
	if loaded.PrimaryAgentID() == "Kimi" {
		t.Error("PrimaryAgentID must not change when space lookup fails (fail-closed)")
	}
}

// ── extractLeadMention unit tests ─────────────────────────────────────────────

func TestExtractLeadMention_ValidMention(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"@Mark help me", "Mark"},
		{"@Kimi", "Kimi"},
		{"  @Atlas-2 what's up?", "Atlas-2"},
		{"@agent_v2 go", "agent_v2"},
	}
	for _, tc := range cases {
		got := extractLeadMention(tc.input)
		if got != tc.want {
			t.Errorf("extractLeadMention(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestExtractLeadMention_InvalidChars_ReturnsEmpty(t *testing.T) {
	cases := []struct {
		input string
		desc  string
	}{
		{"hello @Mark", "mention not at start"},
		{"@", "bare @"},
		{"@123bad", "starts with digit"},
		{"@!hack", "starts with punctuation"},
		{"no mention here", "no @"},
		{"@" + string(make([]byte, 65)), "name over 64 chars"},
	}
	for _, tc := range cases {
		got := extractLeadMention(tc.input)
		if got != "" {
			t.Errorf("extractLeadMention(%q) [%s] = %q, want empty", tc.input, tc.desc, got)
		}
	}
}

// ── Channel @mention routing ──────────────────────────────────────────────────

// TestResolveAgentForMessage_ChannelMention_RoutesToMember verifies that a
// message starting with @Name in a channel space routes to the named agent
// when they are a member of the channel.
func TestResolveAgentForMessage_ChannelMention_RoutesToMember(t *testing.T) {
	dir := t.TempDir()
	sessStore := session.NewStore(dir)
	db := openSpaceDB(t)
	spaceStore := spaces.NewSQLiteSpaceStore(db)

	// Channel with Mark as lead, Kimi as member.
	ch, err := spaceStore.CreateChannel("Engineering", "Mark", []string{"Kimi"}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	sess := sessStore.New("test", "/workspace", "model")
	sess.Manifest.SpaceID = ch.ID
	if err := sessStore.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	cfg := testAgentConfig(
		testAgent("Mark", "model-a", "#fff", "M", true),
		testAgent("Kimi", "model-b", "#000", "K", false),
	)
	s := &Server{
		store:      sessStore,
		spaceStore: spaceStore,
		agentLoader: func() (*agents.AgentsConfig, error) { return cfg, nil },
	}

	ag := s.resolveAgentForMessage(sess.ID, "@Kimi can you review this?")
	if ag == nil {
		t.Fatal("expected non-nil agent from @mention routing")
	}
	if ag.Name != "Kimi" {
		t.Errorf("expected @mention to route to %q, got %q", "Kimi", ag.Name)
	}
}

// TestResolveAgentForMessage_ChannelMention_AgentNotInSpace_FallsThrough
// verifies that @mentioning an agent who is NOT a member of the channel falls
// through to the lead agent rather than routing to the named agent.
func TestResolveAgentForMessage_ChannelMention_AgentNotInSpace_FallsThrough(t *testing.T) {
	dir := t.TempDir()
	sessStore := session.NewStore(dir)
	db := openSpaceDB(t)
	spaceStore := spaces.NewSQLiteSpaceStore(db)

	// Channel with only Mark — Kimi is NOT a member.
	ch, err := spaceStore.CreateChannel("Engineering", "Mark", []string{}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	sess := sessStore.New("test", "/workspace", "model")
	sess.Manifest.SpaceID = ch.ID
	if err := sessStore.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	cfg := testAgentConfig(
		testAgent("Mark", "model-a", "#fff", "M", true),
		testAgent("Kimi", "model-b", "#000", "K", false),
	)
	s := &Server{
		store:      sessStore,
		spaceStore: spaceStore,
		agentLoader: func() (*agents.AgentsConfig, error) { return cfg, nil },
	}

	// @Kimi is not a channel member — should fall through to lead agent Mark.
	ag := s.resolveAgentForMessage(sess.ID, "@Kimi are you there?")
	if ag == nil {
		t.Fatal("expected fallback to lead agent, got nil")
	}
	if ag.Name != "Mark" {
		t.Errorf("expected fallback to lead agent %q, got %q", "Mark", ag.Name)
	}
}

// TestResolveAgentForMessage_DMMention_IgnoredUsesLeadAgent verifies that
// @mention routing is ignored in DM spaces — the lead agent always handles
// the message regardless of any @mention in the content.
func TestResolveAgentForMessage_DMMention_IgnoredUsesLeadAgent(t *testing.T) {
	dir := t.TempDir()
	sessStore := session.NewStore(dir)
	db := openSpaceDB(t)
	spaceStore := spaces.NewSQLiteSpaceStore(db)

	dm, err := spaceStore.OpenDM("Mark")
	if err != nil {
		t.Fatalf("OpenDM: %v", err)
	}

	sess := sessStore.New("test", "/workspace", "model")
	sess.Manifest.SpaceID = dm.ID
	if err := sessStore.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	cfg := testAgentConfig(
		testAgent("Mark", "model-a", "#fff", "M", true),
		testAgent("Kimi", "model-b", "#000", "K", false),
	)
	s := &Server{
		store:      sessStore,
		spaceStore: spaceStore,
		agentLoader: func() (*agents.AgentsConfig, error) { return cfg, nil },
	}

	// Even though the message starts with @Kimi, it's a DM — Mark must respond.
	ag := s.resolveAgentForMessage(sess.ID, "@Kimi is anyone there?")
	if ag == nil {
		t.Fatal("expected non-nil agent, got nil")
	}
	if ag.Name != "Mark" {
		t.Errorf("expected DM lead agent %q to be used (ignoring @Kimi mention), got %q", "Mark", ag.Name)
	}
}
