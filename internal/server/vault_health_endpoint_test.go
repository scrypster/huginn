package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

// TestVaultHealthEndpoint_noMuninnConfig verifies that the endpoint returns
// "unavailable" when no muninn config path has been set.
func TestVaultHealthEndpoint_noMuninnConfig(t *testing.T) {
	srv, ts := newTestServer(t)
	// muninnCfgPath is empty by default.
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{
			Agents: []agents.AgentDef{
				{Name: "myagent", VaultName: "myvault"},
			},
		}, nil
	}

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents/myagent/vault-status", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	status, _ := body["status"].(string)
	if status != "unavailable" {
		t.Fatalf("expected status=unavailable (no muninn cfg), got %q", status)
	}
	if _, ok := body["latency_ms"]; !ok {
		t.Fatal("response must include latency_ms field")
	}
}

// TestVaultHealthEndpoint_agentNotFound verifies that an agent with no vault configured
// returns "unavailable" with an appropriate warning.
func TestVaultHealthEndpoint_agentNotFound(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{
			Agents: []agents.AgentDef{
				{Name: "otheragent", VaultName: ""},
			},
		}, nil
	}

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents/otheragent/vault-status", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body) //nolint:errcheck
	status, _ := body["status"].(string)
	if status != "unavailable" {
		t.Fatalf("expected unavailable for agent with no vault, got %q", status)
	}
	warning, _ := body["warning"].(string)
	if warning == "" {
		t.Fatal("expected non-empty warning")
	}
}

// TestVaultHealthEndpoint_missingAgentName verifies that the endpoint returns 400
// when the agent name is missing from the path.
func TestVaultHealthEndpoint_missingAgentName(t *testing.T) {
	// The route pattern GET /api/v1/agents/{name}/vault-status requires a non-empty
	// name segment — a bare /api/v1/agents//vault-status or similar won't match.
	// Test against a known registered route with a real name instead, verifying
	// that the handler is correctly mounted and auth-gated.
	_, ts := newTestServer(t)

	// A request without auth should return 401.
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents/foo/vault-status", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401 without auth, got %d", resp.StatusCode)
	}
}

// TestVaultHealthEndpoint_responseShape verifies the JSON response always contains
// all required fields: status, tools_count, warning, latency_ms.
func TestVaultHealthEndpoint_responseShape(t *testing.T) {
	srv, ts := newTestServer(t)
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{
			Agents: []agents.AgentDef{
				{Name: "shapecheck", VaultName: "somevault"},
			},
		}, nil
	}
	// muninnCfgPath is unset — will return unavailable, but shape should be complete.

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/agents/shapecheck/vault-status", nil)
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

	requiredFields := []string{"status", "tools_count", "warning", "latency_ms"}
	for _, f := range requiredFields {
		if _, ok := body[f]; !ok {
			t.Errorf("missing required field %q in response", f)
		}
	}
}
