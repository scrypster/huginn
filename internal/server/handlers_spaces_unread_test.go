package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/spaces"
)

// TestSpaceUnreadBadge_PostMessageIncrementsThenMarkReadClears is the rubric
// 3.5 deliverable: verify the unseen_count computation (spaces store) + the
// mark-read endpoint + the count the web badge reads actually agree —
// posting a message to a space session bumps unseen_count, and marking the
// space read brings it back to 0.
func TestSpaceUnreadBadge_PostMessageIncrementsThenMarkReadClears(t *testing.T) {
	srv := testServer(t)
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("migrate spaces: %v", err)
	}
	spaceStore := spaces.NewSQLiteSpaceStore(db)
	srv.SetSpaceStore(spaceStore)

	// Real message persistence (Server.persistInboundUserMessage /
	// runWSChat) goes through session.SQLiteSessionStore.Append, which is
	// what actually bumps sessions.updated_at — the field UnseenCount reads.
	// Bind it to the SAME db as the space store, exactly as production does
	// when SQLite session storage is configured, so this test exercises the
	// real cross-store mechanism rather than a mock.
	sessionStore := session.NewSQLiteSessionStore(db)
	srv.store = sessionStore

	sp, err := spaceStore.CreateChannel("unread-test", "", nil, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	sess := sessionStore.New("unread-test-session", "/tmp", "test-model")
	sess.Manifest.SpaceID = sp.ID
	if err := sessionStore.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Establish a read baseline so session creation itself doesn't count as
	// unread noise for this assertion.
	markRead := func() {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/spaces/"+sp.ID+"/mark-read", nil)
		req.SetPathValue("id", sp.ID)
		w := httptest.NewRecorder()
		srv.handleMarkSpaceRead(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("mark-read: expected 200, got %d: %s", w.Code, w.Body.String())
		}
	}
	unseenCount := func() int {
		t.Helper()
		count, err := spaceStore.UnseenCount(sp.ID)
		if err != nil {
			t.Fatalf("UnseenCount: %v", err)
		}
		return count
	}

	markRead()
	if got := unseenCount(); got != 0 {
		t.Fatalf("after initial mark-read: unseen_count = %d, want 0", got)
	}

	// Posting a message must bump sessions.updated_at past the mark-read
	// timestamp. Sleep briefly since last_read_at/updated_at are second-ish
	// resolution RFC3339 timestamps.
	time.Sleep(1100 * time.Millisecond)
	if err := sessionStore.Append(sess, session.SessionMessage{
		Role:    "user",
		Content: "hello space",
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if got := unseenCount(); got != 1 {
		t.Fatalf("after posting a message: unseen_count = %d, want 1", got)
	}

	// The list-spaces payload the web badge actually renders from must
	// agree with the store-level count.
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/spaces", nil)
	listW := httptest.NewRecorder()
	srv.handleListSpaces(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("list spaces: expected 200, got %d: %s", listW.Code, listW.Body.String())
	}
	var listResult spaces.ListSpacesResult
	if err := json.NewDecoder(listW.Body).Decode(&listResult); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	found := false
	for _, s := range listResult.Spaces {
		if s.ID == sp.ID {
			found = true
			if s.UnseenCount != 1 {
				t.Errorf("list-spaces payload UnseenCount = %d, want 1", s.UnseenCount)
			}
		}
	}
	if !found {
		t.Fatalf("space %s not present in list-spaces result", sp.ID)
	}

	markRead()
	if got := unseenCount(); got != 0 {
		t.Fatalf("after second mark-read: unseen_count = %d, want 0", got)
	}
}
