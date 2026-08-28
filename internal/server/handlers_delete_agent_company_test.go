package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/spaces"
)

// setupDeleteAgentCompanyServer wires a Server with both a real on-disk
// agents.json (handleDeleteAgent's DeleteAgentDefault/LoadAgents operate on
// disk, not the injected agentLoader) and a SQLite company store, isolated
// per test via HUGINN_HOME/HOME.
func setupDeleteAgentCompanyServer(t *testing.T, agentNames []string) *Server {
	t.Helper()
	fakeHome := t.TempDir()
	agentsDir := filepath.Join(fakeHome, ".huginn", "agents")
	if err := os.MkdirAll(agentsDir, 0o700); err != nil {
		t.Fatalf("create agents dir: %v", err)
	}
	for _, name := range agentNames {
		yaml := "name: " + name + "\nmodel: claude-sonnet-4-6\ncolor: \"#aabbcc\"\nicon: \"" + string(name[0]) + "\"\n"
		if err := os.WriteFile(filepath.Join(agentsDir, name+".yaml"), []byte(yaml), 0o600); err != nil {
			t.Fatalf("write agent yaml: %v", err)
		}
	}
	t.Setenv("HUGINN_HOME", fakeHome)
	t.Setenv("HOME", fakeHome)

	srv := testServer(t)
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := spaces.NewSQLiteSpaceStore(db)
	srv.SetSpaceStore(store)
	return srv
}

// TestHandleDeleteAgent_RemovesGhostCompanyMembership covers defect #3:
// DELETE /api/v1/agents/{name} used to leave the agent seated on every
// company roster it belonged to, so it stayed in GET /companies (and thus
// the UI rail) until someone manually unseated it.
func TestHandleDeleteAgent_RemovesGhostCompanyMembership(t *testing.T) {
	srv := setupDeleteAgentCompanyServer(t, []string{"Winston", "fableprobe"})
	cs := srv.companyAPI()
	if cs == nil {
		t.Fatal("company API not wired")
	}
	co, err := cs.CreateCompany("Acme", "acme-vault", []string{"Winston", "fableprobe"}, "A", "#111111")
	if err != nil {
		t.Fatalf("create company: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/v1/agents/{name}", srv.handleDeleteAgent)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/agents/fableprobe", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var result struct {
		Deleted bool `json:"deleted"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !result.Deleted {
		t.Fatal("expected deleted:true")
	}

	got, err := cs.GetCompany(co.ID)
	if err != nil {
		t.Fatalf("get company: %v", err)
	}
	for _, m := range got.Members {
		if m == "fableprobe" {
			t.Fatalf("ghost membership: fableprobe still a member: %v", got.Members)
		}
	}
}

// TestHandleDeleteAgent_BlocksDeletingCompanyLead covers the other half of
// defect #3's decision: deleting a company lead would orphan the company
// (no seated member left to reassign the lead to), so it must fail closed
// with an actionable 409 instead.
func TestHandleDeleteAgent_BlocksDeletingCompanyLead(t *testing.T) {
	srv := setupDeleteAgentCompanyServer(t, []string{"Winston", "fableprobe"})
	cs := srv.companyAPI()
	if cs == nil {
		t.Fatal("company API not wired")
	}
	co, err := cs.CreateCompany("Acme", "acme-vault", []string{"Winston", "fableprobe"}, "A", "#111111")
	if err != nil {
		t.Fatalf("create company: %v", err)
	}
	lead := "fableprobe"
	if _, err := cs.UpdateCompany(co.ID, spaces.CompanyUpdates{Lead: &lead}); err != nil {
		t.Fatalf("set lead: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/v1/agents/{name}", srv.handleDeleteAgent)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/agents/fableprobe", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 409 {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
	var body struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.Error == "" {
		t.Fatal("expected an actionable error message")
	}

	// The agent must still exist — it must not have been deleted.
	got, err := cs.GetCompany(co.ID)
	if err != nil {
		t.Fatalf("get company: %v", err)
	}
	if got.Lead != "fableprobe" {
		t.Fatalf("lead should be unchanged, got %q", got.Lead)
	}
	if len(got.Members) != 2 {
		t.Fatalf("members should be unchanged, got %v", got.Members)
	}
}
