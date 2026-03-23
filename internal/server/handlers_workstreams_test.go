package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// openWorkstreamTestServer creates a Server wired with a real in-process
// SQLite WorkstreamStore for handler tests.
func openWorkstreamTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlitedb.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	if err := db.Migrate(spaces.WorkstreamMigrations()); err != nil {
		t.Fatalf("migrate workstreams: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	store := spaces.NewWorkstreamStore(db)
	s := &Server{}
	s.workstreamStore = store
	return s
}

// ── handleCreateWorkstream ────────────────────────────────────────────────────

func TestHandleCreateWorkstream_Valid(t *testing.T) {
	s := openWorkstreamTestServer(t)

	body := `{"name":"ops-plan","description":"Q1 ops"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.handleCreateWorkstream(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["id"] == "" {
		t.Error("expected non-empty id in response")
	}
	if resp["name"] != "ops-plan" {
		t.Errorf("name = %v, want ops-plan", resp["name"])
	}
}

func TestHandleCreateWorkstream_MissingName_Returns400(t *testing.T) {
	s := openWorkstreamTestServer(t)

	body := `{"description":"no name"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.handleCreateWorkstream(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleCreateWorkstream_NilStore_Returns503(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams", bytes.NewBufferString(`{"name":"x"}`))
	w := httptest.NewRecorder()
	s.handleCreateWorkstream(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

// ── handleListWorkstreams ─────────────────────────────────────────────────────

func TestHandleListWorkstreams_Empty(t *testing.T) {
	s := openWorkstreamTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workstreams", nil)
	w := httptest.NewRecorder()
	s.handleListWorkstreams(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var list []any
	json.NewDecoder(w.Body).Decode(&list)
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}
}

func TestHandleListWorkstreams_AfterCreate(t *testing.T) {
	s := openWorkstreamTestServer(t)

	// Create two workstreams
	for _, name := range []string{"ws-a", "ws-b"} {
		body := `{"name":"` + name + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handleCreateWorkstream(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %q: status %d", name, w.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workstreams", nil)
	w := httptest.NewRecorder()
	s.handleListWorkstreams(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var list []any
	json.NewDecoder(w.Body).Decode(&list)
	if len(list) != 2 {
		t.Errorf("expected 2 workstreams, got %d", len(list))
	}
}

func TestHandleListWorkstreams_NilStore_Returns503(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/workstreams", nil)
	w := httptest.NewRecorder()
	s.handleListWorkstreams(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

// ── handleGetWorkstream ───────────────────────────────────────────────────────

func TestHandleGetWorkstream_Found(t *testing.T) {
	s := openWorkstreamTestServer(t)

	// Create
	body := `{"name":"project-delta"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.handleCreateWorkstream(w, req)
	var created map[string]any
	json.NewDecoder(w.Body).Decode(&created)
	id := created["id"].(string)

	// Get
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/workstreams/"+id, nil)
	req2.SetPathValue("id", id)
	w2 := httptest.NewRecorder()
	s.handleGetWorkstream(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w2.Code, w2.Body.String())
	}
	var got map[string]any
	json.NewDecoder(w2.Body).Decode(&got)
	if got["name"] != "project-delta" {
		t.Errorf("name = %v, want project-delta", got["name"])
	}
}

func TestHandleGetWorkstream_NotFound_Returns404(t *testing.T) {
	s := openWorkstreamTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workstreams/nonexistent", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()
	s.handleGetWorkstream(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ── handleDeleteWorkstream ────────────────────────────────────────────────────

func TestHandleDeleteWorkstream_Valid(t *testing.T) {
	s := openWorkstreamTestServer(t)

	// Create
	body := `{"name":"to-delete"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	s.handleCreateWorkstream(w, req)
	var created map[string]any
	json.NewDecoder(w.Body).Decode(&created)
	id := created["id"].(string)

	// Delete
	req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/workstreams/"+id, nil)
	req2.SetPathValue("id", id)
	w2 := httptest.NewRecorder()
	s.handleDeleteWorkstream(w2, req2)

	if w2.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w2.Code)
	}

	// Verify gone
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/workstreams/"+id, nil)
	req3.SetPathValue("id", id)
	w3 := httptest.NewRecorder()
	s.handleGetWorkstream(w3, req3)
	if w3.Code != http.StatusNotFound {
		t.Errorf("after delete, get returned %d, want 404", w3.Code)
	}
}

func TestHandleDeleteWorkstream_NotFound_Returns404(t *testing.T) {
	s := openWorkstreamTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/workstreams/ghost", nil)
	req.SetPathValue("id", "ghost")
	w := httptest.NewRecorder()
	s.handleDeleteWorkstream(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ── handleTagWorkstreamSession ────────────────────────────────────────────────

func TestHandleTagWorkstreamSession_Valid(t *testing.T) {
	s := openWorkstreamTestServer(t)

	// Create workstream
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams", bytes.NewBufferString(`{"name":"tag-test"}`))
	w := httptest.NewRecorder()
	s.handleCreateWorkstream(w, req)
	var ws map[string]any
	json.NewDecoder(w.Body).Decode(&ws)
	id := ws["id"].(string)

	// Tag session
	tagBody := `{"session_id":"sess-42"}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams/"+id+"/sessions", bytes.NewBufferString(tagBody))
	req2.SetPathValue("id", id)
	w2 := httptest.NewRecorder()
	s.handleTagWorkstreamSession(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w2.Code, w2.Body.String())
	}
}

func TestHandleTagWorkstreamSession_MissingSessionID_Returns400(t *testing.T) {
	s := openWorkstreamTestServer(t)

	// Create workstream
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams", bytes.NewBufferString(`{"name":"x"}`))
	w := httptest.NewRecorder()
	s.handleCreateWorkstream(w, req)
	var ws map[string]any
	json.NewDecoder(w.Body).Decode(&ws)
	id := ws["id"].(string)

	// Tag with no session_id
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams/"+id+"/sessions", bytes.NewBufferString(`{}`))
	req2.SetPathValue("id", id)
	w2 := httptest.NewRecorder()
	s.handleTagWorkstreamSession(w2, req2)

	if w2.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w2.Code)
	}
}

// ── handleListWorkstreamSessions ──────────────────────────────────────────────

func TestHandleListWorkstreamSessions_Empty(t *testing.T) {
	s := openWorkstreamTestServer(t)

	// Create workstream
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams", bytes.NewBufferString(`{"name":"empty-ws"}`))
	w := httptest.NewRecorder()
	s.handleCreateWorkstream(w, req)
	var ws map[string]any
	json.NewDecoder(w.Body).Decode(&ws)
	id := ws["id"].(string)

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/workstreams/"+id+"/sessions", nil)
	req2.SetPathValue("id", id)
	w2 := httptest.NewRecorder()
	s.handleListWorkstreamSessions(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w2.Code)
	}
	var list []any
	json.NewDecoder(w2.Body).Decode(&list)
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}
}

func TestHandleListWorkstreamSessions_AfterTag(t *testing.T) {
	s := openWorkstreamTestServer(t)

	// Create workstream
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams", bytes.NewBufferString(`{"name":"tagged-ws"}`))
	w := httptest.NewRecorder()
	s.handleCreateWorkstream(w, req)
	var ws map[string]any
	json.NewDecoder(w.Body).Decode(&ws)
	id := ws["id"].(string)

	// Tag two sessions
	for _, sessID := range []string{"sess-1", "sess-2"} {
		tagBody := `{"session_id":"` + sessID + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams/"+id+"/sessions", bytes.NewBufferString(tagBody))
		req.SetPathValue("id", id)
		w := httptest.NewRecorder()
		s.handleTagWorkstreamSession(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("tag %q: status %d", sessID, w.Code)
		}
	}

	// List sessions
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/workstreams/"+id+"/sessions", nil)
	req2.SetPathValue("id", id)
	w2 := httptest.NewRecorder()
	s.handleListWorkstreamSessions(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w2.Code)
	}
	var list []any
	json.NewDecoder(w2.Body).Decode(&list)
	if len(list) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(list))
	}
}
