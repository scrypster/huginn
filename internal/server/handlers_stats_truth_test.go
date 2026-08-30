package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/threadmgr"
)

// writeManifestWithUpdatedAt directly overwrites a session's manifest.json
// with a specific UpdatedAt, bypassing SaveManifest's time.Now() stamp so
// tests can simulate old/stale sessions.
func writeManifestWithUpdatedAt(t *testing.T, sessDir string, m session.Manifest, updatedAt time.Time) {
	t.Helper()
	m.UpdatedAt = updatedAt
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	path := filepath.Join(sessDir, m.SessionID, "manifest.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

// TestHandleStats_ActiveSessionsTruth verifies that /api/v1/stats reports
// active_sessions honestly: a session counts as active only when it has a
// run in flight OR activity within the last 15 minutes. Old, quiet sessions
// must not inflate the active count — total_sessions still counts everyone.
func TestHandleStats_ActiveSessionsTruth(t *testing.T) {
	sessDir := t.TempDir()
	store := session.NewStore(sessDir)

	// Recent session: activity 2 minutes ago -> active.
	recent := store.New("recent", "", "")
	if err := store.SaveManifest(recent); err != nil {
		t.Fatalf("save recent: %v", err)
	}
	writeManifestWithUpdatedAt(t, sessDir, recent.Manifest, time.Now().UTC().Add(-2*time.Minute))

	// Stale session: activity 2 hours ago, no run in flight -> not active.
	stale := store.New("stale", "", "")
	if err := store.SaveManifest(stale); err != nil {
		t.Fatalf("save stale: %v", err)
	}
	writeManifestWithUpdatedAt(t, sessDir, stale.Manifest, time.Now().UTC().Add(-2*time.Hour))

	// In-flight session: activity 2 hours ago BUT a thread is running -> active.
	inFlight := store.New("in-flight", "", "")
	if err := store.SaveManifest(inFlight); err != nil {
		t.Fatalf("save in-flight: %v", err)
	}
	writeManifestWithUpdatedAt(t, sessDir, inFlight.Manifest, time.Now().UTC().Add(-2*time.Hour))

	b := &stubBackend{}
	models := modelconfig.DefaultModels()
	orch, err := agent.NewOrchestrator(b, models, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("orch: %v", err)
	}
	cfg := *config.Default()
	srv := New(cfg, orch, store, testToken, t.TempDir(), nil, nil, nil)
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return agents.DefaultAgentsConfig(), nil
	}
	srv.configPath = t.TempDir() + "/config.json"

	tm := threadmgr.New()
	if _, err := tm.Create(threadmgr.CreateParams{
		SessionID: inFlight.ID,
		AgentID:   "agent-a",
		Task:      "still running",
	}); err != nil {
		t.Fatalf("tm.Create: %v", err)
	}
	srv.SetThreadManager(tm)

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/stats", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	total, _ := body["total_sessions"].(float64)
	active, _ := body["active_sessions"].(float64)

	if total != 3 {
		t.Errorf("total_sessions = %v, want 3", body["total_sessions"])
	}
	if active != 2 {
		t.Errorf("active_sessions = %v, want 2 (recent + in-flight, not stale); got body=%v", body["active_sessions"], body)
	}
}
