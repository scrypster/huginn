package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

// TestHandleUpdateAgent_ToolbeltRoundtrips verifies that the toolbelt field
// correctly round-trips through PUT /api/v1/agents/{name} and GET /api/v1/agents/{name}.
func TestHandleUpdateAgent_ToolbeltRoundtrips(t *testing.T) {
	_, ts := newTestServer(t)

	// PUT with toolbelt containing two entries
	body := `{
		"name": "TestAgent",
		"slot": "coder",
		"model": "claude-opus-4",
		"toolbelt": [
			{"connection_id": "conn-github-1", "provider": "github", "approval_gate": false},
			{"connection_id": "conn-slack-2", "provider": "slack", "approval_gate": true}
		]
	}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/TestAgent", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// GET and verify toolbelt was saved
	getReq, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents/TestAgent", nil)
	getReq.Header.Set("Authorization", "Bearer "+testToken)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != 200 {
		t.Fatalf("expected GET 200, got %d", getResp.StatusCode)
	}

	var def agents.AgentDef
	if err := json.NewDecoder(getResp.Body).Decode(&def); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	// Verify toolbelt array length
	if len(def.Toolbelt) != 2 {
		t.Fatalf("expected 2 toolbelt entries, got %d", len(def.Toolbelt))
	}

	// Verify first entry (GitHub)
	githubFound := false
	for _, e := range def.Toolbelt {
		if e.ConnectionID == "conn-github-1" && e.Provider == "github" && !e.ApprovalGate {
			githubFound = true
		}
	}
	if !githubFound {
		t.Fatalf("github approval_gate=false entry not found in toolbelt: %+v", def.Toolbelt)
	}

	// Verify second entry (Slack)
	slackFound := false
	for _, e := range def.Toolbelt {
		if e.ConnectionID == "conn-slack-2" && e.Provider == "slack" && e.ApprovalGate {
			slackFound = true
		}
	}
	if !slackFound {
		t.Fatalf("slack approval_gate=true entry not found in toolbelt: %+v", def.Toolbelt)
	}
}

// TestHandleUpdateAgent_EmptyToolbelt verifies that an empty toolbelt is handled correctly.
func TestHandleUpdateAgent_EmptyToolbelt(t *testing.T) {
	_, ts := newTestServer(t)

	// PUT with empty toolbelt
	body := `{
		"name": "AgentNoTools",
		"slot": "planner",
		"model": "claude-opus-4",
		"toolbelt": []
	}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/AgentNoTools", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// GET and verify empty toolbelt
	getReq, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents/AgentNoTools", nil)
	getReq.Header.Set("Authorization", "Bearer "+testToken)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer getResp.Body.Close()

	var def agents.AgentDef
	if err := json.NewDecoder(getResp.Body).Decode(&def); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(def.Toolbelt) != 0 {
		t.Fatalf("expected 0 toolbelt entries, got %d", len(def.Toolbelt))
	}
}

// TestHandleUpdateAgent_NoToolbeltField verifies that agents without a toolbelt field
// continue to work (backward compatibility).
func TestHandleUpdateAgent_NoToolbeltField(t *testing.T) {
	_, ts := newTestServer(t)

	// PUT without toolbelt field at all
	body := `{
		"name": "AgentNoToolbeltField",
		"slot": "reasoner",
		"model": "claude-opus-4"
	}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/AgentNoToolbeltField", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// GET and verify toolbelt is nil or empty
	getReq, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents/AgentNoToolbeltField", nil)
	getReq.Header.Set("Authorization", "Bearer "+testToken)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer getResp.Body.Close()

	var def agents.AgentDef
	if err := json.NewDecoder(getResp.Body).Decode(&def); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	// Toolbelt should be nil or empty (both acceptable for backward compat)
	if def.Toolbelt != nil && len(def.Toolbelt) != 0 {
		t.Fatalf("expected nil or empty toolbelt, got %+v", def.Toolbelt)
	}
}

// TestHandleUpdateAgent_ToolbeltPreservesApprovalGate verifies that approval_gate
// field is preserved exactly as provided.
func TestHandleUpdateAgent_ToolbeltPreservesApprovalGate(t *testing.T) {
	_, ts := newTestServer(t)

	// PUT with mixed approval_gate values
	body := `{
		"name": "TestGates",
		"slot": "coder",
		"model": "claude-opus-4",
		"toolbelt": [
			{"connection_id": "c1", "provider": "aws", "approval_gate": true},
			{"connection_id": "c2", "provider": "gcp", "approval_gate": false},
			{"connection_id": "c3", "provider": "azure"}
		]
	}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/TestGates", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT failed: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// GET and verify all three entries with correct approval_gate values
	getReq, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents/TestGates", nil)
	getReq.Header.Set("Authorization", "Bearer "+testToken)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer getResp.Body.Close()

	var def agents.AgentDef
	if err := json.NewDecoder(getResp.Body).Decode(&def); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(def.Toolbelt) != 3 {
		t.Fatalf("expected 3 toolbelt entries, got %d", len(def.Toolbelt))
	}

	// Check AWS (approval_gate: true)
	if def.Toolbelt[0].ConnectionID != "c1" || def.Toolbelt[0].Provider != "aws" || !def.Toolbelt[0].ApprovalGate {
		t.Fatalf("AWS entry not correct: %+v", def.Toolbelt[0])
	}

	// Check GCP (approval_gate: false)
	if def.Toolbelt[1].ConnectionID != "c2" || def.Toolbelt[1].Provider != "gcp" || def.Toolbelt[1].ApprovalGate {
		t.Fatalf("GCP entry not correct: %+v", def.Toolbelt[1])
	}

	// Check Azure (approval_gate: omitted, should default to false)
	if def.Toolbelt[2].ConnectionID != "c3" || def.Toolbelt[2].Provider != "azure" || def.Toolbelt[2].ApprovalGate {
		t.Fatalf("Azure entry not correct: %+v", def.Toolbelt[2])
	}
}
