package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/spaces"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func newSpaceTestServer(t *testing.T) (*Server, *spaces.SQLiteSpaceStore) {
	t.Helper()
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("migrate spaces: %v", err)
	}
	store := spaces.NewSQLiteSpaceStore(db)
	srv.SetSpaceStore(store)
	return srv, store
}

// ── handleDeleteSpace ─────────────────────────────────────────────────────────

func TestHandleDeleteSpace_DeletesChannel_Returns200(t *testing.T) {
	srv, store := newSpaceTestServer(t)

	ch, err := store.CreateChannel("Engineering", "atlas", []string{}, "", "")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/spaces/"+ch.ID, nil)
	req.SetPathValue("id", ch.ID)
	w := httptest.NewRecorder()
	srv.handleDeleteSpace(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]bool
	json.NewDecoder(w.Body).Decode(&body)
	if !body["ok"] {
		t.Errorf("expected ok=true, got %+v", body)
	}

	// Space should now be archived — verify by listing (archived spaces are excluded).
	all, _ := store.ListSpaces(spaces.ListOpts{})
	for _, s := range all.Spaces {
		if s.ID == ch.ID {
			t.Errorf("deleted space %q still present in ListSpaces", ch.ID)
		}
	}
}

func TestHandleDeleteSpace_DMSpace_Returns403(t *testing.T) {
	srv, store := newSpaceTestServer(t)

	dm, err := store.OpenDM("atlas")
	if err != nil {
		t.Fatalf("open dm: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/spaces/"+dm.ID, nil)
	req.SetPathValue("id", dm.ID)
	w := httptest.NewRecorder()
	srv.handleDeleteSpace(w, req)

	if w.Code != 403 {
		t.Errorf("expected 403 for DM delete, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDeleteSpace_UnknownSpace_Returns404(t *testing.T) {
	srv, _ := newSpaceTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/spaces/doesnotexist", nil)
	req.SetPathValue("id", "doesnotexist")
	w := httptest.NewRecorder()
	srv.handleDeleteSpace(w, req)

	if w.Code != 404 {
		t.Errorf("expected 404 for unknown space, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDeleteSpace_NoStore_Returns503(t *testing.T) {
	srv := testServer(t) // no store wired

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/spaces/any", nil)
	req.SetPathValue("id", "any")
	w := httptest.NewRecorder()
	srv.handleDeleteSpace(w, req)

	if w.Code != 503 {
		t.Errorf("expected 503 when store is nil, got %d", w.Code)
	}
}

// ── handleGetSpace ────────────────────────────────────────────────────────────

func TestHandleGetSpace_ExistingChannel_Returns200(t *testing.T) {
	srv, store := newSpaceTestServer(t)

	ch, err := store.CreateChannel("Product", "atlas", []string{}, "", "")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/spaces/"+ch.ID, nil)
	req.SetPathValue("id", ch.ID)
	w := httptest.NewRecorder()
	srv.handleGetSpace(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var sp spaces.Space
	json.NewDecoder(w.Body).Decode(&sp)
	if sp.ID != ch.ID {
		t.Errorf("expected ID %q, got %q", ch.ID, sp.ID)
	}
}

func TestHandleGetSpace_UnknownSpace_Returns404(t *testing.T) {
	srv, _ := newSpaceTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/spaces/ghost", nil)
	req.SetPathValue("id", "ghost")
	w := httptest.NewRecorder()
	srv.handleGetSpace(w, req)

	if w.Code != 404 {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// ── handleMarkSpaceRead ───────────────────────────────────────────────────────

func TestHandleMarkSpaceRead_ExistingSpace_Returns200(t *testing.T) {
	srv, store := newSpaceTestServer(t)

	ch, err := store.CreateChannel("Alerts", "atlas", []string{}, "", "")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/spaces/"+ch.ID+"/read", nil)
	req.SetPathValue("id", ch.ID)
	w := httptest.NewRecorder()
	srv.handleMarkSpaceRead(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleMarkSpaceRead_UnknownSpace_Returns404(t *testing.T) {
	srv, _ := newSpaceTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/spaces/ghost/read", nil)
	req.SetPathValue("id", "ghost")
	w := httptest.NewRecorder()
	srv.handleMarkSpaceRead(w, req)

	if w.Code != 404 {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleMarkSpaceRead_Idempotent(t *testing.T) {
	srv, store := newSpaceTestServer(t)

	ch, err := store.CreateChannel("Logs", "atlas", []string{}, "", "")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}

	for range 3 {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/spaces/"+ch.ID+"/read", nil)
		req.SetPathValue("id", ch.ID)
		w := httptest.NewRecorder()
		srv.handleMarkSpaceRead(w, req)
		if w.Code != 200 {
			t.Fatalf("expected 200 on idempotent mark read, got %d: %s", w.Code, w.Body.String())
		}
	}
}

// ── handleCreateSpace ─────────────────────────────────────────────────────────

func TestHandleCreateSpace_NoStore_Returns503(t *testing.T) {
	srv := testServer(t) // no store wired

	req := httptest.NewRequest(http.MethodPost, "/api/v1/spaces", nil)
	w := httptest.NewRecorder()
	srv.handleCreateSpace(w, req)

	if w.Code != 503 {
		t.Errorf("expected 503 when store is nil, got %d", w.Code)
	}
}

func TestHandleCreateSpace_DuplicateName_Returns409(t *testing.T) {
	srv, store := newSpaceTestServer(t)
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{
			{Name: "atlas", Model: "test"},
			{Name: "sam", Model: "test"},
		}}, nil
	}

	// Create first channel
	_, err := store.CreateChannel("Engineering", "atlas", []string{"sam"}, "", "")
	if err != nil {
		t.Fatalf("create first channel: %v", err)
	}

	// Try to create another channel with the same name
	body := `{"name":"Engineering","lead_agent":"atlas","member_agents":["sam"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spaces", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateSpace(w, req)

	if w.Code != 409 {
		t.Errorf("expected 409 for duplicate name, got %d: %s", w.Code, w.Body.String())
	}

	// Case-insensitive check
	body2 := `{"name":"engineering","lead_agent":"atlas","member_agents":["sam"]}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/spaces", strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	srv.handleCreateSpace(w2, req2)

	if w2.Code != 409 {
		t.Errorf("expected 409 for case-insensitive duplicate, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestHandleCreateSpace_UniqueName_Succeeds(t *testing.T) {
	srv, _ := newSpaceTestServer(t)
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return &agents.AgentsConfig{Agents: []agents.AgentDef{
			{Name: "atlas", Model: "test"},
		}}, nil
	}

	body := `{"name":"Engineering","lead_agent":"atlas","member_agents":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/spaces", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateSpace(w, req)

	if w.Code != 201 {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Second channel with different name should succeed
	body2 := `{"name":"Design","lead_agent":"atlas","member_agents":[]}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/spaces", strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	srv.handleCreateSpace(w2, req2)

	if w2.Code != 201 {
		t.Errorf("expected 201 for unique name, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestHandleGetSpace_NoStore_Returns503(t *testing.T) {
	srv := testServer(t) // no store wired

	req := httptest.NewRequest(http.MethodGet, "/api/v1/spaces/any", nil)
	req.SetPathValue("id", "any")
	w := httptest.NewRecorder()
	srv.handleGetSpace(w, req)

	if w.Code != 503 {
		t.Errorf("expected 503 when store is nil, got %d", w.Code)
	}
}
