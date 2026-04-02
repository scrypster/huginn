package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helpers shared by rename tests ─────────────────────────────────────────────

func setupAgentsDir(t *testing.T, agents map[string]string) (agentsDir string) {
	t.Helper()
	fakeHome := t.TempDir()
	agentsDir = filepath.Join(fakeHome, ".huginn", "agents")
	if err := os.MkdirAll(agentsDir, 0o700); err != nil {
		t.Fatalf("create agents dir: %v", err)
	}
	for filename, content := range agents {
		if err := os.WriteFile(filepath.Join(agentsDir, filename), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", filename, err)
		}
	}
	t.Setenv("HUGINN_HOME", fakeHome)
	return agentsDir
}

// ── Rename: old file deleted, new file created ────────────────────────────────

// TestHandleUpdateAgent_Rename_DeletesOldFile is the core regression test for
// the "edit creates duplicate agent" bug.
// PUT /api/v1/agents/Alice with body {"name":"Charlie",...} must:
//   - create ~/.huginn/agents/charlie.json
//   - delete ~/.huginn/agents/alice.json
func TestHandleUpdateAgent_Rename_DeletesOldFile(t *testing.T) {
	agentsDir := setupAgentsDir(t, map[string]string{
		"alice.json": `{"name":"Alice","model":"claude-sonnet-4-6","color":"#58a6ff"}`,
	})
	_, ts := newTestServer(t)

	body := `{"name":"Charlie","model":"claude-sonnet-4-6","color":"#58a6ff"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/Alice", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// New file must exist.
	if _, err := os.Stat(filepath.Join(agentsDir, "charlie.json")); os.IsNotExist(err) {
		t.Error("charlie.json should exist after rename")
	}
	// Old file must be gone.
	if _, err := os.Stat(filepath.Join(agentsDir, "alice.json")); !os.IsNotExist(err) {
		t.Error("alice.json should be deleted after rename but still exists")
	}
}

// TestHandleUpdateAgent_Rename_NewFileHasCorrectContent verifies the saved file
// contains the new name and updated fields, not the old ones.
func TestHandleUpdateAgent_Rename_NewFileHasCorrectContent(t *testing.T) {
	agentsDir := setupAgentsDir(t, map[string]string{
		"alice.json": `{"name":"Alice","model":"claude-sonnet-4-6","color":"#58a6ff","system_prompt":"old"}`,
	})
	_, ts := newTestServer(t)

	body := `{"name":"Charlie","model":"claude-sonnet-4-6","color":"#3fb950","system_prompt":"new prompt"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/Alice", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	data, err := os.ReadFile(filepath.Join(agentsDir, "charlie.json"))
	if err != nil {
		t.Fatalf("read charlie.json: %v", err)
	}
	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if saved["name"] != "Charlie" {
		t.Errorf("name: want Charlie, got %v", saved["name"])
	}
	if saved["system_prompt"] != "new prompt" {
		t.Errorf("system_prompt: want 'new prompt', got %v", saved["system_prompt"])
	}
}

// ── Edit in-place: no duplicate files ────────────────────────────────────────

// TestHandleUpdateAgent_EditInPlace_NoDuplication verifies that editing an agent
// without changing its name produces exactly one file and updates the content.
func TestHandleUpdateAgent_EditInPlace_NoDuplication(t *testing.T) {
	agentsDir := setupAgentsDir(t, map[string]string{
		"alice.json": `{"name":"Alice","model":"claude-sonnet-4-6","color":"#58a6ff","system_prompt":"original"}`,
	})
	_, ts := newTestServer(t)

	body := `{"name":"Alice","model":"claude-sonnet-4-6","color":"#58a6ff","system_prompt":"updated"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/Alice", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		t.Fatalf("read agents dir: %v", err)
	}
	var jsonFiles []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			jsonFiles = append(jsonFiles, e.Name())
		}
	}
	if len(jsonFiles) != 1 {
		t.Errorf("expected 1 agent file, got %d: %v", len(jsonFiles), jsonFiles)
	}

	data, err := os.ReadFile(filepath.Join(agentsDir, "alice.json"))
	if err != nil {
		t.Fatalf("read alice.json: %v", err)
	}
	var saved map[string]any
	json.Unmarshal(data, &saved) //nolint:errcheck
	if saved["system_prompt"] != "updated" {
		t.Errorf("system_prompt: want 'updated', got %v", saved["system_prompt"])
	}
}

// ── GET is case-insensitive ───────────────────────────────────────────────────

// TestHandleGetAgent_CaseInsensitive verifies that GET /api/v1/agents/alice
// returns the agent even when it was stored as "Alice".
func TestHandleGetAgent_CaseInsensitive(t *testing.T) {
	setupAgentsDir(t, map[string]string{
		"alice.json": `{"name":"Alice","model":"claude-sonnet-4-6","color":"#58a6ff"}`,
	})
	_, ts := newTestServer(t)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents/alice", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for case-insensitive GET, got %d", resp.StatusCode)
	}

	var agent map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&agent); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if agent["name"] != "Alice" {
		t.Errorf("name: want Alice, got %v", agent["name"])
	}
}

// ── Rename collision is rejected ─────────────────────────────────────────────

// TestHandleUpdateAgent_Rename_ExistingAgentCount verifies that after a successful
// rename the total agent count stays the same (one removed, one added — net zero).
func TestHandleUpdateAgent_Rename_ExistingAgentCount(t *testing.T) {
	agentsDir := setupAgentsDir(t, map[string]string{
		"alice.json": `{"name":"Alice","model":"claude-sonnet-4-6","color":"#58a6ff"}`,
		"bob.json":   `{"name":"Bob","model":"claude-sonnet-4-6","color":"#3fb950"}`,
	})
	_, ts := newTestServer(t)

	// Rename Alice → Charlie (not Bob, so no collision).
	body := `{"name":"Charlie","model":"claude-sonnet-4-6","color":"#58a6ff"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/Alice", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		t.Fatalf("read agents dir: %v", err)
	}
	var jsonFiles []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			jsonFiles = append(jsonFiles, e.Name())
		}
	}
	// Still 2 agents: Bob + Charlie.
	if len(jsonFiles) != 2 {
		t.Errorf("expected 2 agent files after rename, got %d: %v", len(jsonFiles), jsonFiles)
	}
	if _, err := os.Stat(filepath.Join(agentsDir, "bob.json")); os.IsNotExist(err) {
		t.Error("bob.json should still exist")
	}
	if _, err := os.Stat(filepath.Join(agentsDir, "charlie.json")); os.IsNotExist(err) {
		t.Error("charlie.json should exist")
	}
	if _, err := os.Stat(filepath.Join(agentsDir, "alice.json")); !os.IsNotExist(err) {
		t.Error("alice.json should be deleted")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Hardening: Duplicate agent name check on CREATE (review/agents-channels-dm-hardening)
// ────────────────────────────────────────────────────────────────────────────

// TestHandleUpdateAgent_Create_DuplicateName_CaseInsensitive verifies that
// creating a NEW agent whose name collides case-insensitively with an existing
// agent returns 409. The URL path uses a unique new name ("bob") but the body
// sets name to "alice" which collides with the existing "Alice".
func TestHandleUpdateAgent_Create_DuplicateName_CaseInsensitive(t *testing.T) {
	setupAgentsDir(t, map[string]string{
		"alice.json": `{"name":"Alice","model":"claude-sonnet-4-6","color":"#58a6ff"}`,
	})
	_, ts := newTestServer(t)

	// Create a NEW agent "bob" but set body name to "alice" (rename-into-collision).
	body := `{"name":"alice","model":"claude-sonnet-4-6","color":"#3fb950"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/bob", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Should return 409 Conflict because "alice" (case-insensitive) already exists.
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 Conflict for duplicate name, got %d", resp.StatusCode)
	}

	var errResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil {
		if msg, ok := errResp["error"].(string); ok && strings.Contains(msg, "already exists") {
			// Good: error message mentions the duplicate.
		}
	}
}

// TestHandleUpdateAgent_Create_DifferentName_Succeeds verifies that creating
// agents with different names both succeeds.
func TestHandleUpdateAgent_Create_DifferentName_Succeeds(t *testing.T) {
	setupAgentsDir(t, map[string]string{
		"alice.json": `{"name":"Alice","model":"claude-sonnet-4-6","color":"#58a6ff"}`,
	})
	_, ts := newTestServer(t)

	// Create a different agent "Bob" (should succeed).
	body := `{"name":"Bob","model":"claude-sonnet-4-6","color":"#3fb950"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/Bob", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for creating different agent, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["saved"] != "Bob" {
		t.Errorf("expected saved agent 'Bob', got %v", result["saved"])
	}
}
