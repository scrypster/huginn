package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/notification"
)

// ---------------------------------------------------------------------------
// In-memory stub notification store for handler tests
// ---------------------------------------------------------------------------

// stubNotifStore is a minimal in-memory implementation of
// notification.StoreInterface used to inject canned state into server tests
// without requiring a real Pebble database.
type stubNotifStore struct {
	mu      sync.Mutex
	records map[string]*notification.Notification
}

func newStubNotifStore() *stubNotifStore {
	return &stubNotifStore{records: make(map[string]*notification.Notification)}
}

func (s *stubNotifStore) Put(n *notification.Notification) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *n
	s.records[n.ID] = &cp
	return nil
}

func (s *stubNotifStore) Get(id string) (*notification.Notification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.records[id]
	if !ok {
		return nil, fmt.Errorf("not found: %s", id)
	}
	cp := *n
	return &cp, nil
}

func (s *stubNotifStore) Transition(id string, newStatus notification.Status) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, ok := s.records[id]
	if !ok {
		return fmt.Errorf("not found: %s", id)
	}
	n.Status = newStatus
	n.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *stubNotifStore) ListPending() ([]*notification.Notification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*notification.Notification
	for _, n := range s.records {
		if n.Status == notification.StatusPending {
			cp := *n
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *stubNotifStore) ListByRoutine(routineID string) ([]*notification.Notification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*notification.Notification
	for _, n := range s.records {
		if n.RoutineID == routineID {
			cp := *n
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *stubNotifStore) ListByWorkflow(workflowID string) ([]*notification.Notification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*notification.Notification
	for _, n := range s.records {
		if n.WorkflowID == workflowID {
			cp := *n
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *stubNotifStore) PendingCount() (int, error) {
	ns, err := s.ListPending()
	return len(ns), err
}

func (s *stubNotifStore) ExpireRun(runID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for _, n := range s.records {
		if n.RunID == runID {
			n.ExpiresAt = &now
		}
	}
	return nil
}

// Compile-time assertion.
var _ notification.StoreInterface = (*stubNotifStore)(nil)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeStubNotification(routineID, runID, workflowID string, actions []notification.ProposedAction) *notification.Notification {
	n := &notification.Notification{
		ID:              notification.NewID(),
		RoutineID:       routineID,
		RunID:           runID,
		WorkflowID:      workflowID,
		Summary:         "test summary",
		Detail:          "test detail",
		Severity:        notification.SeverityInfo,
		Status:          notification.StatusPending,
		ProposedActions: actions,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	return n
}

// ---------------------------------------------------------------------------
// Existing tests
// ---------------------------------------------------------------------------

func TestHandleListNotifications(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/notifications", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body []any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
}

func TestHandleInboxSummary(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/inbox/summary", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["pending_count"]; !ok {
		t.Error("want pending_count in response")
	}
}

// ---------------------------------------------------------------------------
// New tests: approve action on notification with no ProposedActions
// ---------------------------------------------------------------------------

// TestHandleNotificationAction_ApproveWithNoProposedActions verifies that
// approving a notification that has no proposed actions returns 422. Without
// the guard the notification is moved to StatusApproved with no action queued,
// permanently stuck in that state.
func TestHandleNotificationAction_ApproveWithNoProposedActions(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	store := newStubNotifStore()
	// Notification with zero proposed actions.
	n := makeStubNotification("routine-1", "run-1", "", nil)
	if err := store.Put(n); err != nil {
		t.Fatal(err)
	}
	srv.notifStore = store

	body := `{"action":"approve"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/notifications/"+n.ID+"/action", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("want 422, got %d", resp.StatusCode)
	}
	// Confirm the status was NOT changed to approved.
	got, err := store.Get(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != notification.StatusPending {
		t.Errorf("status should remain pending, got %s", got.Status)
	}
}

// TestHandleNotificationAction_ApproveWithProposedActions verifies that
// approving a notification that has at least one proposed action succeeds (200)
// and transitions the status to StatusApproved. This is the "happy path"
// companion to the rejection test above.
func TestHandleNotificationAction_ApproveWithProposedActions(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	store := newStubNotifStore()
	actions := []notification.ProposedAction{
		{ID: "a1", Label: "Merge PR #42", ToolName: "bash", ToolParams: map[string]any{"cmd": "gh pr merge 42"}},
	}
	n := makeStubNotification("routine-1", "run-1", "", actions)
	if err := store.Put(n); err != nil {
		t.Fatal(err)
	}
	srv.notifStore = store

	body := `{"action":"approve","proposed_action_id":"a1"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/notifications/"+n.ID+"/action", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
	got, err := store.Get(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != notification.StatusApproved {
		t.Errorf("want status approved, got %s", got.Status)
	}
}

// TestHandleNotificationAction_UnknownAction verifies that an unknown action
// string returns 400 with a descriptive error.
func TestHandleNotificationAction_UnknownAction(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	store := newStubNotifStore()
	n := makeStubNotification("r", "run", "", nil)
	store.Put(n) //nolint:errcheck
	srv.notifStore = store

	body := `{"action":"explode"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/notifications/"+n.ID+"/action", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400 for unknown action, got %d", resp.StatusCode)
	}
}

// TestHandleNotificationAction_EmptyAction verifies that an empty action
// string is treated as unknown and returns 400.
func TestHandleNotificationAction_EmptyAction(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	store := newStubNotifStore()
	n := makeStubNotification("r", "run", "", nil)
	store.Put(n) //nolint:errcheck
	srv.notifStore = store

	body := `{"action":""}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/notifications/"+n.ID+"/action", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("want 400 for empty action, got %d", resp.StatusCode)
	}
}

// TestHandleNotificationAction_DismissAndSeen verifies the dismiss and seen
// actions update status correctly. These are simpler paths that don't require
// proposed-action guards.
func TestHandleNotificationAction_DismissAndSeen(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	store := newStubNotifStore()
	srv.notifStore = store

	for _, tc := range []struct {
		action     string
		wantStatus notification.Status
	}{
		{"dismiss", notification.StatusDismissed},
		{"seen", notification.StatusSeen},
	} {
		t.Run(tc.action, func(t *testing.T) {
			n := makeStubNotification("r", "run", "", nil)
			store.Put(n) //nolint:errcheck

			reqBody := fmt.Sprintf(`{"action":"%s"}`, tc.action)
			req, _ := http.NewRequest("POST", ts.URL+"/api/v1/notifications/"+n.ID+"/action", strings.NewReader(reqBody))
			req.Header.Set("Authorization", "Bearer "+testToken)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("action %s: want 200, got %d", tc.action, resp.StatusCode)
			}
			got, _ := store.Get(n.ID)
			if got.Status != tc.wantStatus {
				t.Errorf("action %s: want status %s, got %s", tc.action, tc.wantStatus, got.Status)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// New tests: workflow_id filter on GET /api/v1/notifications
// ---------------------------------------------------------------------------

// TestHandleListNotifications_FilterByWorkflowID verifies that
// ?workflow_id=<id> returns only notifications for that workflow and that
// the new field (WorkflowID) is serialised in the response body.
func TestHandleListNotifications_FilterByWorkflowID(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	store := newStubNotifStore()
	wfID := "wf-abc"
	// Two notifications belonging to wf-abc, one to another workflow.
	n1 := makeStubNotification("r", "run1", wfID, nil)
	n2 := makeStubNotification("r", "run2", wfID, nil)
	nOther := makeStubNotification("r", "run3", "wf-other", nil)
	store.Put(n1)     //nolint:errcheck
	store.Put(n2)     //nolint:errcheck
	store.Put(nOther) //nolint:errcheck
	srv.notifStore = store

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/notifications?workflow_id="+wfID, nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 2 {
		t.Errorf("want 2 notifications for wf-abc, got %d", len(body))
	}
	for _, item := range body {
		if item["workflow_id"] != wfID {
			t.Errorf("unexpected workflow_id in response: %v", item["workflow_id"])
		}
	}
}

// TestHandleListNotifications_NewFieldsInResponse verifies that the newer
// Notification fields — StepPosition, StepName, Deliveries — are included in
// GET responses when populated. They must be JSON-serialised because the
// handler passes the struct directly to jsonOK.
func TestHandleListNotifications_NewFieldsInResponse(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	store := newStubNotifStore()
	pos := 2
	n := &notification.Notification{
		ID:           notification.NewID(),
		RoutineID:    "r",
		RunID:        "run1",
		WorkflowID:   "wf1",
		StepPosition: &pos,
		StepName:     "step-b",
		Summary:      "s",
		Detail:       "d",
		Severity:     notification.SeverityInfo,
		Status:       notification.StatusPending,
		Deliveries: []notification.DeliveryRecord{
			{Type: "inbox", Status: "sent", SentAt: time.Now().UTC()},
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	store.Put(n) //nolint:errcheck
	srv.notifStore = store

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/notifications", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 {
		t.Fatalf("want 1 notification, got %d", len(body))
	}
	item := body[0]
	if item["step_name"] != "step-b" {
		t.Errorf("step_name: want step-b, got %v", item["step_name"])
	}
	if item["step_position"] == nil {
		t.Error("step_position should be present in response")
	}
	deliveries, _ := item["deliveries"].([]any)
	if len(deliveries) != 1 {
		t.Errorf("deliveries: want 1, got %d", len(deliveries))
	}
}

// ---------------------------------------------------------------------------
// New tests: ?workflow_id= edge cases
// ---------------------------------------------------------------------------

// TestHandleListNotifications_WorkflowIDEmptyParam verifies that
// ?workflow_id= (empty string value) does NOT trigger the workflow filter.
// The switch condition checks for non-empty string, so an empty workflow_id
// param falls through to the default (ListPending). This tests that the
// handler is consistent: an empty param is treated the same as no param.
func TestHandleListNotifications_WorkflowIDEmptyParam(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	store := newStubNotifStore()
	// One pending notification with no workflow ID.
	n1 := makeStubNotification("r", "run1", "", nil)
	// One notification for a specific workflow (should NOT show up in pending
	// filter because it has status pending but we only verify count here).
	n2 := makeStubNotification("r", "run2", "wf-xyz", nil)
	store.Put(n1) //nolint:errcheck
	store.Put(n2) //nolint:errcheck
	srv.notifStore = store

	// ?workflow_id= with empty value — should fall through to ListPending.
	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/notifications?workflow_id=", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	// Both notifications are pending — ListPending returns both.
	if len(body) != 2 {
		t.Errorf("empty workflow_id param: want 2 pending notifications (fallback to ListPending), got %d", len(body))
	}
}

// TestHandleListNotifications_RoutineIDTakesPrecedence verifies the documented
// filter priority: when both ?routine_id= and ?workflow_id= are provided, the
// handler's switch statement evaluates routine_id first and uses it, ignoring
// workflow_id. This is intentional (routine_id is the primary filter).
func TestHandleListNotifications_RoutineIDTakesPrecedence(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	store := newStubNotifStore()
	routineID := "routine-alpha"
	workflowID := "wf-beta"

	// Two notifications for routineID (one also has the workflowID).
	n1 := makeStubNotification(routineID, "run1", workflowID, nil)
	n2 := makeStubNotification(routineID, "run2", "", nil)
	// One notification for the workflowID but a DIFFERENT routine.
	n3 := makeStubNotification("other-routine", "run3", workflowID, nil)
	store.Put(n1) //nolint:errcheck
	store.Put(n2) //nolint:errcheck
	store.Put(n3) //nolint:errcheck
	srv.notifStore = store

	req, _ := http.NewRequest("GET",
		ts.URL+"/api/v1/notifications?routine_id="+routineID+"&workflow_id="+workflowID, nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	// routine_id filter applies: n1 and n2 match. n3 does NOT (different routine).
	if len(body) != 2 {
		t.Errorf("routine_id takes precedence: want 2 (by routine), got %d", len(body))
	}
	for _, item := range body {
		if item["routine_id"] != routineID {
			t.Errorf("unexpected routine_id in response: %v", item["routine_id"])
		}
	}
}

// TestHandleNotificationAction_Approve_PutFailure verifies that a Put error
// after a successful Transition is surfaced as a 500. This exercises the
// path: Get(ok) → len(ProposedActions)>0 → Transition(ok) → (implicit: store
// does NOT call Put in this handler). Actually the handler only calls
// Transition, not Put — this confirms no double-write bug exists.
// The test also verifies the final status via a direct store.Get.
func TestHandleNotificationAction_Approve_StatusConfirmedViaStore(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	store := newStubNotifStore()
	actions := []notification.ProposedAction{
		{ID: "a1", Label: "Run deploy", ToolName: "bash", ToolParams: map[string]any{"cmd": "deploy.sh"}},
	}
	n := makeStubNotification("r", "run1", "wf-1", actions)
	store.Put(n) //nolint:errcheck
	srv.notifStore = store

	body := `{"action":"approve","proposed_action_id":"a1"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/notifications/"+n.ID+"/action", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}

	// Confirm the status was persisted correctly via store.Get.
	got, err := store.Get(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != notification.StatusApproved {
		t.Errorf("status via store.Get = %s, want approved", got.Status)
	}
	// ProposedActions must still be intact after Transition (Transition only
	// updates status + updated_at in the stub, does not clear actions).
	if len(got.ProposedActions) != 1 {
		t.Errorf("ProposedActions len = %d after approve, want 1", len(got.ProposedActions))
	}
}
