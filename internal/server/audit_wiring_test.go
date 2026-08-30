package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAuditWiring_AgentCreateAndDelete_AppendsEntries verifies that creating
// (hiring) and deleting an agent through the real HTTP handlers appends
// entity-audit entries, and that GET /api/v1/audit surfaces them newest
// first.
func TestAuditWiring_AgentCreateAndDelete_AppendsEntries(t *testing.T) {
	t.Setenv("HUGINN_HOME", t.TempDir())
	srv := testServer(t)

	// Seed a second agent first so the one under test isn't "the last
	// agent" (handleDeleteAgent refuses to delete the last one).
	seedReq := httptest.NewRequest(http.MethodPut, "/api/v1/agents/AuditKeeperAgent",
		strings.NewReader(`{"name":"AuditKeeperAgent","model":"claude-sonnet-4-6"}`))
	seedReq.SetPathValue("name", "AuditKeeperAgent")
	seedW := httptest.NewRecorder()
	srv.handleUpdateAgent(seedW, seedReq)
	if seedW.Code != 200 {
		t.Fatalf("seed create: expected 200, got %d: %s", seedW.Code, seedW.Body.String())
	}

	// Create (hire) the agent under test via PUT.
	body := strings.NewReader(`{"name":"AuditTestAgent","model":"claude-sonnet-4-6"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/agents/AuditTestAgent", body)
	req.SetPathValue("name", "AuditTestAgent")
	w := httptest.NewRecorder()
	srv.handleUpdateAgent(w, req)
	if w.Code != 200 {
		t.Fatalf("create: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Delete it.
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/agents/AuditTestAgent", nil)
	req.SetPathValue("name", "AuditTestAgent")
	w = httptest.NewRecorder()
	srv.handleDeleteAgent(w, req)
	if w.Code != 200 {
		t.Fatalf("delete: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Read back via the audit API.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil)
	w = httptest.NewRecorder()
	srv.handleGetAudit(w, req)
	if w.Code != 200 {
		t.Fatalf("audit: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var out struct {
		Entries []entityAuditEntry `json:"entries"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Entries) < 2 {
		t.Fatalf("expected at least 2 audit entries, got %d: %+v", len(out.Entries), out.Entries)
	}
	// Newest first: delete should precede create.
	if out.Entries[0].Action != "agent_delete" {
		t.Errorf("entries[0].Action = %q, want agent_delete", out.Entries[0].Action)
	}
	if out.Entries[0].Actor != "user" {
		t.Errorf("entries[0].Actor = %q, want user", out.Entries[0].Actor)
	}
	foundCreate := false
	for _, e := range out.Entries {
		if e.Action == "agent_create" {
			foundCreate = true
		}
	}
	if !foundCreate {
		t.Error("expected an agent_create entry")
	}
}

// TestAuditWiring_CompanySeatUnseat_AppendsEntries verifies seat/unseat
// company-member actions append audit entries.
func TestAuditWiring_CompanySeatUnseat_AppendsEntries(t *testing.T) {
	srv, store := companyTestServer(t)
	co, err := store.CreateCompany("Lab", "", []string{}, "", "")
	if err != nil {
		t.Fatalf("CreateCompany: %v", err)
	}

	// Seat.
	body := strings.NewReader(`{"agent":"Winston"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/companies/"+co.ID+"/members", body)
	req.SetPathValue("id", co.ID)
	w := httptest.NewRecorder()
	srv.handleSeatCompanyMember(w, req)
	if w.Code != 200 {
		t.Fatalf("seat: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Unseat.
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/companies/"+co.ID+"/members/Winston", nil)
	req.SetPathValue("id", co.ID)
	req.SetPathValue("agent", "Winston")
	w = httptest.NewRecorder()
	srv.handleUnseatCompanyMember(w, req)
	if w.Code != 200 {
		t.Fatalf("unseat: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil)
	w = httptest.NewRecorder()
	srv.handleGetAudit(w, req)
	var out struct {
		Entries []entityAuditEntry `json:"entries"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var sawSeat, sawUnseat bool
	for _, e := range out.Entries {
		if e.Action == "member_seat" {
			sawSeat = true
		}
		if e.Action == "member_unseat" {
			sawUnseat = true
		}
	}
	if !sawSeat {
		t.Error("expected a member_seat entry")
	}
	if !sawUnseat {
		t.Error("expected a member_unseat entry")
	}
}

// TestAuditWiring_MemoryForget_Reachable covers the branch the implementer
// flagged as unverified. The full handleMuninnTool path cannot be driven from
// a unit test — memory.MCPURLFromEndpoint rewrites the endpoint to a FIXED
// MuninnDB port, so an httptest server on an ephemeral port is unreachable —
// so this asserts the two things that actually decide whether the audit line
// ever fires: (1) "muninn_forget" passes the allowlist gate that precedes it
// (otherwise the handler 403s and the audit line is dead code), and (2) a
// memory_forget entry written by the same helper the handler calls surfaces
// through GET /api/v1/audit with its vault detail intact.
func TestAuditWiring_MemoryForget_Reachable(t *testing.T) {
	if !allowedMuninnTools["muninn_forget"] {
		t.Fatal("muninn_forget is not in allowedMuninnTools: handleMuninnTool 403s before reaching the audit line, making it dead code")
	}

	t.Setenv("HUGINN_HOME", t.TempDir())
	srv := testServer(t)
	srv.logEntityAudit("memory_forget", "forgot memory in vault huginn:agent:mj:steve",
		map[string]any{"vault": "huginn:agent:mj:steve", "args": map[string]any{"id": "mem-1"}})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil)
	w := httptest.NewRecorder()
	srv.handleGetAudit(w, req)
	if w.Code != 200 {
		t.Fatalf("audit: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var out struct {
		Entries []entityAuditEntry `json:"entries"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, e := range out.Entries {
		if e.Action == "memory_forget" {
			found = true
			if e.Actor != "user" {
				t.Errorf("actor = %q, want user", e.Actor)
			}
			if e.Detail["vault"] != "huginn:agent:mj:steve" {
				t.Errorf("detail vault = %v, want the forgotten vault", e.Detail["vault"])
			}
		}
	}
	if !found {
		t.Fatalf("no memory_forget entry surfaced: %+v", out.Entries)
	}
}
