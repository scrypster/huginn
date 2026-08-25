package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/claudecode"
)

func TestClaudeStatusReportsDisabled(t *testing.T) {
	s := &Server{claudeCfg: claudecode.DefaultConfig()}

	rec := httptest.NewRecorder()
	s.handleClaudeStatus(rec, httptest.NewRequest(http.MethodGet, "/api/v1/claude/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Enabled  bool   `json:"enabled"`
		Watching bool   `json:"watching"`
		Root     string `json:"root"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Enabled {
		t.Error("enabled = true for the default config")
	}
	if body.Watching {
		t.Error("watching = true while disabled")
	}
}

func TestClaudeBackfillRefusesWhenDisabled(t *testing.T) {
	s := &Server{claudeCfg: claudecode.DefaultConfig()}

	rec := httptest.NewRecorder()
	s.handleClaudeBackfill(rec, httptest.NewRequest(http.MethodPost, "/api/v1/claude/backfill", nil))

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 when the bridge is disabled", rec.Code)
	}
}

func TestClaudeStatusReportsEnabledState(t *testing.T) {
	cfg := claudecode.DefaultConfig()
	cfg.Enabled = true
	s := &Server{claudeCfg: cfg, claudeWatching: true, claudeRoot: "/tmp/projects"}

	rec := httptest.NewRecorder()
	s.handleClaudeStatus(rec, httptest.NewRequest(http.MethodGet, "/api/v1/claude/status", nil))

	var body struct {
		Enabled  bool   `json:"enabled"`
		Watching bool   `json:"watching"`
		Root     string `json:"root"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Enabled || !body.Watching {
		t.Errorf("enabled=%v watching=%v, want both true", body.Enabled, body.Watching)
	}
	if body.Root != "/tmp/projects" {
		t.Errorf("root = %q, want /tmp/projects", body.Root)
	}
}

func TestClaudeRoutesAreRegistered(t *testing.T) {
	s := &Server{claudeCfg: claudecode.DefaultConfig()}
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/claude/status"},
		{http.MethodPost, "/api/v1/claude/backfill"},
	} {
		h, pattern := mux.Handler(httptest.NewRequest(tc.method, tc.path, nil))
		if h == nil || pattern == "" {
			t.Errorf("%s %s is not routed (pattern=%q)", tc.method, tc.path, pattern)
		}
	}
}

func TestStartClaudeBridgeWithNilDBIsSafe(t *testing.T) {
	s := &Server{}
	cfg := claudecode.DefaultConfig()
	cfg.Enabled = true // watch is enabled by default, so this would otherwise start

	if err := s.StartClaudeBridge(context.Background(), cfg, nil); err != nil {
		t.Fatalf("StartClaudeBridge with a nil DB returned %v, want nil — it must never abort startup", err)
	}

	rec := httptest.NewRecorder()
	s.handleClaudeStatus(rec, httptest.NewRequest(http.MethodGet, "/api/v1/claude/status", nil))
	var body struct {
		Watching bool `json:"watching"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Watching {
		t.Error("watching = true after a nil-DB start; nothing should be running")
	}
}
