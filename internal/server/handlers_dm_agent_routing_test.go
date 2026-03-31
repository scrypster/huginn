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

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

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
