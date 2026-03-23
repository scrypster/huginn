package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

// TestAgentMemoryModeRoundTrip verifies that memory_mode and vault_description
// fields round-trip correctly through the agent REST API.
// This test validates the MuninnDB migration (Task 6) by ensuring that:
// 1. PATCH/PUT requests preserve memory_mode and vault_description in JSON
// 2. GET requests return the stored values
// 3. The values persist to disk (agents.SaveAgentDefault)
func TestAgentMemoryModeRoundTrip(t *testing.T) {
	_, ts := newTestServer(t)

	// Step 1: Create/update an agent with memory_mode and vault_description
	agentBody := `{
		"name": "test-memory-agent",
		"slot": "planner",
		"model": "test-model",
		"system_prompt": "You are a test agent",
		"memory_mode": "immersive",
		"vault_description": "This agent manages project knowledge and learning",
		"color": "#0000ff",
		"icon": "robot"
	}`

	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/test-memory-agent", strings.NewReader(agentBody))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 from PUT agent, got %d", resp.StatusCode)
	}

	// Step 2: GET the agent back and verify memory_mode and vault_description are present
	getReq, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents/test-memory-agent", nil)
	getReq.Header.Set("Authorization", "Bearer "+testToken)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != 200 {
		t.Fatalf("expected 200 from GET agent, got %d", getResp.StatusCode)
	}

	var retrieved agents.AgentDef
	if err := json.NewDecoder(getResp.Body).Decode(&retrieved); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify the fields survived the round-trip
	if retrieved.MemoryMode != "immersive" {
		t.Errorf("expected memory_mode=immersive, got %q", retrieved.MemoryMode)
	}
	if retrieved.VaultDescription != "This agent manages project knowledge and learning" {
		t.Errorf("expected vault_description to match, got %q", retrieved.VaultDescription)
	}
	if retrieved.Name != "test-memory-agent" {
		t.Errorf("expected name=test-memory-agent, got %q", retrieved.Name)
	}
}

// TestAgentMemoryModeConversational verifies that memory_mode="conversational"
// is properly stored and retrieved.
func TestAgentMemoryModeConversational(t *testing.T) {
	_, ts := newTestServer(t)

	agentBody := `{
		"name": "conversational-agent",
		"slot": "coder",
		"model": "test-model",
		"system_prompt": "Test prompt",
		"memory_mode": "conversational",
		"vault_description": "For conversational memory",
		"color": "#00ff00",
		"icon": "terminal"
	}`

	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/conversational-agent", strings.NewReader(agentBody))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Retrieve and verify
	getReq, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents/conversational-agent", nil)
	getReq.Header.Set("Authorization", "Bearer "+testToken)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()

	var retrieved agents.AgentDef
	if err := json.NewDecoder(getResp.Body).Decode(&retrieved); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if retrieved.MemoryMode != "conversational" {
		t.Errorf("expected conversational, got %q", retrieved.MemoryMode)
	}
	if retrieved.VaultDescription != "For conversational memory" {
		t.Errorf("expected specific description, got %q", retrieved.VaultDescription)
	}
}

// TestAgentMemoryModePassive verifies that memory_mode="passive" is handled correctly.
func TestAgentMemoryModePassive(t *testing.T) {
	_, ts := newTestServer(t)

	agentBody := `{
		"name": "passive-agent",
		"slot": "reasoner",
		"model": "test-model",
		"system_prompt": "Test",
		"memory_mode": "passive",
		"vault_description": "Passive memory usage",
		"color": "#800080",
		"icon": "brain"
	}`

	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/passive-agent", strings.NewReader(agentBody))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	getReq, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents/passive-agent", nil)
	getReq.Header.Set("Authorization", "Bearer "+testToken)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()

	var retrieved agents.AgentDef
	if err := json.NewDecoder(getResp.Body).Decode(&retrieved); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if retrieved.MemoryMode != "passive" {
		t.Errorf("expected passive, got %q", retrieved.MemoryMode)
	}
	if retrieved.VaultDescription != "Passive memory usage" {
		t.Errorf("expected matching description, got %q", retrieved.VaultDescription)
	}
}

// TestAgentMemoryModeEmpty verifies that omitting memory_mode is handled correctly.
// The field should remain empty (which means "conversational" at runtime).
func TestAgentMemoryModeEmpty(t *testing.T) {
	_, ts := newTestServer(t)

	agentBody := `{
		"name": "default-agent",
		"slot": "planner",
		"model": "test-model",
		"system_prompt": "Test",
		"color": "#ff0000",
		"icon": "star"
	}`

	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/default-agent", strings.NewReader(agentBody))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	getReq, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents/default-agent", nil)
	getReq.Header.Set("Authorization", "Bearer "+testToken)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()

	var retrieved agents.AgentDef
	if err := json.NewDecoder(getResp.Body).Decode(&retrieved); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	// When not provided, should be empty string (defaults to "conversational" at runtime)
	if retrieved.MemoryMode != "" {
		t.Errorf("expected empty memory_mode, got %q", retrieved.MemoryMode)
	}
	if retrieved.VaultDescription != "" {
		t.Errorf("expected empty vault_description, got %q", retrieved.VaultDescription)
	}
}

// TestAgentVaultDescriptionMultiline verifies that multi-line vault descriptions
// are properly preserved through the round-trip.
func TestAgentVaultDescriptionMultiline(t *testing.T) {
	_, ts := newTestServer(t)

	agentBody := `{
		"name": "multiline-vault-agent",
		"slot": "planner",
		"model": "test-model",
		"system_prompt": "Test",
		"memory_mode": "immersive",
		"vault_description": "This vault contains:\n- Project documentation\n- Code patterns\n- Meeting notes\n- Decision records",
		"color": "#ff8800",
		"icon": "book"
	}`

	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/multiline-vault-agent", strings.NewReader(agentBody))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	getReq, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents/multiline-vault-agent", nil)
	getReq.Header.Set("Authorization", "Bearer "+testToken)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()

	var retrieved agents.AgentDef
	if err := json.NewDecoder(getResp.Body).Decode(&retrieved); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	expectedDesc := "This vault contains:\n- Project documentation\n- Code patterns\n- Meeting notes\n- Decision records"
	if retrieved.VaultDescription != expectedDesc {
		t.Errorf("multi-line vault_description not preserved.\nExpected: %q\nGot: %q", expectedDesc, retrieved.VaultDescription)
	}
}

// TestAgentMemoryFieldsWithOtherFields verifies that memory_mode and vault_description
// don't interfere with other agent fields.
func TestAgentMemoryFieldsWithOtherFields(t *testing.T) {
	_, ts := newTestServer(t)

	agentBody := `{
		"name": "full-agent",
		"slot": "coder",
		"model": "claude-opus-4",
		"system_prompt": "You are a full-featured agent",
		"memory_mode": "conversational",
		"vault_description": "Complete vault setup",
		"color": "#00ffff",
		"icon": "zap",
		"vault_name": "custom:vault:name",
		"plasticity": "knowledge-graph",
		"memory_enabled": true,
		"context_notes_enabled": true
	}`

	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/full-agent", strings.NewReader(agentBody))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	getReq, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents/full-agent", nil)
	getReq.Header.Set("Authorization", "Bearer "+testToken)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()

	var retrieved agents.AgentDef
	if err := json.NewDecoder(getResp.Body).Decode(&retrieved); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	// Verify all fields survived
	if retrieved.MemoryMode != "conversational" {
		t.Errorf("memory_mode mismatch: %q", retrieved.MemoryMode)
	}
	if retrieved.VaultDescription != "Complete vault setup" {
		t.Errorf("vault_description mismatch: %q", retrieved.VaultDescription)
	}
	if retrieved.Model != "claude-opus-4" {
		t.Errorf("model mismatch: %q", retrieved.Model)
	}
	if retrieved.Color != "#00ffff" {
		t.Errorf("color mismatch: %q", retrieved.Color)
	}
	if retrieved.VaultName != "custom:vault:name" {
		t.Errorf("vault_name mismatch: %q", retrieved.VaultName)
	}
	if retrieved.Plasticity != "knowledge-graph" {
		t.Errorf("plasticity mismatch: %q", retrieved.Plasticity)
	}
	if retrieved.MemoryEnabled == nil || !*retrieved.MemoryEnabled {
		t.Errorf("memory_enabled should be true")
	}
	if !retrieved.ContextNotesEnabled {
		t.Errorf("context_notes_enabled should be true")
	}
}
