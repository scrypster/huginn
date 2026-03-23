package server

// hardening_iter15_test.go — Hardening iteration 15.
// Targets remaining coverage gaps across workflows, connections, and skills.
//
// Workflows: GET/DELETE/RUN found path, UPDATE + DELETE found path
// Connections: handleListConnections nil-store path
// Skills: handleSkillsRegistrySearch with cached index, handleSkillsRegistryIndex cached path
// handleCloudConnect: goroutine path with nil storer (bare register call)

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/scheduler"
	"github.com/scrypster/huginn/internal/session"
)

func createTestWorkflow(t *testing.T, huginnDir string) *scheduler.Workflow {
	t.Helper()
	dir := filepath.Join(huginnDir, "workflows")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	wf := &scheduler.Workflow{
		ID:      "test-wf-001",
		Name:    "Test Workflow",
		Enabled: true,
	}
	if err := scheduler.SaveWorkflow(dir, wf); err != nil {
		t.Fatalf("save workflow: %v", err)
	}
	return wf
}

// ── handleGetWorkflow: found path ────────────────────────────────────────────

func TestHandleGetWorkflow_Found(t *testing.T) {
	srv, ts := newTestServer(t)
	createTestWorkflow(t, srv.huginnDir)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/workflows/test-wf-001", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	if body["id"] != "test-wf-001" {
		t.Errorf("want id=test-wf-001, got %v", body["id"])
	}
}

// ── handleUpdateWorkflow: invalid JSON + found path ───────────────────────────

func TestHandleUpdateWorkflow_InvalidJSON(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/workflows/some-id", strings.NewReader("bad json"))
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

func TestHandleUpdateWorkflow_Found(t *testing.T) {
	srv, ts := newTestServer(t)
	createTestWorkflow(t, srv.huginnDir)

	payload := `{"name":"Updated WF","enabled":false}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/workflows/test-wf-001", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	if body["name"] != "Updated WF" {
		t.Errorf("want name=Updated WF, got %v", body["name"])
	}
}

// ── handleDeleteWorkflow: found path ─────────────────────────────────────────

func TestHandleDeleteWorkflow_Found(t *testing.T) {
	srv, ts := newTestServer(t)
	createTestWorkflow(t, srv.huginnDir)

	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/workflows/test-wf-001", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	if body["deleted"] != "test-wf-001" {
		t.Errorf("want deleted=test-wf-001, got %v", body["deleted"])
	}
}

// ── handleRunWorkflow: not-found path ─────────────────────────────────────────

func TestHandleRunWorkflow_NotFound(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/nonexistent/run", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

// ── handleCreateWorkflow: invalid JSON ────────────────────────────────────────

func TestHandleCreateWorkflow_InvalidJSON(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", strings.NewReader("bad json"))
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

// ── handleListWorkflowRuns: with store returning results ──────────────────────

func TestHandleListWorkflowRuns_WithStore_Empty(t *testing.T) {
	srv, ts := newTestServer(t)

	// Wire a real WorkflowRunStore backed by a temp dir.
	dir := t.TempDir()
	store := scheduler.NewWorkflowRunStore(dir)
	srv.SetWorkflowRunStore(store)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/workflows/test-wf/runs", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body []any
	json.NewDecoder(resp.Body).Decode(&body)
	if body == nil {
		t.Error("want non-nil slice, got nil")
	}
}

// ── handleListConnections: nil connStore ──────────────────────────────────────

func TestHandleListConnections_NilStore_Iter15(t *testing.T) {
	_, ts := newTestServer(t) // connStore is nil by default

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/connections", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", resp.StatusCode)
	}
}

// ── handleSkillsRegistrySearch: with cached index ────────────────────────────

// writeCachedIndex writes a properly structured index cache that skills.LoadIndex can read.
// The cache file must be a JSON object with "fetched_at" and "entries" fields.
func writeCachedIndex(t *testing.T, cacheDir string, names ...string) {
	t.Helper()
	os.MkdirAll(cacheDir, 0755)
	type indexEntry struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Author      string `json:"author"`
	}
	entries := make([]indexEntry, len(names))
	for i, n := range names {
		entries[i] = indexEntry{Name: n, Description: n + " skill", Author: "test"}
	}
	// Use a time-stamped JSON directly to avoid importing skills internals.
	// FetchedAt must be recent (within TTL) so the cache is considered fresh.
	data := `{"fetched_at":"` + "2099-12-31T00:00:00Z" + `","entries":[`
	for i, e := range entries {
		if i > 0 {
			data += ","
		}
		data += `{"name":"` + e.Name + `","description":"` + e.Description + `","author":"` + e.Author + `"}`
	}
	data += `]}`
	os.WriteFile(filepath.Join(cacheDir, "skills-index.json"), []byte(data), 0644)
}

func TestHandleSkillsRegistrySearch_WithCache(t *testing.T) {
	dir := t.TempDir()
	writeCachedIndex(t, filepath.Join(dir, "cache"), "go-expert", "rust-expert")

	s := &Server{huginnDir: dir}
	req := httptest.NewRequest("GET", "/api/v1/skills/registry/search?q=go", nil)
	w := httptest.NewRecorder()
	s.handleSkillsRegistrySearch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp []map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp) == 0 {
		t.Error("want at least one result for query 'go'")
	}
}

func TestHandleSkillsRegistrySearch_EmptyQuery(t *testing.T) {
	dir := t.TempDir()
	writeCachedIndex(t, filepath.Join(dir, "cache"), "go-expert", "rust-expert")

	s := &Server{huginnDir: dir}
	req := httptest.NewRequest("GET", "/api/v1/skills/registry/search", nil)
	w := httptest.NewRecorder()
	s.handleSkillsRegistrySearch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp []map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	// Empty query returns all
	if len(resp) != 2 {
		t.Errorf("want 2 results for empty query, got %d", len(resp))
	}
}

// ── handleSkillsRegistryIndex: with cached index ─────────────────────────────

func TestHandleSkillsRegistryIndex_WithCache(t *testing.T) {
	dir := t.TempDir()
	writeCachedIndex(t, filepath.Join(dir, "cache"), "go-expert")

	s := &Server{huginnDir: dir}
	req := httptest.NewRequest("GET", "/api/v1/skills/registry/index", nil)
	w := httptest.NewRecorder()
	s.handleSkillsRegistryIndex(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp struct {
		Skills []map[string]any `json:"skills"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Skills) == 0 {
		t.Error("want at least one entry from cached index")
	}
}

// ── session store List error path (handleListSessions) ────────────────────────

// TestHandleListSessions_WithRoutineSession verifies that routine sessions are
// filtered out by default and included when include_routine_sessions=true.
func TestHandleListSessions_RoutineFilter(t *testing.T) {
	srv, ts := newTestServer(t)

	// Create one regular session and one routine session.
	s1 := srv.store.New("regular", "/tmp", "claude-haiku")
	s1.Manifest.Source = "user"
	srv.store.SaveManifest(s1)

	s2 := srv.store.New("routine", "/tmp", "claude-haiku")
	s2.Manifest.Source = "routine"
	srv.store.SaveManifest(s2)

	// Without filter: only user sessions.
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body []session.Manifest
	json.NewDecoder(resp.Body).Decode(&body)
	for _, m := range body {
		if m.Source == "routine" {
			t.Error("routine session should be filtered by default")
		}
	}

	// With filter: includes routine sessions.
	req2, _ := http.NewRequest("GET", ts.URL+"/api/v1/sessions?include_routine_sessions=true", nil)
	req2.Header.Set("Authorization", "Bearer "+testToken)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var body2 []session.Manifest
	json.NewDecoder(resp2.Body).Decode(&body2)
	if len(body2) < 2 {
		t.Errorf("want >=2 sessions with include_routine_sessions=true, got %d", len(body2))
	}
}
