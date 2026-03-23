package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/spaces"
)

// newWorkstreamR2Server sets up a Server with a live WorkstreamStore backed
// by an in-memory SQLite DB using the openTestSQLiteDB helper.
func newWorkstreamR2Server(t *testing.T) *Server {
	t.Helper()
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.WorkstreamMigrations()); err != nil {
		t.Fatalf("migrate workstreams: %v", err)
	}
	store := spaces.NewWorkstreamStore(db)
	srv.SetWorkstreamStore(store)
	return srv
}

// createR2Workstream is a helper that creates a workstream and returns its ID.
func createR2Workstream(t *testing.T, srv *Server, name string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateWorkstream(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("createR2Workstream %q: expected 201, got %d: %s", name, w.Code, w.Body.String())
	}
	var ws map[string]any
	json.NewDecoder(w.Body).Decode(&ws)
	return ws["id"].(string)
}

// ── handleListWorkstreams additional coverage ──────────────────────────────────

func TestHandleListWorkstreams_R2_ContentType(t *testing.T) {
	srv := newWorkstreamR2Server(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workstreams", nil)
	w := httptest.NewRecorder()
	srv.handleListWorkstreams(w, req)

	ct := w.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected application/json Content-Type, got %q", ct)
	}
}

func TestHandleListWorkstreams_R2_ThreeItems(t *testing.T) {
	srv := newWorkstreamR2Server(t)

	for _, name := range []string{"Alpha", "Beta", "Gamma"} {
		createR2Workstream(t, srv, name)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workstreams", nil)
	w := httptest.NewRecorder()
	srv.handleListWorkstreams(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result []*spaces.Workstream
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("expected 3 workstreams, got %d", len(result))
	}
}

// ── handleCreateWorkstream additional coverage ─────────────────────────────────

func TestHandleCreateWorkstream_R2_InvalidJSON_Returns400(t *testing.T) {
	srv := newWorkstreamR2Server(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams", strings.NewReader("{not valid json}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateWorkstream(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateWorkstream_R2_EmptyBody_Returns400(t *testing.T) {
	srv := newWorkstreamR2Server(t)

	// Empty body → EOF decode error → 400 or name="" → 400.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateWorkstream(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty body, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateWorkstream_R2_ResponseHasAllFields(t *testing.T) {
	srv := newWorkstreamR2Server(t)

	body, _ := json.Marshal(map[string]string{
		"name":        "Field Check",
		"description": "checking fields",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateWorkstream(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var ws map[string]any
	if err := json.NewDecoder(w.Body).Decode(&ws); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, field := range []string{"id", "name", "description", "created_at", "updated_at"} {
		if _, ok := ws[field]; !ok {
			t.Errorf("response missing field %q; keys: %v", field, ws)
		}
	}
}

func TestHandleCreateWorkstream_R2_NilStore_Returns503(t *testing.T) {
	// Use a bare server with no workstream store to exercise the nil guard via
	// the full testServer path (which has a real store, nil by default for WS).
	srv := testServer(t)
	// srv.workstreamStore is nil — do NOT set it.

	body, _ := json.Marshal(map[string]string{"name": "Test"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateWorkstream(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

// ── handleGetWorkstream additional coverage ────────────────────────────────────

func TestHandleGetWorkstream_R2_NilStore_Returns503(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workstreams/some-id", nil)
	req.SetPathValue("id", "some-id")
	w := httptest.NewRecorder()
	srv.handleGetWorkstream(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetWorkstream_R2_VerifiesDescription(t *testing.T) {
	srv := newWorkstreamR2Server(t)

	body, _ := json.Marshal(map[string]string{
		"name":        "Described WS",
		"description": "desc value",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateWorkstream(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created map[string]any
	json.NewDecoder(w.Body).Decode(&created)
	id := created["id"].(string)

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/workstreams/"+id, nil)
	req2.SetPathValue("id", id)
	w2 := httptest.NewRecorder()
	srv.handleGetWorkstream(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var ws map[string]any
	json.NewDecoder(w2.Body).Decode(&ws)
	if ws["description"] != "desc value" {
		t.Errorf("expected description %q, got %v", "desc value", ws["description"])
	}
}

// ── handleDeleteWorkstream additional coverage ─────────────────────────────────

func TestHandleDeleteWorkstream_R2_NilStore_Returns503(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/workstreams/some-id", nil)
	req.SetPathValue("id", "some-id")
	w := httptest.NewRecorder()
	srv.handleDeleteWorkstream(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDeleteWorkstream_R2_IdempotentDelete_SecondReturns404(t *testing.T) {
	srv := newWorkstreamR2Server(t)

	id := createR2Workstream(t, srv, "Once Only")

	// First delete: 204.
	req1 := httptest.NewRequest(http.MethodDelete, "/api/v1/workstreams/"+id, nil)
	req1.SetPathValue("id", id)
	w1 := httptest.NewRecorder()
	srv.handleDeleteWorkstream(w1, req1)
	if w1.Code != http.StatusNoContent {
		t.Fatalf("first delete: expected 204, got %d: %s", w1.Code, w1.Body.String())
	}

	// Second delete: 404.
	req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/workstreams/"+id, nil)
	req2.SetPathValue("id", id)
	w2 := httptest.NewRecorder()
	srv.handleDeleteWorkstream(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Fatalf("second delete: expected 404, got %d: %s", w2.Code, w2.Body.String())
	}
}

// ── handleTagWorkstreamSession additional coverage ────────────────────────────

func TestHandleTagWorkstreamSession_R2_NilStore_Returns503(t *testing.T) {
	srv := testServer(t)

	body, _ := json.Marshal(map[string]string{"session_id": "sess-1"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams/ws-1/sessions", bytes.NewReader(body))
	req.SetPathValue("id", "ws-1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleTagWorkstreamSession(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleTagWorkstreamSession_R2_InvalidJSON_Returns400(t *testing.T) {
	srv := newWorkstreamR2Server(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams/ws-x/sessions", strings.NewReader("not-json"))
	req.SetPathValue("id", "ws-x")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleTagWorkstreamSession(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleTagWorkstreamSession_R2_ResponseBody(t *testing.T) {
	srv := newWorkstreamR2Server(t)
	wsID := createR2Workstream(t, srv, "Response Body WS")

	tagBody, _ := json.Marshal(map[string]string{"session_id": "my-session"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams/"+wsID+"/sessions", bytes.NewReader(tagBody))
	req.SetPathValue("id", wsID)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleTagWorkstreamSession(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["workstream_id"] != wsID {
		t.Errorf("expected workstream_id %q, got %q", wsID, resp["workstream_id"])
	}
	if resp["session_id"] != "my-session" {
		t.Errorf("expected session_id %q, got %q", "my-session", resp["session_id"])
	}
}

func TestHandleTagWorkstreamSession_R2_Idempotent(t *testing.T) {
	srv := newWorkstreamR2Server(t)
	wsID := createR2Workstream(t, srv, "Idempotent WS")

	// Tag the same session twice — both should succeed (INSERT OR IGNORE).
	for i := 0; i < 2; i++ {
		tagBody, _ := json.Marshal(map[string]string{"session_id": "dup-session"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams/"+wsID+"/sessions", bytes.NewReader(tagBody))
		req.SetPathValue("id", wsID)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.handleTagWorkstreamSession(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("tag attempt %d: expected 200, got %d: %s", i+1, w.Code, w.Body.String())
		}
	}

	// Only one entry should exist.
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/workstreams/"+wsID+"/sessions", nil)
	listReq.SetPathValue("id", wsID)
	listW := httptest.NewRecorder()
	srv.handleListWorkstreamSessions(listW, listReq)

	var ids []string
	json.NewDecoder(listW.Body).Decode(&ids)
	if len(ids) != 1 {
		t.Errorf("expected 1 session after idempotent tag, got %d", len(ids))
	}
}

// ── handleListWorkstreamSessions additional coverage ──────────────────────────

func TestHandleListWorkstreamSessions_R2_NilStore_Returns503(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workstreams/ws-1/sessions", nil)
	req.SetPathValue("id", "ws-1")
	w := httptest.NewRecorder()
	srv.handleListWorkstreamSessions(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleListWorkstreamSessions_R2_ThreeSessions(t *testing.T) {
	srv := newWorkstreamR2Server(t)
	wsID := createR2Workstream(t, srv, "Three Sessions WS")

	sessionIDs := []string{"sess-a", "sess-b", "sess-c"}
	for _, sid := range sessionIDs {
		tagBody, _ := json.Marshal(map[string]string{"session_id": sid})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/workstreams/"+wsID+"/sessions", bytes.NewReader(tagBody))
		req.SetPathValue("id", wsID)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.handleTagWorkstreamSession(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("tag %q: expected 200, got %d: %s", sid, w.Code, w.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/workstreams/"+wsID+"/sessions", nil)
	req.SetPathValue("id", wsID)
	w := httptest.NewRecorder()
	srv.handleListWorkstreamSessions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var ids []string
	if err := json.NewDecoder(w.Body).Decode(&ids); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(ids) != 3 {
		t.Errorf("expected 3 sessions, got %d", len(ids))
	}
}

// ── full HTTP integration (auth + routing) ────────────────────────────────────

func TestWorkstreams_R2_ViaHTTP_ListEmpty(t *testing.T) {
	srv, ts := newTestServer(t)
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.WorkstreamMigrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	srv.SetWorkstreamStore(spaces.NewWorkstreamStore(db))

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workstreams", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body []map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	if body == nil {
		t.Error("expected non-nil array")
	}
}

func TestWorkstreams_R2_ViaHTTP_Unauthenticated(t *testing.T) {
	_, ts := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/v1/workstreams")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestWorkstreams_R2_ViaHTTP_CreateAndGet(t *testing.T) {
	srv, ts := newTestServer(t)
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.WorkstreamMigrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	srv.SetWorkstreamStore(spaces.NewWorkstreamStore(db))

	// Create.
	createBody, _ := json.Marshal(map[string]string{"name": "HTTP WS"})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workstreams", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", resp.StatusCode)
	}
	var ws map[string]any
	json.NewDecoder(resp.Body).Decode(&ws)
	wsID, _ := ws["id"].(string)
	if wsID == "" {
		t.Fatal("create: expected non-empty id")
	}

	// Get.
	req2, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/workstreams/"+wsID, nil)
	req2.Header.Set("Authorization", "Bearer "+testToken)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", resp2.StatusCode)
	}
}

func TestWorkstreams_R2_ViaHTTP_Delete(t *testing.T) {
	srv, ts := newTestServer(t)
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.WorkstreamMigrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	srv.SetWorkstreamStore(spaces.NewWorkstreamStore(db))

	// Create.
	createBody, _ := json.Marshal(map[string]string{"name": "Delete Me"})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/workstreams", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var ws map[string]any
	json.NewDecoder(resp.Body).Decode(&ws)
	wsID, _ := ws["id"].(string)

	// Delete.
	req2, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/workstreams/"+wsID, nil)
	req2.Header.Set("Authorization", "Bearer "+testToken)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", resp2.StatusCode)
	}
}
