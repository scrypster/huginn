package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/session"
)

func TestHandleSessionActiveState_NotFound(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/no-such-session/active-state", nil)
	req.SetPathValue("id", "no-such-session")
	w := httptest.NewRecorder()
	srv.handleSessionActiveState(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleSessionActiveState_NoDB(t *testing.T) {
	srv := testServer(t)
	// Create a real session so Exists() returns true.
	sessDir := t.TempDir()
	store := session.NewStore(sessDir)
	srv.store = store

	sess := store.New("active-state-test", "/tmp", "model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sess.ID+"/active-state", nil)
	req.SetPathValue("id", sess.ID)
	w := httptest.NewRecorder()
	srv.handleSessionActiveState(w, req)

	// db is nil so we get a valid stub response.
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result sessionActiveState
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.SessionID != sess.ID {
		t.Errorf("expected session_id %q, got %q", sess.ID, result.SessionID)
	}
	if result.ActiveThreads == nil {
		t.Error("expected non-nil active_threads")
	}
	if result.InFlightTasks == nil {
		t.Error("expected non-nil in_flight_tasks")
	}
}

func TestHandleSessionActiveState_WithDB(t *testing.T) {
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	srv.SetDB(db)

	sqliteStore := session.NewSQLiteSessionStore(db)
	sess := sqliteStore.New("active-state-db-test", "/tmp", "model")
	if err := sqliteStore.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}
	srv.store = sqliteStore

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sess.ID+"/active-state", nil)
	req.SetPathValue("id", sess.ID)
	w := httptest.NewRecorder()
	srv.handleSessionActiveState(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result sessionActiveState
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.SessionID != sess.ID {
		t.Errorf("expected session_id %q, got %q", sess.ID, result.SessionID)
	}
	// Empty session — no messages yet.
	if result.LastSeq != 0 {
		t.Errorf("expected last_seq=0 for empty session, got %d", result.LastSeq)
	}
	if len(result.ActiveThreads) != 0 {
		t.Errorf("expected no active threads for empty session, got %d", len(result.ActiveThreads))
	}
}
