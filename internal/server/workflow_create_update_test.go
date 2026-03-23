package server

// workflow_create_update_test.go — Workflow create/update handler tests including version increment.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/scheduler"
)

// ── handleCreateWorkflow ─────────────────────────────────────────────────────

func TestHandleCreateWorkflow_ValidBody_Iter9(t *testing.T) {
	srv, _ := newTestServer(t)
	_ = os.MkdirAll(filepath.Join(srv.huginnDir, "workflows"), 0755)

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	body, _ := json.Marshal(map[string]any{
		"name":     "New Workflow",
		"enabled":  true,
		"schedule": "0 9 * * 1",
	})
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["name"] != "New Workflow" {
		t.Errorf("want name=New Workflow, got %v", result["name"])
	}
}

func TestHandleCreateWorkflow_InvalidJSON_Iter9(t *testing.T) {
	srv, _ := newTestServer(t)
	_ = os.MkdirAll(filepath.Join(srv.huginnDir, "workflows"), 0755)

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows", bytes.NewReader([]byte("{bad")))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

// ── handleUpdateWorkflow ─────────────────────────────────────────────────────

func TestHandleUpdateWorkflow_Found_Iter9(t *testing.T) {
	srv, _ := newTestServer(t)
	wfDir := filepath.Join(srv.huginnDir, "workflows")
	_ = os.MkdirAll(wfDir, 0755)
	wf := scheduler.Workflow{
		ID:      "upd-wf-iter9",
		Name:    "Update Me",
		Enabled: true,
	}
	if err := scheduler.SaveWorkflow(wfDir, &wf); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	updateBody, _ := json.Marshal(map[string]any{
		"name":     "Updated Workflow",
		"enabled":  false,
		"schedule": "0 10 * * 1",
	})
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/workflows/"+wf.ID, bytes.NewReader(updateBody))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["name"] != "Updated Workflow" {
		t.Errorf("want name=Updated Workflow, got %v", result["name"])
	}
}

func TestHandleUpdateWorkflow_NotFound_Iter9(t *testing.T) {
	srv, _ := newTestServer(t)
	_ = os.MkdirAll(filepath.Join(srv.huginnDir, "workflows"), 0755)

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	body, _ := json.Marshal(map[string]any{"name": "ghost"})
	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/workflows/no-such-wf-iter9", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

func TestHandleUpdateWorkflow_InvalidJSON_Iter9(t *testing.T) {
	srv, _ := newTestServer(t)
	_ = os.MkdirAll(filepath.Join(srv.huginnDir, "workflows"), 0755)

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/workflows/any-wf", bytes.NewReader([]byte("{bad")))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

// ── Version increment & conflict tests ───────────────────────────────────────

// TestWorkflow_VersionIncrementsOnUpdate verifies that:
//   - A freshly saved workflow has version == 1
//   - Each successful update increments the version
//   - A PUT with a stale version (version from before the last update) returns 409
func TestWorkflow_VersionIncrementsOnUpdate(t *testing.T) {
	srv, _ := newTestServer(t)
	wfDir := filepath.Join(srv.huginnDir, "workflows")
	_ = os.MkdirAll(wfDir, 0755)

	// Create an initial workflow on disk (SaveWorkflow increments to version=1).
	wf := scheduler.Workflow{
		ID:      "version-test-wf",
		Name:    "Version Test",
		Enabled: false,
	}
	if err := scheduler.SaveWorkflow(wfDir, &wf); err != nil {
		t.Fatal(err)
	}
	if wf.Version != 1 {
		t.Fatalf("expected initial version=1 after SaveWorkflow, got %d", wf.Version)
	}

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	doUpdate := func(t *testing.T, name string, sendVersion uint64) (statusCode int, returnedVersion uint64) {
		t.Helper()
		payload := map[string]any{
			"name":    name,
			"enabled": false,
			"version": sendVersion,
		}
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest("PUT", ts.URL+"/api/v1/workflows/"+wf.ID, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			var result map[string]any
			json.NewDecoder(resp.Body).Decode(&result)
			if v, ok := result["version"].(float64); ok {
				return resp.StatusCode, uint64(v)
			}
		}
		return resp.StatusCode, 0
	}

	// First update: submit version=1 (matches stored), expect version=2 back.
	status, ver := doUpdate(t, "Version Test v2", 1)
	if status != 200 {
		t.Fatalf("first update: want 200, got %d", status)
	}
	if ver != 2 {
		t.Errorf("first update: want returned version=2, got %d", ver)
	}

	// Second update: submit version=2, expect version=3 back.
	status, ver = doUpdate(t, "Version Test v3", 2)
	if status != 200 {
		t.Fatalf("second update: want 200, got %d", status)
	}
	if ver != 3 {
		t.Errorf("second update: want returned version=3, got %d", ver)
	}

	// Stale update: submit old version=1, expect 409 Conflict.
	status, _ = doUpdate(t, "Stale Update", 1)
	if status != 409 {
		t.Errorf("stale update: want 409, got %d", status)
	}
}
