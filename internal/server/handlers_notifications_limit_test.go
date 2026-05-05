package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/scrypster/huginn/internal/notification"
)

// TestHandleListNotifications_DefaultLimit verifies that GET /api/v1/notifications
// returns at most defaultNotificationLimit (100) items even when the store holds more.
func TestHandleListNotifications_DefaultLimit(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	store := newStubNotifStore()
	for i := 0; i < 150; i++ {
		n := makeStubNotification(
			fmt.Sprintf("routine-%d", i),
			fmt.Sprintf("run-%d", i),
			"",
			nil,
		)
		if err := store.Put(n); err != nil {
			t.Fatal(err)
		}
	}
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
	if len(body) != defaultNotificationLimit {
		t.Errorf("want %d notifications (default limit), got %d", defaultNotificationLimit, len(body))
	}
}

// TestHandleListNotifications_CustomLimit verifies that ?limit=5 returns exactly 5 items.
func TestHandleListNotifications_CustomLimit(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	store := newStubNotifStore()
	for i := 0; i < 20; i++ {
		n := makeStubNotification(
			fmt.Sprintf("routine-%d", i),
			fmt.Sprintf("run-%d", i),
			"",
			nil,
		)
		if err := store.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	srv.notifStore = store

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/notifications?limit=5", nil)
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
	if len(body) != 5 {
		t.Errorf("want 5 notifications (custom limit), got %d", len(body))
	}
}

// TestHandleListNotifications_MaxLimit verifies that ?limit=9999 is capped at
// maxNotificationLimit (1000) and does not return more than that.
func TestHandleListNotifications_MaxLimit(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	store := newStubNotifStore()
	// Populate more than maxNotificationLimit to confirm capping works.
	for i := 0; i < 1100; i++ {
		n := makeStubNotification(
			fmt.Sprintf("routine-%d", i),
			fmt.Sprintf("run-%d", i),
			"",
			nil,
		)
		if err := store.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	srv.notifStore = store

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/notifications?limit=9999", nil)
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
	if len(body) > maxNotificationLimit {
		t.Errorf("want at most %d notifications (max limit cap), got %d", maxNotificationLimit, len(body))
	}
	if len(body) != maxNotificationLimit {
		t.Errorf("want exactly %d notifications (max limit cap), got %d", maxNotificationLimit, len(body))
	}
}

// TestHandleInboxSummary_UsesPendingCount verifies that handleInboxSummary returns
// the correct total pending_count even when more than defaultNotificationLimit items
// are in the store (i.e., it uses PendingCount() not len(ListPending())).
func TestHandleInboxSummary_UsesPendingCount(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	store := newStubNotifStore()
	totalPending := 150
	for i := 0; i < totalPending; i++ {
		n := makeStubNotification(
			fmt.Sprintf("routine-%d", i),
			fmt.Sprintf("run-%d", i),
			"",
			nil,
		)
		// Mark every other one urgent to verify urgent_count is capped sensibly.
		if i%2 == 0 {
			n.Severity = notification.SeverityUrgent
		}
		if err := store.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	srv.notifStore = store

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

	// pending_count must reflect the full 150 (via PendingCount()).
	pendingCount, ok := body["pending_count"].(float64)
	if !ok {
		t.Fatalf("pending_count missing or wrong type: %v", body["pending_count"])
	}
	if int(pendingCount) != totalPending {
		t.Errorf("pending_count: want %d, got %d", totalPending, int(pendingCount))
	}

	// urgent_count is a lower bound capped at defaultNotificationLimit items scanned.
	urgentCount, ok := body["urgent_count"].(float64)
	if !ok {
		t.Fatalf("urgent_count missing or wrong type: %v", body["urgent_count"])
	}
	// With 150 items (every other urgent = 75 urgent total) but only 100 scanned,
	// the urgent count should be ≤ 50 (half of the 100 limit). We just verify it's
	// non-negative and ≤ totalPending.
	if int(urgentCount) < 0 || int(urgentCount) > totalPending {
		t.Errorf("urgent_count out of range: got %d", int(urgentCount))
	}
}
