package server

// Iteration 2 hardening tests for the server package.
// Targets the zero-coverage handler functions: handleGetNotification,
// handleNotificationAction, handleDeleteRoutine, and server setter methods.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/scrypster/huginn/internal/notification"
)

func startTestServerFromMux(t *testing.T, mux *http.ServeMux) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// newTestServerWithNotifStore creates a test server wired with a real notification store.
func newTestServerWithNotifStore(t *testing.T) (*Server, *http.ServeMux, *notification.Store) {
	t.Helper()
	srv, _ := newTestServer(t)

	db, err := pebble.Open(t.TempDir(), &pebble.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ns := notification.NewStore(db)
	srv.SetNotificationStore(ns)
	return srv, nil, ns
}

// makeTestNotification stores a notification and returns it.
func makeTestNotification(t *testing.T, ns *notification.Store) *notification.Notification {
	t.Helper()
	n := &notification.Notification{
		ID:        notification.NewID(),
		RoutineID: "test-routine",
		RunID:     notification.NewID(),
		Summary:   "test summary",
		Detail:    "test detail",
		Severity:  notification.SeverityInfo,
		Status:    notification.StatusPending,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := ns.Put(n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Hardening 1: SetNotificationStore wires the store correctly.
func TestServer_SetNotificationStore(t *testing.T) {
	srv, _ := newTestServer(t)
	if srv.notifStore != nil {
		t.Error("want nil notifStore before SetNotificationStore")
	}
	db, err := pebble.Open(t.TempDir(), &pebble.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ns := notification.NewStore(db)
	srv.SetNotificationStore(ns)
	if srv.notifStore != ns {
		t.Error("want notifStore set after SetNotificationStore")
	}
}

// Hardening 2: handleGetNotification — no store configured → 503.
func TestHandleGetNotification_NoStore_503(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/notifications/some-id", nil)
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

// Hardening 3: handleGetNotification — notification found.
func TestHandleGetNotification_Found(t *testing.T) {
	srv, _, ns := newTestServerWithNotifStore(t)
	n := makeTestNotification(t, ns)

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := startTestServerFromMux(t, mux)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/notifications/"+n.ID, nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var result notification.Notification
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.ID != n.ID {
		t.Errorf("want ID %s, got %s", n.ID, result.ID)
	}
}

// Hardening 4: handleGetNotification — notification not found → 404.
func TestHandleGetNotification_NotFound_404(t *testing.T) {
	srv, _, _ := newTestServerWithNotifStore(t)

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := startTestServerFromMux(t, mux)

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/notifications/nonexistent-id", nil)
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

// Hardening 5: handleNotificationAction — dismiss action changes status.
func TestHandleNotificationAction_Dismiss(t *testing.T) {
	srv, _, ns := newTestServerWithNotifStore(t)
	n := makeTestNotification(t, ns)

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := startTestServerFromMux(t, mux)

	body, _ := json.Marshal(map[string]string{"action": "dismiss"})
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
	if result["status"] != string(notification.StatusDismissed) {
		t.Errorf("want status dismissed, got %v", result["status"])
	}
}

// Hardening 6: handleNotificationAction — unknown action → 400.
func TestHandleNotificationAction_UnknownAction_400(t *testing.T) {
	srv, _, ns := newTestServerWithNotifStore(t)
	n := makeTestNotification(t, ns)

	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	ts := startTestServerFromMux(t, mux)

	body, _ := json.Marshal(map[string]string{"action": "explode"})
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/notifications/"+n.ID+"/action", bytes.NewReader(body))
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

// Hardening 7: handleNotificationAction — no store configured → 503.
func TestHandleNotificationAction_NoStore_503(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{"action": "dismiss"})
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/notifications/abc/action", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Errorf("want 503, got %d", resp.StatusCode)
	}
}

// Hardening 8: Server.SetScheduler and SetWorkflowRunStore setter methods.
func TestServer_SetScheduler_SetWorkflowRunStore(t *testing.T) {
	srv, _ := newTestServer(t)

	// SetScheduler with nil (should not panic)
	srv.SetScheduler(nil)
	if srv.sched != nil {
		t.Error("want nil sched after SetScheduler(nil)")
	}

	// SetWorkflowRunStore with nil (should not panic)
	srv.SetWorkflowRunStore(nil)
	if srv.workflowRunStore != nil {
		t.Error("want nil workflowRunStore after SetWorkflowRunStore(nil)")
	}
}

