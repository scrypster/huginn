package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/logger"
)

func TestHandleGetLogLevel(t *testing.T) {
	srv, _ := newTestServer(t)

	// Set a known level so the test is deterministic.
	logger.SetLevel(slog.LevelWarn)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/log-level", nil)
	w := httptest.NewRecorder()
	srv.handleGetLogLevel(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["level"] != "WARN" {
		t.Errorf("expected level=WARN, got %q", resp["level"])
	}
}

func TestHandleSetLogLevel_Valid(t *testing.T) {
	// Ensure the global logger is initialised so SetLevel/Level are not no-ops.
	if err := logger.InitWithLevel(t.TempDir(), slog.LevelWarn); err != nil {
		t.Fatalf("logger.InitWithLevel: %v", err)
	}
	srv, _ := newTestServer(t)

	// Reset to warn so we can verify the change.
	logger.SetLevel(slog.LevelWarn)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/log-level",
		strings.NewReader(`{"level":"debug"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSetLogLevel(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d (body: %s)", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["level"] != "DEBUG" {
		t.Errorf("expected level=DEBUG, got %q", resp["level"])
	}
	// Verify the global level actually changed.
	if logger.Level() != slog.LevelDebug {
		t.Errorf("expected global level=DEBUG after set, got %v", logger.Level())
	}
	// Restore.
	logger.SetLevel(slog.LevelWarn)
}

func TestHandleSetLogLevel_InvalidLevel(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/log-level",
		strings.NewReader(`{"level":"verbose"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSetLogLevel(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleSetLogLevel_InvalidJSON(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/log-level",
		strings.NewReader(`not-json`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSetLogLevel(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestLogLevelRoutes_AuthRequired(t *testing.T) {
	srv, _ := newTestServer(t)

	// GET without auth should return 401 — test via recorder to avoid
	// mux-level routing differences across CI environments.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/log-level", nil)
	getW := httptest.NewRecorder()
	srv.authMiddleware(srv.handleGetLogLevel)(getW, getReq)
	if getW.Code != http.StatusUnauthorized {
		t.Errorf("GET without auth: expected 401, got %d", getW.Code)
	}

	// PUT without auth should return 401.
	putReq := httptest.NewRequest(http.MethodPut, "/api/v1/log-level",
		strings.NewReader(`{"level":"info"}`))
	putW := httptest.NewRecorder()
	srv.authMiddleware(srv.handleSetLogLevel)(putW, putReq)
	if putW.Code != http.StatusUnauthorized {
		t.Errorf("PUT without auth: expected 401, got %d", putW.Code)
	}
}
