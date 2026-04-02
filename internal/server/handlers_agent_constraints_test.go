package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// newTestServerWithSpaceStore creates a Server with a wired space store.
// Used for testing handlers that interact with spaces (like delete agent with lead assignment).
func newTestServerWithSpaceStore(t *testing.T, spaceStore *spaces.SQLiteSpaceStore) (*Server, *httptest.Server) {
	t.Helper()
	srv, ts := newTestServer(t)
	srv.spaceStore = spaceStore
	return srv, ts
}

// TestHandleUpdateAgent_InvalidColor_Returns422 verifies that a PUT to
// /api/v1/agents/{name} with an invalid color value results in HTTP 422.
func TestHandleUpdateAgent_InvalidColor_Returns422(t *testing.T) {
	_, ts := newTestServer(t)

	body := `{"name":"TestAgent","model":"claude-sonnet-4-6","color":"notacolor"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/TestAgent", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for invalid color, got %d", resp.StatusCode)
	}

	var body2 map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body2); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body2["error"] == "" {
		t.Error("expected non-empty error message in response body")
	}
}

// TestHandleUpdateAgent_MissingModel_Returns422 verifies that PUT to
// /api/v1/agents/{name} without a model field is rejected with HTTP 422.
// Regression: agents could be saved without a model, causing silent failures
// at chat time (no helpful error, ghost notifications, broken DMs).
func TestHandleUpdateAgent_MissingModel_Returns422(t *testing.T) {
	_, ts := newTestServer(t)

	body := `{"name":"TestAgent"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/TestAgent", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for missing model, got %d", resp.StatusCode)
	}
	var respBody map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(respBody["error"], "model") {
		t.Errorf("expected 'model' in error message, got: %s", respBody["error"])
	}
}

// TestHandleDeleteAgent_LastAgent_Returns409 verifies that attempting to
// DELETE the last remaining agent results in HTTP 409.
//
// The handler calls agents.LoadAgents() which reads from ~/.huginn/agents/*.json.
// To control the agent count we redirect HOME to a temp directory containing
// exactly one agent file.
func TestHandleDeleteAgent_LastAgent_Returns409(t *testing.T) {
	// Build a fake HOME with exactly one agent so that LoadAgents() sees len==1.
	fakeHome := t.TempDir()
	agentsDir := filepath.Join(fakeHome, ".huginn", "agents")
	if err := os.MkdirAll(agentsDir, 0o700); err != nil {
		t.Fatalf("create fake agents dir: %v", err)
	}

	// Write a single agent JSON file.
	singleAgent := `{"name":"OnlyAgent","slot":"planner","model":"test-model","color":"#58a6ff"}`
	agentPath := filepath.Join(agentsDir, "onlyagent.json")
	if err := os.WriteFile(agentPath, []byte(singleAgent), 0o600); err != nil {
		t.Fatalf("write agent file: %v", err)
	}

	// Redirect HOME so huginnBaseDir() resolves to our temp dir.
	t.Setenv("HUGINN_HOME", "") // clear package-level override so HOME takes effect
	t.Setenv("HOME", fakeHome)

	_, ts := newTestServer(t)

	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/agents/OnlyAgent", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 when deleting last agent, got %d", resp.StatusCode)
	}

	var respBody map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if respBody["error"] == "" {
		t.Error("expected non-empty error message in response body")
	}
}

// TestHandleUpdateAgent_RenameCollision_Returns409 verifies that renaming an agent
// to an existing agent name results in HTTP 409.
//
// Setup: two agents "Alice" and "Bob" in a temp HOME.
// Request: PUT /api/v1/agents/Alice with body {"name":"Bob"}
// Expected: 409 status code with "already exists" message.
func TestHandleUpdateAgent_RenameCollision_Returns409(t *testing.T) {
	// Build a fake HOME with two agents.
	fakeHome := t.TempDir()
	agentsDir := filepath.Join(fakeHome, ".huginn", "agents")
	if err := os.MkdirAll(agentsDir, 0o700); err != nil {
		t.Fatalf("create fake agents dir: %v", err)
	}

	// Write two agent JSON files.
	aliceAgent := `{"name":"Alice","slot":"planner","model":"test-model","color":"#58a6ff"}`
	alicePath := filepath.Join(agentsDir, "alice.json")
	if err := os.WriteFile(alicePath, []byte(aliceAgent), 0o600); err != nil {
		t.Fatalf("write alice agent file: %v", err)
	}

	bobAgent := `{"name":"Bob","slot":"coder","model":"test-model","color":"#3fb950"}`
	bobPath := filepath.Join(agentsDir, "bob.json")
	if err := os.WriteFile(bobPath, []byte(bobAgent), 0o600); err != nil {
		t.Fatalf("write bob agent file: %v", err)
	}

	// Redirect HOME so huginnBaseDir() resolves to our temp dir.
	t.Setenv("HUGINN_HOME", "") // clear package-level override so HOME takes effect
	t.Setenv("HOME", fakeHome)

	_, ts := newTestServer(t)

	// Try to rename Alice to Bob (collision).
	body := `{"name":"Bob","model":"test-model","system_prompt":"test"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/Alice", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 when renaming to existing agent, got %d", resp.StatusCode)
	}

	var respBody map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if respBody["error"] == "" {
		t.Error("expected non-empty error message in response body")
	}
	if !strings.Contains(respBody["error"], "already exists") {
		t.Errorf("expected 'already exists' in error, got: %s", respBody["error"])
	}
}

// TestHandleUpdateAgent_RenameToSameName_Returns200 verifies that renaming an agent
// to its current name is allowed and returns 200.
//
// Setup: one agent "Alice" in a temp HOME.
// Request: PUT /api/v1/agents/Alice with body {"name":"Alice","system_prompt":"updated"}
// Expected: 200 status code (same name = not a collision).
func TestHandleUpdateAgent_RenameToSameName_Returns200(t *testing.T) {
	// Build a fake HOME with one agent.
	fakeHome := t.TempDir()
	agentsDir := filepath.Join(fakeHome, ".huginn", "agents")
	if err := os.MkdirAll(agentsDir, 0o700); err != nil {
		t.Fatalf("create fake agents dir: %v", err)
	}

	// Write one agent JSON file.
	aliceAgent := `{"name":"Alice","slot":"planner","model":"test-model","color":"#58a6ff","system_prompt":"original"}`
	alicePath := filepath.Join(agentsDir, "alice.json")
	if err := os.WriteFile(alicePath, []byte(aliceAgent), 0o600); err != nil {
		t.Fatalf("write alice agent file: %v", err)
	}

	// Redirect HOME so huginnBaseDir() resolves to our temp dir.
	t.Setenv("HUGINN_HOME", "") // clear package-level override so HOME takes effect
	t.Setenv("HOME", fakeHome)

	_, ts := newTestServer(t)

	// Update Alice with the same name.
	body := `{"name":"Alice","model":"test-model","system_prompt":"updated"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/Alice", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 when updating agent with same name, got %d", resp.StatusCode)
	}

	var respBody map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if respBody["saved"] != "Alice" {
		t.Errorf("expected saved agent name 'Alice', got: %s", respBody["saved"])
	}
}

// TestHandleUpdateAgent_InvalidPlasticity_Returns422 verifies that a PUT with
// an unknown plasticity value is rejected with HTTP 422.
func TestHandleUpdateAgent_InvalidPlasticity_Returns422(t *testing.T) {
	_, ts := newTestServer(t)

	body := `{"name":"TestAgent","model":"claude-sonnet-4-6","plasticity":"turbo-charged"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/TestAgent", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for invalid plasticity, got %d", resp.StatusCode)
	}
	var respBody map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(respBody["error"], "plasticity") {
		t.Errorf("expected 'plasticity' in error, got: %s", respBody["error"])
	}
}

// TestHandleUpdateAgent_ValidPlasticity_Returns200 verifies that PUT with a
// valid plasticity value is accepted.
func TestHandleUpdateAgent_ValidPlasticity_Returns200(t *testing.T) {
	_, ts := newTestServer(t)

	body := `{"name":"TestAgent","model":"claude-sonnet-4-6","plasticity":"knowledge-graph"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/TestAgent", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for valid plasticity, got %d", resp.StatusCode)
	}
}

// TestHandleUpdateAgent_InvalidMemoryMode_Returns422 verifies that a PUT with
// an unknown memory_mode value is rejected with HTTP 422.
func TestHandleUpdateAgent_InvalidMemoryMode_Returns422(t *testing.T) {
	_, ts := newTestServer(t)

	body := `{"name":"TestAgent","model":"claude-sonnet-4-6","memory_mode":"aggressive"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/TestAgent", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for invalid memory_mode, got %d", resp.StatusCode)
	}
	var respBody map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(respBody["error"], "memory_mode") {
		t.Errorf("expected 'memory_mode' in error, got: %s", respBody["error"])
	}
}

// TestHandleUpdateAgent_ValidMemoryMode_Returns200 verifies that PUT with a
// valid memory_mode value is accepted.
func TestHandleUpdateAgent_ValidMemoryMode_Returns200(t *testing.T) {
	_, ts := newTestServer(t)

	body := `{"name":"TestAgent","model":"claude-sonnet-4-6","memory_mode":"immersive"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/TestAgent", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for valid memory_mode, got %d", resp.StatusCode)
	}
}

// TestHandleUpdateAgent_VaultNameCollision_Returns422 verifies that updating an
// agent so its vault name would match an existing agent's explicitly-set vault_name
// is rejected with HTTP 422.
func TestHandleUpdateAgent_VaultNameCollision_Returns422(t *testing.T) {
	fakeHome := t.TempDir()
	agentsDir := filepath.Join(fakeHome, ".huginn", "agents")
	if err := os.MkdirAll(agentsDir, 0o700); err != nil {
		t.Fatalf("create fake agents dir: %v", err)
	}

	// "Alpha" has an explicitly set vault_name "shared-vault".
	alpha := `{"name":"Alpha","slot":"planner","model":"test-model","color":"#58a6ff","vault_name":"shared-vault"}`
	if err := os.WriteFile(filepath.Join(agentsDir, "alpha.json"), []byte(alpha), 0o600); err != nil {
		t.Fatalf("write Alpha agent: %v", err)
	}
	// "Beta" currently has no vault_name (auto-generated as "huginn:agent::beta").
	beta := `{"name":"Beta","slot":"planner","model":"test-model","color":"#3fb950"}`
	if err := os.WriteFile(filepath.Join(agentsDir, "beta.json"), []byte(beta), 0o600); err != nil {
		t.Fatalf("write Beta agent: %v", err)
	}

	t.Setenv("HUGINN_HOME", "")
	t.Setenv("HOME", fakeHome)
	_, ts := newTestServer(t)

	// Update "Beta" to also use vault_name "shared-vault" → collision with Alpha.
	body := `{"name":"Beta","slot":"planner","model":"test-model","color":"#3fb950","vault_name":"shared-vault"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/Beta", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for vault name collision, got %d", resp.StatusCode)
	}
	var respBody map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(respBody["error"], "vault") {
		t.Errorf("expected 'vault' in error, got: %s", respBody["error"])
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Hardening: Delete agent assigned as space lead (review/agents-channels-dm-hardening)
// ────────────────────────────────────────────────────────────────────────────

// TestHandleDeleteAgent_WithChannelLeadAssignment_Returns409 verifies that
// attempting to delete an agent that is assigned as a lead_agent in any space
// results in HTTP 409 with a message listing the spaces.
//
// This prevents orphaning channels when their lead agent is deleted.
func TestHandleDeleteAgent_WithChannelLeadAssignment_Returns409(t *testing.T) {
	// Setup agents
	fakeHome := t.TempDir()
	agentsDir := filepath.Join(fakeHome, ".huginn", "agents")
	if err := os.MkdirAll(agentsDir, 0o700); err != nil {
		t.Fatalf("create fake agents dir: %v", err)
	}

	// Write two agents
	aliceAgent := `{"name":"Alice","model":"test-model"}`
	alicePath := filepath.Join(agentsDir, "alice.json")
	if err := os.WriteFile(alicePath, []byte(aliceAgent), 0o600); err != nil {
		t.Fatalf("write alice agent file: %v", err)
	}

	bobAgent := `{"name":"Bob","model":"test-model"}`
	bobPath := filepath.Join(agentsDir, "bob.json")
	if err := os.WriteFile(bobPath, []byte(bobAgent), 0o600); err != nil {
		t.Fatalf("write bob agent file: %v", err)
	}

	t.Setenv("HUGINN_HOME", "")
	t.Setenv("HOME", fakeHome)

	// Setup space store with a channel where Alice is the lead agent
	dbDir := t.TempDir()
	db, err := sqlitedb.Open(filepath.Join(dbDir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := db.ApplySchema(); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("migrate spaces: %v", err)
	}

	spaceStore := spaces.NewSQLiteSpaceStore(db)

	// Create a channel with Alice as the lead agent
	_, err = spaceStore.CreateChannel("Engineering Team", "Alice", []string{"Bob"}, "", "")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	// Create server with space store wired
	_, ts := newTestServerWithSpaceStore(t, spaceStore)

	// Try to delete Alice (should fail because she's a lead agent)
	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/agents/Alice", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 when deleting agent with channel assignment, got %d", resp.StatusCode)
	}

	var respBody map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if respBody["error"] == "" {
		t.Error("expected non-empty error message in response body")
	}
	// Error should mention the space name
	if !strings.Contains(respBody["error"], "Engineering Team") {
		t.Errorf("expected space name in error, got: %s", respBody["error"])
	}
}

// TestHandleDeleteAgent_WithoutChannelLeadAssignment_Succeeds verifies that
// deleting an agent that is NOT assigned as a lead_agent succeeds (200).
// Even if the agent is a member of spaces, deletion should work.
func TestHandleDeleteAgent_WithoutChannelLeadAssignment_Succeeds(t *testing.T) {
	// Setup agents
	fakeHome := t.TempDir()
	agentsDir := filepath.Join(fakeHome, ".huginn", "agents")
	if err := os.MkdirAll(agentsDir, 0o700); err != nil {
		t.Fatalf("create fake agents dir: %v", err)
	}

	// Write two agents
	aliceAgent := `{"name":"Alice","model":"test-model"}`
	alicePath := filepath.Join(agentsDir, "alice.json")
	if err := os.WriteFile(alicePath, []byte(aliceAgent), 0o600); err != nil {
		t.Fatalf("write alice agent file: %v", err)
	}

	bobAgent := `{"name":"Bob","model":"test-model"}`
	bobPath := filepath.Join(agentsDir, "bob.json")
	if err := os.WriteFile(bobPath, []byte(bobAgent), 0o600); err != nil {
		t.Fatalf("write bob agent file: %v", err)
	}

	t.Setenv("HUGINN_HOME", "")
	t.Setenv("HOME", fakeHome)

	// Setup space store with a channel where Alice is the lead agent
	dbDir := t.TempDir()
	db, err := sqlitedb.Open(filepath.Join(dbDir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := db.ApplySchema(); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("migrate spaces: %v", err)
	}

	spaceStore := spaces.NewSQLiteSpaceStore(db)

	// Create a channel with Alice as the lead agent and Bob as a member
	_, err = spaceStore.CreateChannel("Engineering Team", "Alice", []string{"Bob"}, "", "")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	// Create server with space store wired
	_, ts := newTestServerWithSpaceStore(t, spaceStore)

	// Delete Bob (should succeed because Bob is not a lead agent)
	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/agents/Bob", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 when deleting agent not assigned as lead, got %d", resp.StatusCode)
	}

	var respBody map[string]bool
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if !respBody["deleted"] {
		t.Error("expected deleted=true in response")
	}

	// Verify the agent file is actually deleted
	if _, err := os.Stat(bobPath); !os.IsNotExist(err) {
		t.Error("bob.json should be deleted")
	}
}
