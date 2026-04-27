package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

// TestHandleCloneAgent_NewModelInferred verifies the clone endpoint copies
// the source agent and reassigns the model + provider. Provider is inferred
// when the caller doesn't supply one — the most ergonomic UX.
func TestHandleCloneAgent_NewModelInferred(t *testing.T) {
	_, ts := newTestServer(t)

	src := agents.AgentDef{
		Name:     "Source",
		Model:    "claude-haiku-4-5",
		Provider: "anthropic",
		Skills:   []string{"summarise"},
		LocalTools: []string{"read_file"},
	}
	if err := agents.SaveAgentDefault(src); err != nil {
		t.Fatalf("SaveAgentDefault(src): %v", err)
	}

	body := `{"new_name":"Source-Sonnet","model":"claude-sonnet-4-6"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/agents/Source/clone", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clone: want 200, got %d", resp.StatusCode)
	}
	var got agents.AgentDef
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "Source-Sonnet" {
		t.Errorf("clone Name = %q, want %q", got.Name, "Source-Sonnet")
	}
	if got.Model != "claude-sonnet-4-6" {
		t.Errorf("clone Model = %q, want %q", got.Model, "claude-sonnet-4-6")
	}
	if got.Provider != "anthropic" {
		t.Errorf("clone Provider = %q, want anthropic (preserved or inferred)", got.Provider)
	}
	// Skills + local tools should be copied across — the whole point of the
	// "clone, swap model" UX is to inherit configuration.
	if len(got.Skills) != 1 || got.Skills[0] != "summarise" {
		t.Errorf("clone Skills = %#v, want [\"summarise\"]", got.Skills)
	}
	if len(got.LocalTools) != 1 || got.LocalTools[0] != "read_file" {
		t.Errorf("clone LocalTools = %#v, want [\"read_file\"]", got.LocalTools)
	}

	// Source must NOT be mutated.
	cfg, err := agents.LoadAgents()
	if err != nil {
		t.Fatalf("LoadAgents: %v", err)
	}
	for _, a := range cfg.Agents {
		if a.Name == "Source" && a.Model != "claude-haiku-4-5" {
			t.Errorf("source agent Model mutated: %q", a.Model)
		}
	}
}

// TestHandleCloneAgent_RejectsCollision returns 409 when the new name is
// already taken — duplicate-name agents would corrupt the registry.
func TestHandleCloneAgent_RejectsCollision(t *testing.T) {
	_, ts := newTestServer(t)
	if err := agents.SaveAgentDefault(agents.AgentDef{Name: "First", Model: "haiku", Provider: "anthropic"}); err != nil {
		t.Fatal(err)
	}
	if err := agents.SaveAgentDefault(agents.AgentDef{Name: "Second", Model: "haiku", Provider: "anthropic"}); err != nil {
		t.Fatal(err)
	}

	body := `{"new_name":"Second"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/agents/First/clone", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("collision: want 409, got %d", resp.StatusCode)
	}
}

// TestHandleCloneAgent_RequiresNewName returns 400 when no new_name is set.
func TestHandleCloneAgent_RequiresNewName(t *testing.T) {
	_, ts := newTestServer(t)
	if err := agents.SaveAgentDefault(agents.AgentDef{Name: "Solo", Model: "haiku", Provider: "anthropic"}); err != nil {
		t.Fatal(err)
	}

	body := `{}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/agents/Solo/clone", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("missing new_name: want 400, got %d", resp.StatusCode)
	}
}

// TestHandleCloneAgent_RejectsSelfRename returns 400 when new_name equals
// source. Cloning yourself onto yourself would silently overwrite the source.
func TestHandleCloneAgent_RejectsSelfRename(t *testing.T) {
	_, ts := newTestServer(t)
	if err := agents.SaveAgentDefault(agents.AgentDef{Name: "Mira", Model: "haiku", Provider: "anthropic"}); err != nil {
		t.Fatal(err)
	}

	body := `{"new_name":"Mira"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/agents/Mira/clone", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("self-rename: want 400, got %d", resp.StatusCode)
	}
}

// TestHandleCloneAgent_NotFound returns 404 when the source doesn't exist.
func TestHandleCloneAgent_NotFound(t *testing.T) {
	_, ts := newTestServer(t)

	body := `{"new_name":"Anything"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/agents/missing/clone", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing source: want 404, got %d", resp.StatusCode)
	}
}
