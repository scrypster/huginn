package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

func TestHandleUpdateAgent_DerivesDescriptionFromSystemPrompt(t *testing.T) {
	_, ts := newTestServer(t)
	t.Cleanup(func() { _ = agents.DeleteAgentDefault("DescDeriveAgent") })

	body := `{
		"name": "DescDeriveAgent",
		"model": "claude-sonnet-4-6",
		"system_prompt": "You are Steve, a coder. Use tools."
	}`
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/agents/DescDeriveAgent", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("PUT status %d", resp.StatusCode)
	}

	cfg, err := agents.LoadAgents()
	if err != nil {
		t.Fatalf("LoadAgents: %v", err)
	}
	var found *agents.AgentDef
	for i := range cfg.Agents {
		if strings.EqualFold(cfg.Agents[i].Name, "DescDeriveAgent") {
			found = &cfg.Agents[i]
			break
		}
	}
	if found == nil {
		t.Fatal("saved agent not found on disk")
	}
	if found.Description != "a coder" {
		t.Fatalf("persisted description = %q, want %q", found.Description, "a coder")
	}
}

func TestHandleGetAgent_FillsDerivedDescriptionForLegacyAgent(t *testing.T) {
	_, ts := newTestServer(t)
	t.Cleanup(func() { _ = agents.DeleteAgentDefault("LegacyDescAgent") })

	if err := agents.SaveAgentDefault(agents.AgentDef{
		Name:         "LegacyDescAgent",
		Model:        "claude-sonnet-4-6",
		SystemPrompt: "You are Nova, the scheduler.",
	}); err != nil {
		t.Fatalf("SaveAgentDefault: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/agents/LegacyDescAgent", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET status %d", resp.StatusCode)
	}

	var def agents.AgentDef
	if err := json.NewDecoder(resp.Body).Decode(&def); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if def.Description != "the scheduler" {
		t.Fatalf("GET description = %q, want %q", def.Description, "the scheduler")
	}

	cfg, err := agents.LoadAgents()
	if err != nil {
		t.Fatalf("LoadAgents: %v", err)
	}
	for _, a := range cfg.Agents {
		if strings.EqualFold(a.Name, "LegacyDescAgent") && a.Description != "" {
			t.Fatalf("legacy yaml should remain without a persisted description, got %q", a.Description)
		}
	}
}
