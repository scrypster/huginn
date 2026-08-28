package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/models"
)

// These tests verify that config-persisting handlers use a read-modify-write
// pattern (config.UpdateAt via Server.updateConfig) rather than saving a
// stale full in-memory Config over whatever another writer (e.g. the TUI,
// or a concurrent request) has since written to disk. Each test simulates a
// concurrent writer by mutating the on-disk config file directly (a field
// the handler under test does not touch) after the server's in-memory s.cfg
// was already established, then asserts that field survives the handler's
// save.

// writeDiskConfig persists cfg to srv.configPath, simulating a write made by
// another process (e.g. the TUI) after the server's in-memory s.cfg snapshot
// was taken.
func writeDiskConfig(t *testing.T, srv *Server, mutate func(*config.Config)) {
	t.Helper()
	if err := config.UpdateAt(srv.configPath, mutate); err != nil {
		t.Fatalf("writeDiskConfig: %v", err)
	}
}

func readDiskConfig(t *testing.T, srv *Server) *config.Config {
	t.Helper()
	cfg, err := config.LoadFrom(srv.configPath)
	if err != nil {
		t.Fatalf("readDiskConfig: %v", err)
	}
	return cfg
}

func TestHandleRestoreActiveState_PreservesConcurrentDiskWrite(t *testing.T) {
	srv, _ := newTestServer(t)

	sess := srv.store.New("cross-writer-active-state", "/tmp", "model")
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	// A concurrent writer (e.g. the TUI) changes an unrelated field on disk
	// after the server's in-memory cfg was established.
	writeDiskConfig(t, srv, func(c *config.Config) {
		c.Theme = "concurrent-writer-theme"
	})

	body := `{"session_id":"` + sess.ID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/active-state/restore", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleRestoreActiveState(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	disk := readDiskConfig(t, srv)
	if disk.ActiveSessionID != sess.ID {
		t.Errorf("expected active_session_id=%q on disk, got %q", sess.ID, disk.ActiveSessionID)
	}
	if disk.Theme != "concurrent-writer-theme" {
		t.Errorf("concurrent writer's theme was clobbered: got %q", disk.Theme)
	}
}

func TestHandleUpdateConfig_PreservesConcurrentActiveSessionWrite(t *testing.T) {
	srv, ts := newTestServer(t)

	// A concurrent writer (e.g. the active-state handler, or the TUI) sets
	// active_session_id on disk after the server's in-memory cfg snapshot
	// (built from config.Default()) was taken — and after the client that
	// is about to PUT /api/v1/config did its GET.
	writeDiskConfig(t, srv, func(c *config.Config) {
		c.ActiveSessionID = "session-from-other-writer"
	})

	// The settings PUT body does not mention active_session_id at all (as a
	// real settings form wouldn't), but does change an unrelated field.
	payload := `{"version":1,"web_ui":{"port":0},"backend":{"provider":"anthropic"},"theme":"light"}`
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/config", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	disk := readDiskConfig(t, srv)
	if disk.ActiveSessionID != "session-from-other-writer" {
		t.Errorf("settings PUT clobbered active_session_id: got %q, want %q", disk.ActiveSessionID, "session-from-other-writer")
	}
	if disk.Theme != "light" {
		t.Errorf("expected theme=light from settings PUT, got %q", disk.Theme)
	}
}

func TestHandleBuiltinActivate_PreservesConcurrentDiskWrite(t *testing.T) {
	srv, _ := newTestServer(t)

	tmpDir := t.TempDir()
	store, err := models.NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Record("installed-model", models.LockEntry{
		Name:        "installed-model",
		Filename:    "installed-model.gguf",
		InstalledAt: time.Now(),
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	srv.modelStore = store

	writeDiskConfig(t, srv, func(c *config.Config) {
		c.ActiveSessionID = "session-from-other-writer"
	})

	body := `{"model":"installed-model"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/builtin/activate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleBuiltinActivate(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	disk := readDiskConfig(t, srv)
	if disk.Backend.Type != "managed" || disk.Backend.BuiltinModel != "installed-model" {
		t.Errorf("expected backend.type=managed, builtin_model=installed-model, got %+v", disk.Backend)
	}
	if disk.ActiveSessionID != "session-from-other-writer" {
		t.Errorf("builtin activate clobbered active_session_id: got %q", disk.ActiveSessionID)
	}
}

func TestHandleSetSecret_PreservesConcurrentDiskWrite(t *testing.T) {
	srv, _ := newTestServer(t)

	writeDiskConfig(t, srv, func(c *config.Config) {
		c.ActiveSessionID = "session-from-other-writer"
	})

	body := `{"value":"sk-ant-test-value"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/secrets/anthropic", strings.NewReader(body))
	req.SetPathValue("slot", "anthropic")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSetSecret(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	disk := readDiskConfig(t, srv)
	if disk.Backend.APIKey == "" {
		t.Error("expected backend.api_key to be persisted")
	}
	if disk.ActiveSessionID != "session-from-other-writer" {
		t.Errorf("set-secret clobbered active_session_id: got %q", disk.ActiveSessionID)
	}
}

func TestHandleDeleteSecret_PreservesConcurrentDiskWrite(t *testing.T) {
	srv, _ := newTestServer(t)

	// Seed a secret first so there's something to delete.
	writeDiskConfig(t, srv, func(c *config.Config) {
		c.Backend.APIKey = "keyring:huginn:anthropic"
	})
	srv.cfg.Backend.APIKey = "keyring:huginn:anthropic"

	writeDiskConfig(t, srv, func(c *config.Config) {
		c.ActiveSessionID = "session-from-other-writer"
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/secrets/anthropic", nil)
	req.SetPathValue("slot", "anthropic")
	w := httptest.NewRecorder()
	srv.handleDeleteSecret(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	disk := readDiskConfig(t, srv)
	if disk.Backend.APIKey != "" {
		t.Errorf("expected backend.api_key cleared, got %q", disk.Backend.APIKey)
	}
	if disk.ActiveSessionID != "session-from-other-writer" {
		t.Errorf("delete-secret clobbered active_session_id: got %q", disk.ActiveSessionID)
	}
}
