package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

// TestPersistAgent_PersonalityAndVetWorkRoundTrip fails without the
// feature: before AgentDef.Personality/VetWork existed, a PUT carrying
// these fields would silently drop them (unknown JSON fields are ignored)
// and a subsequent GET would never surface them.
func TestPersistAgent_PersonalityAndVetWorkRoundTrip(t *testing.T) {
	setupAgentsDir(t, map[string]string{})
	srv, ts := newTestServer(t)
	_ = srv

	body := `{"name":"ReviewBot","model":"qwen2.5-coder:14b","personality":"strict-reviewer"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/ReviewBot", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", resp.StatusCode)
	}

	cfg, err := agents.LoadAgents()
	if err != nil {
		t.Fatal(err)
	}
	var got agents.AgentDef
	found := false
	for _, a := range cfg.Agents {
		if a.Name == "ReviewBot" {
			got = a
			found = true
		}
	}
	if !found {
		t.Fatal("agent not persisted")
	}
	if got.Personality != "strict-reviewer" {
		t.Errorf("Personality = %q, want strict-reviewer", got.Personality)
	}
	// vet_work was never sent explicitly — FromDef must resolve it to true
	// via the strict-reviewer default, not persist a raw nil forever.
	runtime := agents.FromDef(got)
	if !runtime.VetWork {
		t.Errorf("resolved VetWork = false, want true (strict-reviewer default, no override)")
	}

	// Now explicitly override vet_work to false — the override must win.
	body2 := `{"name":"ReviewBot","model":"qwen2.5-coder:14b","personality":"strict-reviewer","vet_work":false}`
	req2, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/ReviewBot", strings.NewReader(body2))
	req2.Header.Set("Authorization", "Bearer "+testToken)
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("PUT (override) status = %d, want 200", resp2.StatusCode)
	}
	cfg2, err := agents.LoadAgents()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range cfg2.Agents {
		if a.Name == "ReviewBot" {
			if a.VetWork == nil || *a.VetWork != false {
				t.Fatalf("VetWork override not persisted: %+v", a.VetWork)
			}
			runtime2 := agents.FromDef(a)
			if runtime2.VetWork {
				t.Errorf("resolved VetWork = true, want false (explicit override should win over preset default)")
			}
		}
	}
}

func TestPersistAgent_RejectsInvalidPersonality(t *testing.T) {
	setupAgentsDir(t, map[string]string{})
	_, ts := newTestServer(t)

	body := `{"name":"BadBot","model":"qwen2.5-coder:14b","personality":"not-a-real-preset"}`
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/agents/BadBot", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("PUT status = %d, want 422 for invalid personality", resp.StatusCode)
	}
}
