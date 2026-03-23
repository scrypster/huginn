package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/workforce"
)

// stubArtifactStore is a minimal in-memory artifactStore for testing.
type stubArtifactStore struct {
	artifacts map[string]*workforce.Artifact
}

func newStubArtifactStore() *stubArtifactStore {
	return &stubArtifactStore{artifacts: make(map[string]*workforce.Artifact)}
}

func (s *stubArtifactStore) Write(_ context.Context, a *workforce.Artifact) error {
	if a.ID == "" {
		a.ID = "stub-" + a.Title
	}
	s.artifacts[a.ID] = a
	return nil
}

func (s *stubArtifactStore) Read(_ context.Context, id string) (*workforce.Artifact, error) {
	a, ok := s.artifacts[id]
	if !ok {
		return nil, workforce.ErrArtifactNotFound
	}
	return a, nil
}

func (s *stubArtifactStore) ReadMetaOnly(_ context.Context, id string) (*workforce.Artifact, error) {
	a, ok := s.artifacts[id]
	if !ok {
		return nil, workforce.ErrArtifactNotFound
	}
	// Return copy without content.
	copy := *a
	copy.Content = nil
	return &copy, nil
}

func (s *stubArtifactStore) ListBySession(_ context.Context, sessionID string, _ int, _ string) ([]*workforce.Artifact, error) {
	var out []*workforce.Artifact
	for _, a := range s.artifacts {
		if a.SessionID == sessionID {
			out = append(out, a)
		}
	}
	if out == nil {
		out = []*workforce.Artifact{}
	}
	return out, nil
}

func (s *stubArtifactStore) ListByAgent(_ context.Context, agentName string, since time.Time, _ int, _ string) ([]*workforce.Artifact, error) {
	var out []*workforce.Artifact
	for _, a := range s.artifacts {
		if a.AgentName == agentName && !a.CreatedAt.Before(since) {
			out = append(out, a)
		}
	}
	if out == nil {
		out = []*workforce.Artifact{}
	}
	return out, nil
}

func (s *stubArtifactStore) UpdateStatus(_ context.Context, id string, status workforce.ArtifactStatus, reason string) error {
	a, ok := s.artifacts[id]
	if !ok {
		return workforce.ErrArtifactNotFound
	}
	a.Status = status
	a.RejectionReason = reason
	return nil
}

func (s *stubArtifactStore) OpenContent(_ context.Context, id string) (io.ReadCloser, error) {
	a, ok := s.artifacts[id]
	if !ok || a.ContentRef == "" {
		return nil, workforce.ErrArtifactNotFound
	}
	return io.NopCloser(strings.NewReader(string(a.Content))), nil
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestPostArtifact_NoStore(t *testing.T) {
	srv := testServer(t)
	// artifactStore is nil (default)

	body := `{"kind":"document","title":"Test","agent_name":"atlas","session_id":"sess-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleWorkforceCreateArtifact(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp) //nolint:errcheck
	if resp["error"] != "artifact store not available" {
		t.Errorf("unexpected error message: %q", resp["error"])
	}
}

func TestGetArtifact_NotFound(t *testing.T) {
	srv := testServer(t)
	srv.SetArtifactStore(newStubArtifactStore())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/no-such-id", nil)
	req.SetPathValue("id", "no-such-id")
	w := httptest.NewRecorder()
	srv.handleWorkforceGetArtifact(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPostArtifact_Created(t *testing.T) {
	srv := testServer(t)
	store := newStubArtifactStore()
	srv.SetArtifactStore(store)

	body := `{"kind":"document","title":"My Doc","agent_name":"atlas","session_id":"sess-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/artifacts", strings.NewReader(body))
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
	if len(store.artifacts) != 1 {
		t.Errorf("expected 1 stored artifact, got %d", len(store.artifacts))
	}
}

func TestGetArtifact_Found(t *testing.T) {
	srv := testServer(t)
	store := newStubArtifactStore()
	srv.SetArtifactStore(store)

	a := &workforce.Artifact{
		ID:        "art-1",
		Kind:      workforce.KindDocument,
		Title:     "My Doc",
		AgentName: "atlas",
		SessionID: "sess-1",
		Status:    workforce.StatusDraft,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	store.Write(context.Background(), a) //nolint:errcheck

	req := httptest.NewRequest(http.MethodGet, "/api/v1/artifacts/art-1", nil)
	req.SetPathValue("id", "art-1")
	w := httptest.NewRecorder()
	srv.handleWorkforceGetArtifact(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result workforce.Artifact
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.ID != "art-1" {
		t.Errorf("expected id art-1, got %q", result.ID)
	}
}

func TestPatchArtifactStatus_NoStore(t *testing.T) {
	srv := testServer(t)
	// artifactStore is nil

	body := `{"status":"accepted"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/artifacts/art-1/status", strings.NewReader(body))
	req.SetPathValue("id", "art-1")
	w := httptest.NewRecorder()
	srv.handleWorkforceUpdateArtifactStatus(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPatchArtifactStatus_NotFound(t *testing.T) {
	srv := testServer(t)
	srv.SetArtifactStore(newStubArtifactStore())

	body := `{"status":"accepted"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/artifacts/no-such/status", strings.NewReader(body))
	req.SetPathValue("id", "no-such")
	w := httptest.NewRecorder()
	srv.handleWorkforceUpdateArtifactStatus(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestPatchArtifactStatus_OK(t *testing.T) {
	srv := testServer(t)
	store := newStubArtifactStore()
	srv.SetArtifactStore(store)

	a := &workforce.Artifact{
		ID:        "art-patch",
		Kind:      workforce.KindCodePatch,
		Title:     "patch",
		AgentName: "bot",
		SessionID: "sess-2",
		Status:    workforce.StatusDraft,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	store.Write(context.Background(), a) //nolint:errcheck

	body := `{"status":"accepted"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/artifacts/art-patch/status", strings.NewReader(body))
	req.SetPathValue("id", "art-patch")
	w := httptest.NewRecorder()
	srv.handleWorkforceUpdateArtifactStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if store.artifacts["art-patch"].Status != workforce.StatusAccepted {
		t.Errorf("expected status accepted, got %q", store.artifacts["art-patch"].Status)
	}
}

func TestListAgentArtifacts_NoStore(t *testing.T) {
	srv := testServer(t)
	// artifactStore is nil

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/atlas/artifacts", nil)
	req.SetPathValue("name", "atlas")
	w := httptest.NewRecorder()
	srv.handleWorkforceListAgentArtifacts(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListAgentArtifacts_Empty(t *testing.T) {
	srv := testServer(t)
	srv.SetArtifactStore(newStubArtifactStore())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/atlas/artifacts", nil)
	req.SetPathValue("name", "atlas")
	w := httptest.NewRecorder()
	srv.handleWorkforceListAgentArtifacts(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result []artifactSummary
	json.NewDecoder(w.Body).Decode(&result) //nolint:errcheck
	if result == nil {
		t.Error("expected non-nil empty array")
	}
}
