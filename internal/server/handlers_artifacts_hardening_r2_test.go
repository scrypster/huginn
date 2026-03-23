package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/workforce"
)

// ── in-memory artifact store for testing ─────────────────────────────────────

type memArtifactStore struct {
	artifacts map[string]*workforce.Artifact
	writeErr  error
	readErr   error
	listErr   error
	updateErr error
}

func newMemArtifactStore() *memArtifactStore {
	return &memArtifactStore{artifacts: make(map[string]*workforce.Artifact)}
}

func (m *memArtifactStore) Write(_ context.Context, a *workforce.Artifact) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	if a.ID == "" {
		a.ID = "mem-" + a.Title + "-" + strconv.Itoa(len(m.artifacts))
	}
	cp := *a
	m.artifacts[a.ID] = &cp
	return nil
}

func (m *memArtifactStore) Read(_ context.Context, id string) (*workforce.Artifact, error) {
	if m.readErr != nil {
		return nil, m.readErr
	}
	a, ok := m.artifacts[id]
	if !ok {
		return nil, workforce.ErrArtifactNotFound
	}
	cp := *a
	return &cp, nil
}

func (m *memArtifactStore) ReadMetaOnly(_ context.Context, id string) (*workforce.Artifact, error) {
	if m.readErr != nil {
		return nil, m.readErr
	}
	a, ok := m.artifacts[id]
	if !ok {
		return nil, workforce.ErrArtifactNotFound
	}
	cp := *a
	cp.Content = nil
	return &cp, nil
}

func (m *memArtifactStore) ListBySession(_ context.Context, sessionID string, limit int, afterID string) ([]*workforce.Artifact, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var out []*workforce.Artifact
	for _, a := range m.artifacts {
		if a.SessionID != sessionID {
			continue
		}
		if afterID != "" && a.ID <= afterID {
			continue
		}
		cp := *a
		out = append(out, &cp)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memArtifactStore) ListByAgent(_ context.Context, agentName string, since time.Time, limit int, afterID string) ([]*workforce.Artifact, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var out []*workforce.Artifact
	for _, a := range m.artifacts {
		if a.AgentName != agentName {
			continue
		}
		if a.CreatedAt.Before(since) {
			continue
		}
		if afterID != "" && a.ID <= afterID {
			continue
		}
		cp := *a
		out = append(out, &cp)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memArtifactStore) UpdateStatus(_ context.Context, id string, status workforce.ArtifactStatus, reason string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	a, ok := m.artifacts[id]
	if !ok {
		return workforce.ErrArtifactNotFound
	}
	a.Status = status
	a.RejectionReason = reason
	return nil
}

func (m *memArtifactStore) OpenContent(_ context.Context, id string) (io.ReadCloser, error) {
	a, ok := m.artifacts[id]
	if !ok || a.ContentRef == "" {
		return nil, workforce.ErrArtifactNotFound
	}
	return io.NopCloser(strings.NewReader(string(a.Content))), nil
}

// ── helper to create a session in the server store ───────────────────────────

func makeSessionInStore(t *testing.T, srv *Server) *session.Session {
	t.Helper()
	sessDir := t.TempDir()
	store := session.NewStore(sessDir)
	sess := store.New("test-session", "/tmp", "test-model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	srv.store = store
	return sess
}

// ── session-scoped artifact CRUD ──────────────────────────────────────────────

func TestHandleListArtifacts_SessionNotFound(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/missing/artifacts", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	srv.handleListArtifacts(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleListArtifacts_EmptyForExistingSession(t *testing.T) {
	srv := testServer(t)
	sess := makeSessionInStore(t, srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sess.ID+"/artifacts", nil)
	req.SetPathValue("id", sess.ID)
	w := httptest.NewRecorder()
	srv.handleListArtifacts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result []artifactSummary
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 artifacts, got %d", len(result))
	}
}

func TestHandleListArtifacts_MissingSessionIDParam(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions//artifacts", nil)
	w := httptest.NewRecorder()
	srv.handleListArtifacts(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateArtifact_SessionNotFound(t *testing.T) {
	srv := testServer(t)
	body := `{"title":"report","kind":"document"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/missing/artifacts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	srv.handleCreateArtifact(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateArtifact_InvalidJSON(t *testing.T) {
	srv := testServer(t)
	sess := makeSessionInStore(t, srv)
	srv.SetArtifactStore(newMemArtifactStore())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+sess.ID+"/artifacts", strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", sess.ID)
	w := httptest.NewRecorder()
	srv.handleCreateArtifact(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateArtifact_MissingRequiredFields(t *testing.T) {
	srv := testServer(t)
	sess := makeSessionInStore(t, srv)
	srv.SetArtifactStore(newMemArtifactStore())

	// title is empty — required
	body := `{"kind":"document"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+sess.ID+"/artifacts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", sess.ID)
	w := httptest.NewRecorder()
	srv.handleCreateArtifact(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing title, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateArtifact_MissingTitle(t *testing.T) {
	srv := testServer(t)
	sess := makeSessionInStore(t, srv)
	srv.SetArtifactStore(newMemArtifactStore())

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+sess.ID+"/artifacts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", sess.ID)
	w := httptest.NewRecorder()
	srv.handleCreateArtifact(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty title, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateArtifact_Success(t *testing.T) {
	srv := testServer(t)
	sess := makeSessionInStore(t, srv)
	store := newMemArtifactStore()
	srv.SetArtifactStore(store)

	body := `{"title":"output.txt","kind":"document","mime_type":"text/plain"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+sess.ID+"/artifacts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", sess.ID)
	w := httptest.NewRecorder()
	srv.handleCreateArtifact(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["id"] == "" {
		t.Error("expected non-empty artifact ID in response")
	}
}

func TestHandleCreateArtifact_SetsSessionID(t *testing.T) {
	srv := testServer(t)
	sess := makeSessionInStore(t, srv)
	store := newMemArtifactStore()
	srv.SetArtifactStore(store)

	body := `{"title":"file.txt","kind":"document"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+sess.ID+"/artifacts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", sess.ID)
	w := httptest.NewRecorder()
	srv.handleCreateArtifact(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp) //nolint:errcheck
	id := resp["id"]
	if id == "" {
		t.Fatal("no id in response")
	}
	art, ok := store.artifacts[id]
	if !ok {
		t.Fatal("artifact not found in store")
	}
	if art.SessionID != sess.ID {
		t.Errorf("expected session_id %q, got %q", sess.ID, art.SessionID)
	}
}

func TestHandleGetArtifact_SessionNotFound(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/missing/artifacts/art-1", nil)
	req.SetPathValue("id", "missing")
	req.SetPathValue("artifact_id", "art-1")
	w := httptest.NewRecorder()
	srv.handleGetArtifact(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetArtifact_ArtifactNotFound(t *testing.T) {
	srv := testServer(t)
	sess := makeSessionInStore(t, srv)
	srv.SetArtifactStore(newMemArtifactStore())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sess.ID+"/artifacts/nonexistent", nil)
	req.SetPathValue("id", sess.ID)
	req.SetPathValue("artifact_id", "nonexistent")
	w := httptest.NewRecorder()
	srv.handleGetArtifact(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleGetArtifact_MissingParams(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions//artifacts/", nil)
	w := httptest.NewRecorder()
	srv.handleGetArtifact(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdateArtifact_SessionNotFound(t *testing.T) {
	srv := testServer(t)
	body := `{"status":"accepted"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/sessions/missing/artifacts/art-1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "missing")
	req.SetPathValue("artifact_id", "art-1")
	w := httptest.NewRecorder()
	srv.handleUpdateArtifact(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdateArtifact_InvalidJSON(t *testing.T) {
	srv := testServer(t)
	sess := makeSessionInStore(t, srv)
	store := newMemArtifactStore()
	srv.SetArtifactStore(store)
	// Pre-seed artifact so ReadMetaOnly passes before body decode
	store.artifacts["art-1"] = &workforce.Artifact{
		ID: "art-1", SessionID: sess.ID, Status: workforce.StatusDraft,
	}

	req := httptest.NewRequest(http.MethodPut, "/api/v1/sessions/"+sess.ID+"/artifacts/art-1", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", sess.ID)
	req.SetPathValue("artifact_id", "art-1")
	w := httptest.NewRecorder()
	srv.handleUpdateArtifact(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdateArtifact_ArtifactNotFound(t *testing.T) {
	srv := testServer(t)
	sess := makeSessionInStore(t, srv)
	srv.SetArtifactStore(newMemArtifactStore())

	body := `{"status":"accepted"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/sessions/"+sess.ID+"/artifacts/art-1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", sess.ID)
	req.SetPathValue("artifact_id", "art-1")
	w := httptest.NewRecorder()
	srv.handleUpdateArtifact(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleUpdateArtifact_MissingParams(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/sessions//artifacts/", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleUpdateArtifact(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDeleteArtifact_SessionNotFound(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/missing/artifacts/art-1", nil)
	req.SetPathValue("id", "missing")
	req.SetPathValue("artifact_id", "art-1")
	w := httptest.NewRecorder()
	srv.handleDeleteArtifact(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDeleteArtifact_Success(t *testing.T) {
	srv := testServer(t)
	sess := makeSessionInStore(t, srv)
	store := newMemArtifactStore()
	srv.SetArtifactStore(store)
	store.artifacts["art-1"] = &workforce.Artifact{
		ID: "art-1", SessionID: sess.ID, Status: workforce.StatusDraft,
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions/"+sess.ID+"/artifacts/art-1", nil)
	req.SetPathValue("id", sess.ID)
	req.SetPathValue("artifact_id", "art-1")
	w := httptest.NewRecorder()
	srv.handleDeleteArtifact(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
	if store.artifacts["art-1"].Status != workforce.StatusDeleted {
		t.Errorf("expected status deleted, got %q", store.artifacts["art-1"].Status)
	}
}

func TestHandleDeleteArtifact_MissingParams(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/sessions//artifacts/", nil)
	w := httptest.NewRecorder()
	srv.handleDeleteArtifact(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDownloadArtifact_SessionNotFound(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/missing/artifacts/art-1/download", nil)
	req.SetPathValue("id", "missing")
	req.SetPathValue("artifact_id", "art-1")
	w := httptest.NewRecorder()
	srv.handleDownloadArtifact(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDownloadArtifact_ArtifactNotFound(t *testing.T) {
	srv := testServer(t)
	sess := makeSessionInStore(t, srv)
	srv.SetArtifactStore(newMemArtifactStore())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sess.ID+"/artifacts/missing/download", nil)
	req.SetPathValue("id", sess.ID)
	req.SetPathValue("artifact_id", "missing")
	w := httptest.NewRecorder()
	srv.handleDownloadArtifact(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDownloadArtifact_MissingParams(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions//artifacts//download", nil)
	w := httptest.NewRecorder()
	srv.handleDownloadArtifact(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ── Workforce global artifact endpoints ──────────────────────────────────────

func TestHandleWorkforceCreateArtifact_StoreNil(t *testing.T) {
	srv := testServer(t)
	body := `{"kind":"document","title":"My Doc","agent_name":"atlas","session_id":"sess-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleWorkforceCreateArtifact(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleWorkforceCreateArtifact_InvalidJSON(t *testing.T) {
	srv := testServer(t)
	srv.artifactStore = newMemArtifactStore()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleWorkforceCreateArtifact(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleWorkforceCreateArtifact_Success(t *testing.T) {
	srv := testServer(t)
	store := newMemArtifactStore()
	srv.artifactStore = store

	body := `{"kind":"document","title":"Analysis","agent_name":"atlas","session_id":"sess-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleWorkforceCreateArtifact(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["id"] == "" {
		t.Error("expected non-empty id in response")
	}
}

func TestHandleWorkforceCreateArtifact_SetsDefaultStatus(t *testing.T) {
	srv := testServer(t)
	store := newMemArtifactStore()
	srv.artifactStore = store

	body := `{"id":"art-explicit","kind":"code_patch","title":"Patch","agent_name":"bot","session_id":"sess-x"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleWorkforceCreateArtifact(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	a := store.artifacts["art-explicit"]
	if a == nil {
		t.Fatal("artifact not stored")
	}
	if a.Status != workforce.StatusDraft {
		t.Errorf("expected status draft, got %q", a.Status)
	}
}

func TestHandleWorkforceCreateArtifact_PreservesExplicitID(t *testing.T) {
	srv := testServer(t)
	store := newMemArtifactStore()
	srv.artifactStore = store

	body := `{"id":"my-id-123","kind":"document","title":"Doc","agent_name":"bot","session_id":"sess-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleWorkforceCreateArtifact(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp) //nolint:errcheck
	if resp["id"] != "my-id-123" {
		t.Errorf("expected id my-id-123, got %q", resp["id"])
	}
}

func TestHandleWorkforceCreateArtifact_WriteError(t *testing.T) {
	srv := testServer(t)
	store := newMemArtifactStore()
	store.writeErr = errors.New("disk full")
	srv.artifactStore = store

	body := `{"kind":"document","title":"Doc","agent_name":"bot","session_id":"sess-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleWorkforceCreateArtifact(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleWorkforceGetArtifact_StoreNil(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/art-1", nil)
	req.SetPathValue("id", "art-1")
	w := httptest.NewRecorder()
	srv.handleWorkforceGetArtifact(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleWorkforceGetArtifact_MissingID(t *testing.T) {
	srv := testServer(t)
	srv.artifactStore = newMemArtifactStore()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/", nil)
	w := httptest.NewRecorder()
	srv.handleWorkforceGetArtifact(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleWorkforceGetArtifact_NotFound(t *testing.T) {
	srv := testServer(t)
	srv.artifactStore = newMemArtifactStore()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	srv.handleWorkforceGetArtifact(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleWorkforceGetArtifact_Found(t *testing.T) {
	srv := testServer(t)
	store := newMemArtifactStore()
	srv.artifactStore = store

	art := &workforce.Artifact{
		ID:        "art-found",
		Kind:      workforce.KindDocument,
		Title:     "Test Artifact",
		AgentName: "atlas",
		SessionID: "sess-1",
		Status:    workforce.StatusDraft,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	store.artifacts["art-found"] = art

	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/art-found", nil)
	req.SetPathValue("id", "art-found")
	w := httptest.NewRecorder()
	srv.handleWorkforceGetArtifact(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result workforce.Artifact
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.ID != "art-found" {
		t.Errorf("expected ID art-found, got %q", result.ID)
	}
	if result.Title != "Test Artifact" {
		t.Errorf("expected title, got %q", result.Title)
	}
}

func TestHandleWorkforceGetArtifact_LargeContent(t *testing.T) {
	srv := testServer(t)
	store := newMemArtifactStore()
	srv.artifactStore = store

	largeContent := []byte(strings.Repeat("x", 100_000))
	art := &workforce.Artifact{
		ID:        "art-large",
		Kind:      workforce.KindDocument,
		Title:     "Large",
		Content:   largeContent,
		AgentName: "bot",
		SessionID: "sess-1",
		Status:    workforce.StatusDraft,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	store.artifacts["art-large"] = art

	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/art-large", nil)
	req.SetPathValue("id", "art-large")
	w := httptest.NewRecorder()
	srv.handleWorkforceGetArtifact(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result workforce.Artifact
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Content) != len(largeContent) {
		t.Errorf("expected content length %d, got %d", len(largeContent), len(result.Content))
	}
}

func TestHandleWorkforceGetArtifact_ReadError(t *testing.T) {
	srv := testServer(t)
	store := newMemArtifactStore()
	store.readErr = errors.New("database error")
	srv.artifactStore = store

	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/art-1", nil)
	req.SetPathValue("id", "art-1")
	w := httptest.NewRecorder()
	srv.handleWorkforceGetArtifact(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleWorkforceUpdateArtifactStatus_StoreNil(t *testing.T) {
	srv := testServer(t)
	body := `{"status":"accepted"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/artifacts/art-1/status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "art-1")
	w := httptest.NewRecorder()
	srv.handleWorkforceUpdateArtifactStatus(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleWorkforceUpdateArtifactStatus_MissingID(t *testing.T) {
	srv := testServer(t)
	srv.artifactStore = newMemArtifactStore()

	body := `{"status":"accepted"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/artifacts//status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleWorkforceUpdateArtifactStatus(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleWorkforceUpdateArtifactStatus_InvalidJSON(t *testing.T) {
	srv := testServer(t)
	srv.artifactStore = newMemArtifactStore()

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/artifacts/art-1/status", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "art-1")
	w := httptest.NewRecorder()
	srv.handleWorkforceUpdateArtifactStatus(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleWorkforceUpdateArtifactStatus_MissingStatus(t *testing.T) {
	srv := testServer(t)
	srv.artifactStore = newMemArtifactStore()

	body := `{"reason":"some reason"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/artifacts/art-1/status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "art-1")
	w := httptest.NewRecorder()
	srv.handleWorkforceUpdateArtifactStatus(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing status, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleWorkforceUpdateArtifactStatus_NotFound(t *testing.T) {
	srv := testServer(t)
	srv.artifactStore = newMemArtifactStore()

	body := `{"status":"accepted"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/artifacts/missing/status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	srv.handleWorkforceUpdateArtifactStatus(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleWorkforceUpdateArtifactStatus_AcceptedTransition(t *testing.T) {
	srv := testServer(t)
	store := newMemArtifactStore()
	srv.artifactStore = store

	store.artifacts["art-draft"] = &workforce.Artifact{
		ID:     "art-draft",
		Status: workforce.StatusDraft,
	}

	body := `{"status":"accepted"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/artifacts/art-draft/status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "art-draft")
	w := httptest.NewRecorder()
	srv.handleWorkforceUpdateArtifactStatus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "accepted" {
		t.Errorf("expected status accepted, got %q", resp["status"])
	}
	if store.artifacts["art-draft"].Status != workforce.StatusAccepted {
		t.Error("artifact status not updated in store")
	}
}

func TestHandleWorkforceUpdateArtifactStatus_RejectedWithReason(t *testing.T) {
	srv := testServer(t)
	store := newMemArtifactStore()
	srv.artifactStore = store

	store.artifacts["art-r"] = &workforce.Artifact{
		ID:     "art-r",
		Status: workforce.StatusDraft,
	}

	body := `{"status":"rejected","reason":"does not meet requirements"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/artifacts/art-r/status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "art-r")
	w := httptest.NewRecorder()
	srv.handleWorkforceUpdateArtifactStatus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if store.artifacts["art-r"].RejectionReason != "does not meet requirements" {
		t.Errorf("expected rejection reason stored, got %q", store.artifacts["art-r"].RejectionReason)
	}
}

func TestHandleWorkforceUpdateArtifactStatus_SupersededTransition(t *testing.T) {
	srv := testServer(t)
	store := newMemArtifactStore()
	srv.artifactStore = store

	store.artifacts["art-s"] = &workforce.Artifact{ID: "art-s", Status: workforce.StatusDraft}

	body := `{"status":"superseded"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/artifacts/art-s/status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "art-s")
	w := httptest.NewRecorder()
	srv.handleWorkforceUpdateArtifactStatus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleWorkforceUpdateArtifactStatus_UpdateError(t *testing.T) {
	srv := testServer(t)
	store := newMemArtifactStore()
	store.updateErr = errors.New("storage error")
	srv.artifactStore = store

	body := `{"status":"accepted"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/artifacts/art-1/status", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "art-1")
	w := httptest.NewRecorder()
	srv.handleWorkforceUpdateArtifactStatus(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleWorkforceListSessionArtifacts_FallbackWhenStoreNil(t *testing.T) {
	srv := testServer(t)
	sess := makeSessionInStore(t, srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sess.ID+"/artifacts", nil)
	req.SetPathValue("id", sess.ID)
	w := httptest.NewRecorder()
	srv.handleWorkforceListSessionArtifacts(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (fallback), got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleWorkforceListSessionArtifacts_MissingSessionID(t *testing.T) {
	srv := testServer(t)
	srv.artifactStore = newMemArtifactStore()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions//artifacts", nil)
	w := httptest.NewRecorder()
	srv.handleWorkforceListSessionArtifacts(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleWorkforceListSessionArtifacts_Empty(t *testing.T) {
	srv := testServer(t)
	store := newMemArtifactStore()
	srv.artifactStore = store

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-empty/artifacts", nil)
	req.SetPathValue("id", "sess-empty")
	w := httptest.NewRecorder()
	srv.handleWorkforceListSessionArtifacts(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result []artifactSummary
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil empty slice")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 artifacts, got %d", len(result))
	}
}

func TestHandleWorkforceListSessionArtifacts_WithArtifacts(t *testing.T) {
	srv := testServer(t)
	store := newMemArtifactStore()
	srv.artifactStore = store

	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		id := "art-" + string(rune('A'+i))
		store.artifacts[id] = &workforce.Artifact{
			ID:        id,
			SessionID: "sess-with-arts",
			Kind:      workforce.KindDocument,
			Title:     "Artifact " + string(rune('A'+i)),
			AgentName: "bot",
			Status:    workforce.StatusDraft,
			CreatedAt: now,
			UpdatedAt: now,
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-with-arts/artifacts", nil)
	req.SetPathValue("id", "sess-with-arts")
	w := httptest.NewRecorder()
	srv.handleWorkforceListSessionArtifacts(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result []artifactSummary
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("expected 3 artifacts, got %d", len(result))
	}
}

func TestHandleWorkforceListSessionArtifacts_WithLimitQuery(t *testing.T) {
	srv := testServer(t)
	store := newMemArtifactStore()
	srv.artifactStore = store

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		id := "lim-art-" + string(rune('0'+i))
		store.artifacts[id] = &workforce.Artifact{
			ID:        id,
			SessionID: "sess-limit",
			Kind:      workforce.KindDocument,
			Status:    workforce.StatusDraft,
			CreatedAt: now,
			UpdatedAt: now,
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-limit/artifacts?limit=2", nil)
	req.SetPathValue("id", "sess-limit")
	w := httptest.NewRecorder()
	srv.handleWorkforceListSessionArtifacts(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result []artifactSummary
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 artifacts with limit=2, got %d", len(result))
	}
}

func TestHandleWorkforceListSessionArtifacts_ListError(t *testing.T) {
	srv := testServer(t)
	store := newMemArtifactStore()
	store.listErr = errors.New("db error")
	srv.artifactStore = store

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/sess-err/artifacts", nil)
	req.SetPathValue("id", "sess-err")
	w := httptest.NewRecorder()
	srv.handleWorkforceListSessionArtifacts(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleWorkforceListAgentArtifacts_StoreNil(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/atlas/artifacts", nil)
	req.SetPathValue("name", "atlas")
	w := httptest.NewRecorder()
	srv.handleWorkforceListAgentArtifacts(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleWorkforceListAgentArtifacts_MissingName(t *testing.T) {
	srv := testServer(t)
	srv.artifactStore = newMemArtifactStore()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents//artifacts", nil)
	w := httptest.NewRecorder()
	srv.handleWorkforceListAgentArtifacts(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleWorkforceListAgentArtifacts_Empty(t *testing.T) {
	srv := testServer(t)
	srv.artifactStore = newMemArtifactStore()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/atlas/artifacts", nil)
	req.SetPathValue("name", "atlas")
	w := httptest.NewRecorder()
	srv.handleWorkforceListAgentArtifacts(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result []artifactSummary
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil empty slice")
	}
}

func TestHandleWorkforceListAgentArtifacts_WithArtifacts(t *testing.T) {
	srv := testServer(t)
	store := newMemArtifactStore()
	srv.artifactStore = store

	now := time.Now().UTC()
	store.artifacts["agent-art-1"] = &workforce.Artifact{
		ID:        "agent-art-1",
		AgentName: "my-agent",
		SessionID: "sess-1",
		Status:    workforce.StatusDraft,
		CreatedAt: now,
		UpdatedAt: now,
	}
	store.artifacts["agent-art-2"] = &workforce.Artifact{
		ID:        "agent-art-2",
		AgentName: "other-agent",
		SessionID: "sess-2",
		Status:    workforce.StatusDraft,
		CreatedAt: now,
		UpdatedAt: now,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/my-agent/artifacts", nil)
	req.SetPathValue("name", "my-agent")
	w := httptest.NewRecorder()
	srv.handleWorkforceListAgentArtifacts(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result []artifactSummary
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 artifact for my-agent, got %d", len(result))
	}
	if result[0].ID != "agent-art-1" {
		t.Errorf("expected agent-art-1, got %q", result[0].ID)
	}
}

func TestHandleWorkforceListAgentArtifacts_SinceQueryParam(t *testing.T) {
	srv := testServer(t)
	store := newMemArtifactStore()
	srv.artifactStore = store

	old := time.Now().UTC().Add(-60 * 24 * time.Hour)
	recent := time.Now().UTC().Add(-5 * 24 * time.Hour)

	store.artifacts["old-art"] = &workforce.Artifact{
		ID:        "old-art",
		AgentName: "scanner",
		SessionID: "sess-1",
		Status:    workforce.StatusDraft,
		CreatedAt: old,
		UpdatedAt: old,
	}
	store.artifacts["recent-art"] = &workforce.Artifact{
		ID:        "recent-art",
		AgentName: "scanner",
		SessionID: "sess-1",
		Status:    workforce.StatusDraft,
		CreatedAt: recent,
		UpdatedAt: recent,
	}

	since := time.Now().UTC().Add(-7 * 24 * time.Hour).Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/scanner/artifacts?since="+since, nil)
	req.SetPathValue("name", "scanner")
	w := httptest.NewRecorder()
	srv.handleWorkforceListAgentArtifacts(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result []artifactSummary
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, a := range result {
		if a.ID == "old-art" {
			t.Error("old artifact should be filtered by since param")
		}
	}
}

func TestHandleWorkforceListAgentArtifacts_ListError(t *testing.T) {
	srv := testServer(t)
	store := newMemArtifactStore()
	store.listErr = errors.New("db error")
	srv.artifactStore = store

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/atlas/artifacts", nil)
	req.SetPathValue("name", "atlas")
	w := httptest.NewRecorder()
	srv.handleWorkforceListAgentArtifacts(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// ── parseIntQuery helper ──────────────────────────────────────────────────────

func TestParseIntQuery_Default(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	if got := parseIntQuery(req, "limit", 42); got != 42 {
		t.Errorf("expected 42, got %d", got)
	}
}

func TestParseIntQuery_Valid(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test?limit=10", nil)
	if got := parseIntQuery(req, "limit", 0); got != 10 {
		t.Errorf("expected 10, got %d", got)
	}
}

func TestParseIntQuery_Invalid(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test?limit=notanumber", nil)
	if got := parseIntQuery(req, "limit", 99); got != 99 {
		t.Errorf("expected default 99, got %d", got)
	}
}

// ── End-to-end via registered routes ─────────────────────────────────────────

func TestArtifactsEndToEnd_ListArtifacts_Unauthorized(t *testing.T) {
	_, ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/api/v1/sessions/some-session/artifacts")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestArtifactsEndToEnd_ListSessionArtifacts_Authorized(t *testing.T) {
	srv, ts := newTestServer(t)
	sess := makeSessionInStore(t, srv)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/sessions/"+sess.ID+"/artifacts", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestArtifactsEndToEnd_CreateArtifact_Success(t *testing.T) {
	srv, ts := newTestServer(t)
	sess := makeSessionInStore(t, srv)
	srv.SetArtifactStore(newMemArtifactStore())

	body := `{"title":"result.txt","kind":"document"}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/sessions/"+sess.ID+"/artifacts", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["id"] == "" {
		t.Error("expected non-empty artifact ID")
	}
}

func TestArtifactsEndToEnd_WorkforceCreate_NoStore(t *testing.T) {
	_, ts := newTestServer(t)
	body := `{"kind":"document","title":"Doc","agent_name":"bot","session_id":"sess-1"}`
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/artifacts", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
}

func TestArtifactsEndToEnd_WorkforceGet_NoStore(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/artifacts/art-1", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
}

func TestArtifactsEndToEnd_WorkforcePatchStatus_NoStore(t *testing.T) {
	_, ts := newTestServer(t)
	body := `{"status":"accepted"}`
	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/v1/artifacts/art-1/status", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
}

func TestArtifactsEndToEnd_PutOnArtifactID_MethodRouted(t *testing.T) {
	_, ts := newTestServer(t)
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/sessions/missing/artifacts/art-1", bytes.NewBufferString("{}"))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 (session not found), got %d", resp.StatusCode)
	}
}
