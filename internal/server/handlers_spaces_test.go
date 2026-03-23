package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// testServer is a convenience wrapper around newTestServer that returns only
// the *Server (without the httptest.Server) for handler-level unit tests that
// call handler methods directly via httptest.NewRecorder.
func testServer(t *testing.T) *Server {
	t.Helper()
	srv, _ := newTestServer(t)
	return srv
}

// openTestSQLiteDB opens a fresh in-memory SQLite DB for tests,
// applying the base schema and all session migrations (including the
// thread-columns migration that adds parent_message_id, etc.).
func openTestSQLiteDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlitedb.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("openTestSQLiteDB: open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("openTestSQLiteDB: apply schema: %v", err)
	}
	if err := db.Migrate(session.Migrations()); err != nil {
		t.Fatalf("openTestSQLiteDB: migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestHandleListSpaces_Empty(t *testing.T) {
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := spaces.NewSQLiteSpaceStore(db)
	srv.SetSpaceStore(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/spaces", nil)
	w := httptest.NewRecorder()
	srv.handleListSpaces(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result spaces.ListSpacesResult
	json.NewDecoder(w.Body).Decode(&result)
	if result.Spaces == nil {
		t.Error("expected non-nil spaces list")
	}
}

func TestHandleGetOrCreateDM_Idempotent(t *testing.T) {
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := spaces.NewSQLiteSpaceStore(db)
	srv.SetSpaceStore(store)

	doRequest := func() spaces.Space {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/spaces/dm/atlas", nil)
		req.SetPathValue("agent", "atlas")
		w := httptest.NewRecorder()
		srv.handleGetOrCreateDM(w, req)
		if w.Code != 200 {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var sp spaces.Space
		json.NewDecoder(w.Body).Decode(&sp)
		return sp
	}

	sp1 := doRequest()
	sp2 := doRequest()
	if sp1.ID != sp2.ID {
		t.Errorf("idempotency broken: %q vs %q", sp1.ID, sp2.ID)
	}
	if sp1.Kind != "dm" {
		t.Errorf("expected dm, got %q", sp1.Kind)
	}
}

func TestHandleUpdateSpace_DM_Returns403(t *testing.T) {
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := spaces.NewSQLiteSpaceStore(db)
	dm, _ := store.OpenDM("atlas")
	srv.SetSpaceStore(store)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/spaces/"+dm.ID, nil)
	req.SetPathValue("id", dm.ID)
	w := httptest.NewRecorder()
	srv.handleUpdateSpace(w, req)
	if w.Code != 403 {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestHandleUpdateSpace_EmptyName_Returns400(t *testing.T) {
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := spaces.NewSQLiteSpaceStore(db)
	ch, _ := store.CreateChannel("Team", "atlas", []string{}, "", "")
	srv.SetSpaceStore(store)

	body := strings.NewReader(`{"name":""}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/spaces/"+ch.ID, body)
	req.SetPathValue("id", ch.ID)
	w := httptest.NewRecorder()
	srv.handleUpdateSpace(w, req)
	if w.Code != 400 {
		t.Errorf("expected 400 for empty name, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdateSpace_WhitespaceName_Returns400(t *testing.T) {
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := spaces.NewSQLiteSpaceStore(db)
	ch, _ := store.CreateChannel("Team", "atlas", []string{}, "", "")
	srv.SetSpaceStore(store)

	body := strings.NewReader(`{"name":"   "}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/spaces/"+ch.ID, body)
	req.SetPathValue("id", ch.ID)
	w := httptest.NewRecorder()
	srv.handleUpdateSpace(w, req)
	if w.Code != 400 {
		t.Errorf("expected 400 for whitespace name, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleListSpaceSessions_UnknownSpace_Returns404(t *testing.T) {
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := spaces.NewSQLiteSpaceStore(db)
	srv.SetSpaceStore(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/spaces/doesnotexist/sessions", nil)
	req.SetPathValue("id", "doesnotexist")
	w := httptest.NewRecorder()
	srv.handleListSpaceSessions(w, req)
	if w.Code != 404 {
		t.Errorf("expected 404 for unknown space, got %d: %s", w.Code, w.Body.String())
	}
}
