package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/threadmgr"
)

// TestDeleteSession_CallsCleanupSession verifies that handleDeleteSession
// calls tm.CleanupSession before deleting the session record.
// This prevents orphaned threads from running indefinitely when a session is deleted.
func TestDeleteSession_CallsCleanupSession(t *testing.T) {
	srv, ts := newTestServer(t)

	// Step 1: Create a session
	sessDir := t.TempDir()
	store := session.NewStore(sessDir)
	sess := store.New("test-session-1", "/tmp", "claude-haiku-4")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("save session manifest: %v", err)
	}

	// Step 2: Create a thread manager and wire it to the server
	tm := threadmgr.New()
	srv.SetThreadManager(tm)
	srv.store = store // Override the store with our test store

	// Step 3: Create a thread for the session
	thr, err := tm.Create(threadmgr.CreateParams{
		SessionID: sess.ID,
		AgentID:   "test-agent",
		Task:      "test task",
	})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}

	// Verify thread exists before deletion
	_, exists := tm.Get(thr.ID)
	if !exists {
		t.Fatal("thread should exist before session deletion")
	}

	// Step 4: Delete the session via HTTP
	req, _ := http.NewRequest("DELETE", ts.URL+"/api/v1/sessions/"+sess.ID, nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 from DELETE session, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if deleted, ok := body["deleted"].(bool); !ok || !deleted {
		t.Fatalf("expected deleted: true, got %v", body["deleted"])
	}

	// Step 5: Verify the thread was cleaned up (removed from manager)
	// If CleanupSession was not called, the thread would still exist
	_, threadStillExists := tm.Get(thr.ID)
	if threadStillExists {
		t.Fatal("thread should have been removed by CleanupSession when the session was deleted")
	}

	// Step 6: Verify the session was actually deleted from the store
	if store.Exists(sess.ID) {
		t.Fatal("expected session to be deleted from store")
	}
}
