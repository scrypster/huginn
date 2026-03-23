package server

// hardening_iter8_test.go — Iteration 4 quality improvements
// Covers workflow CRUD handlers, handleListNotifications paths, handleListWorkflowRuns with store.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/notification"
	"github.com/scrypster/huginn/internal/scheduler"
	"gopkg.in/yaml.v3"
)

// ── Workflow handler helpers ─────────────────────────────────────────────────

func writeWorkflowFile(t *testing.T, dir string, wf scheduler.Workflow) {
	t.Helper()
	_ = os.MkdirAll(dir, 0755)
	data, err := yaml.Marshal(wf)
	if err != nil {
		t.Fatalf("marshal workflow: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, wf.ID+".yaml"), data, 0644); err != nil {
		t.Fatalf("write workflow file: %v", err)
	}
}

// ── handleListWorkflows ──────────────────────────────────────────────────────

func TestHandleListWorkflows_Empty_Iter8(t *testing.T) {
	srv, _ := newTestServer(t)
	_ = os.MkdirAll(filepath.Join(srv.huginnDir, "workflows"), 0755)

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/workflows", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var result []any
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result) != 0 {
		t.Errorf("want empty list, got %d items", len(result))
	}
}

func TestHandleListWorkflows_WithWorkflow_Iter8(t *testing.T) {
	srv, _ := newTestServer(t)
	wfDir := filepath.Join(srv.huginnDir, "workflows")
	wf := scheduler.Workflow{
		ID:      "list-wf-iter8",
		Name:    "List Me",
		Enabled: true,
	}
	writeWorkflowFile(t, wfDir, wf)

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/workflows", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var result []any
	json.NewDecoder(resp.Body).Decode(&result)
	if len(result) != 1 {
		t.Errorf("want 1 workflow, got %d", len(result))
	}
}

// ── handleGetWorkflow ────────────────────────────────────────────────────────

func TestHandleGetWorkflow_Found_Iter8(t *testing.T) {
	srv, _ := newTestServer(t)
	wfDir := filepath.Join(srv.huginnDir, "workflows")
	wf := scheduler.Workflow{
		ID:      "get-wf-iter8",
		Name:    "Get Me",
		Enabled: true,
	}
	writeWorkflowFile(t, wfDir, wf)

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/workflows/"+wf.ID, nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
}

func TestHandleGetWorkflow_NotFound_Iter8(t *testing.T) {
	srv, _ := newTestServer(t)
	_ = os.MkdirAll(filepath.Join(srv.huginnDir, "workflows"), 0755)

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/workflows/missing-iter8", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

// ── handleDeleteWorkflow ─────────────────────────────────────────────────────

func TestHandleDeleteWorkflow_Found_Iter8(t *testing.T) {
	srv, _ := newTestServer(t)
	wfDir := filepath.Join(srv.huginnDir, "workflows")
	wf := scheduler.Workflow{
		ID:      "del-wf-iter8",
		Name:    "Delete Me",
		Enabled: true,
	}
	writeWorkflowFile(t, wfDir, wf)
	// Need FilePath set so DeleteWorkflow can remove the file.
	// Save via scheduler.SaveWorkflow so FilePath is set in the struct.
	if err := scheduler.SaveWorkflow(wfDir, &wf); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/workflows/"+wf.ID, nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
}

func TestHandleDeleteWorkflow_NotFound_Iter8(t *testing.T) {
	srv, _ := newTestServer(t)
	_ = os.MkdirAll(filepath.Join(srv.huginnDir, "workflows"), 0755)

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/workflows/no-such-wf-iter8", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

// ── handleRunWorkflow ────────────────────────────────────────────────────────

func TestHandleRunWorkflow_NoScheduler_Iter8(t *testing.T) {
	srv, _ := newTestServer(t)
	wfDir := filepath.Join(srv.huginnDir, "workflows")
	wf := scheduler.Workflow{
		ID:      "run-wf-iter8",
		Name:    "Run Me",
		Enabled: true,
	}
	writeWorkflowFile(t, wfDir, wf)

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/"+wf.ID+"/run", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Errorf("want 503, got %d", resp.StatusCode)
	}
}

func TestHandleRunWorkflow_NotFound_Iter8(t *testing.T) {
	srv, _ := newTestServer(t)
	_ = os.MkdirAll(filepath.Join(srv.huginnDir, "workflows"), 0755)

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/workflows/missing-iter8/run", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

// ── handleListWorkflowRuns ───────────────────────────────────────────────────

func TestHandleListWorkflowRuns_WithStore_Iter8(t *testing.T) {
	srv, _, _ := newTestServerWithNotifStore(t)
	// Wire a workflow run store.
	runsDir := filepath.Join(srv.huginnDir, "workflow-runs")
	srv.mu.Lock()
	srv.workflowRunStore = scheduler.NewWorkflowRunStore(runsDir)
	srv.mu.Unlock()

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/workflows/wf-iter8/runs", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var result []any
	json.NewDecoder(resp.Body).Decode(&result)
	// No runs yet — empty is valid.
	if result == nil {
		t.Error("want non-nil result slice")
	}
}

// ── handleListNotifications ──────────────────────────────────────────────────

func TestHandleListNotifications_NoStore_Iter8(t *testing.T) {
	srv, _ := newTestServer(t)
	// notifStore is nil by default in newTestServer.
	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/notifications", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("want 200 (empty), got %d", resp.StatusCode)
	}
}

func TestHandleListNotifications_AllPending_Iter8(t *testing.T) {
	srv, _, ns := newTestServerWithNotifStore(t)
	_ = ns.Put(&notification.Notification{
		ID:        notification.NewID(),
		RoutineID: "rt-iter8",
		RunID:     notification.NewID(),
		Summary:   "pending item",
		Status:    notification.StatusPending,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/notifications", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var items []any
	json.NewDecoder(resp.Body).Decode(&items)
	if len(items) != 1 {
		t.Errorf("want 1 notification, got %d", len(items))
	}
}

func TestHandleListNotifications_ByRoutineID_Iter8(t *testing.T) {
	srv, _, ns := newTestServerWithNotifStore(t)
	_ = ns.Put(&notification.Notification{
		ID:        notification.NewID(),
		RoutineID: "specific-routine-iter8",
		RunID:     notification.NewID(),
		Summary:   "for specific routine",
		Status:    notification.StatusPending,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/notifications?routine_id=specific-routine-iter8", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
}

// ── handleNotificationAction approve path ────────────────────────────────────

func TestHandleNotificationAction_Approve_Iter8(t *testing.T) {
	srv, _, ns := newTestServerWithNotifStore(t)
	n := makeTestNotification(t, ns)

	// Approving a notification requires at least one proposed action; add one now.
	n.ProposedActions = []notification.ProposedAction{
		{ID: "a1", Label: "Run tests", ToolName: "bash", ToolParams: map[string]any{"cmd": "go test ./..."}},
	}
	if err := ns.Put(n); err != nil {
		t.Fatalf("re-put notification with proposed action: %v", err)
	}

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	body, _ := json.Marshal(map[string]string{"action": "approve"})
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/notifications/"+n.ID+"/action", bytes.NewReader(body))
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
	if result["status"] != string(notification.StatusApproved) {
		t.Errorf("want status approved, got %v", result["status"])
	}
}
