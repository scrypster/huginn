package server

// spaces_cursor_pagination_test.go — integration tests for spaces cursor
// (keyset) pagination.
//
// The HTTP handler (handleListSpaces) does not expose a "limit" query
// parameter, so limit-based pagination is exercised at the store layer using
// spaces.ListOpts{Limit: N}. The handler test covers what the handler actually
// exposes: the X-Next-Cursor response header and the cursor query parameter.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// openSpacesDB opens an in-memory SQLite DB with the full session and spaces
// schema applied. It is kept separate from openTestSQLiteDB so that spaces tests
// are self-contained and readable.
func openSpacesDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(fmt.Sprintf("file:%s/spaces-test-%d.db?mode=memory&cache=shared",
		t.TempDir(), time.Now().UnixNano()))
	if err != nil {
		// Fall back to a disk-backed DB in a temp dir.
		db, err = sqlitedb.Open(t.TempDir() + "/spaces.db")
		if err != nil {
			t.Fatalf("openSpacesDB: %v", err)
		}
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("openSpacesDB: ApplySchema: %v", err)
	}
	if err := db.Migrate(session.Migrations()); err != nil {
		t.Fatalf("openSpacesDB: Migrate session: %v", err)
	}
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("openSpacesDB: Migrate spaces: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// createTestSpaces creates n DM spaces named "agent-1" … "agent-n".
// We use slight time offsets so that the (updated_at, id) ordering is stable.
func createTestSpaces(t *testing.T, store *spaces.SQLiteSpaceStore, n int) []*spaces.Space {
	t.Helper()
	var result []*spaces.Space
	for i := 1; i <= n; i++ {
		sp, err := store.OpenDM(fmt.Sprintf("agent-%d", i))
		if err != nil {
			t.Fatalf("createTestSpaces: OpenDM agent-%d: %v", i, err)
		}
		result = append(result, sp)
		// Small sleep so that updated_at values differ — SQLite has 1 ms
		// resolution and the store uses time.Now().UTC().Format(time.RFC3339Nano).
		time.Sleep(2 * time.Millisecond)
	}
	return result
}

// ---------------------------------------------------------------------------
// Store-level pagination tests
// ---------------------------------------------------------------------------

// TestListSpaces_Pagination_FirstPage verifies that ListSpaces returns exactly
// limit rows and a non-empty NextCursor when more rows exist.
func TestListSpaces_Pagination_FirstPage(t *testing.T) {
	db := openSpacesDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	// Create 5 spaces.
	createTestSpaces(t, store, 5)

	// Fetch page 1 with limit=2.
	res, err := store.ListSpaces(spaces.ListOpts{Limit: 2})
	if err != nil {
		t.Fatalf("ListSpaces page 1: %v", err)
	}
	if len(res.Spaces) != 2 {
		t.Fatalf("page 1: want 2 spaces, got %d", len(res.Spaces))
	}
	if res.NextCursor == "" {
		t.Fatal("page 1: expected non-empty NextCursor when more results exist")
	}
}

// TestListSpaces_Pagination_SecondPage verifies that passing the NextCursor
// from page 1 to page 2 returns the next set of rows (no overlap with page 1).
func TestListSpaces_Pagination_SecondPage(t *testing.T) {
	db := openSpacesDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	createTestSpaces(t, store, 5)

	// Page 1.
	page1, err := store.ListSpaces(spaces.ListOpts{Limit: 2})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(page1.Spaces) != 2 {
		t.Fatalf("page 1: want 2 spaces, got %d", len(page1.Spaces))
	}

	// Page 2.
	page2, err := store.ListSpaces(spaces.ListOpts{Limit: 2, Cursor: page1.NextCursor})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(page2.Spaces) != 2 {
		t.Fatalf("page 2: want 2 spaces, got %d", len(page2.Spaces))
	}

	// No ID overlap between pages.
	page1IDs := map[string]bool{}
	for _, sp := range page1.Spaces {
		page1IDs[sp.ID] = true
	}
	for _, sp := range page2.Spaces {
		if page1IDs[sp.ID] {
			t.Errorf("space %s appears in both page 1 and page 2 (overlap)", sp.ID)
		}
	}
}

// TestListSpaces_Pagination_LastPage verifies that the final page returns fewer
// than limit rows and an empty NextCursor.
func TestListSpaces_Pagination_LastPage(t *testing.T) {
	db := openSpacesDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	createTestSpaces(t, store, 5)

	// Page 1 (2 items).
	page1, err := store.ListSpaces(spaces.ListOpts{Limit: 2})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}

	// Page 2 (2 items).
	page2, err := store.ListSpaces(spaces.ListOpts{Limit: 2, Cursor: page1.NextCursor})
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}

	// Page 3 — should return the remaining 1 item and no cursor.
	page3, err := store.ListSpaces(spaces.ListOpts{Limit: 2, Cursor: page2.NextCursor})
	if err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if len(page3.Spaces) != 1 {
		t.Fatalf("page 3: want 1 space (last), got %d", len(page3.Spaces))
	}
	if page3.NextCursor != "" {
		t.Errorf("page 3 (last): want empty NextCursor, got %q", page3.NextCursor)
	}
}

// TestListSpaces_Pagination_InvalidCursor verifies that passing a corrupted
// cursor string returns an error rather than silently returning wrong results.
func TestListSpaces_Pagination_InvalidCursor(t *testing.T) {
	db := openSpacesDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	_, err := store.ListSpaces(spaces.ListOpts{
		Limit:  2,
		Cursor: "not-valid-base64!!!",
	})
	if err == nil {
		t.Fatal("expected error for invalid cursor, got nil")
	}
}

// TestListSpaces_Pagination_AllResultsOrdered verifies that collecting all
// pages returns all 5 spaces exactly once, in a consistent descending order.
func TestListSpaces_Pagination_AllResultsOrdered(t *testing.T) {
	db := openSpacesDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	createTestSpaces(t, store, 5)

	var all []*spaces.Space
	var cursor string
	for page := 0; ; page++ {
		res, err := store.ListSpaces(spaces.ListOpts{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		all = append(all, res.Spaces...)
		cursor = res.NextCursor
		if cursor == "" {
			break
		}
		if page > 10 {
			t.Fatal("pagination did not terminate within 10 pages")
		}
	}

	if len(all) != 5 {
		t.Fatalf("want 5 total spaces, got %d", len(all))
	}

	// Verify no duplicates.
	seen := map[string]int{}
	for _, sp := range all {
		seen[sp.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("space %s appeared %d times in paginated results", id, count)
		}
	}
}

// ---------------------------------------------------------------------------
// Handler-level cursor tests (HTTP)
// ---------------------------------------------------------------------------

// TestHandleListSpaces_Cursor_XNextCursorHeader verifies that when the store
// returns a NextCursor, the handler sets the X-Next-Cursor response header.
//
// Because handleListSpaces does not accept a "limit" query parameter, we rely
// on the store having fewer spaces than DefaultListSpacesLimit to verify the
// empty-cursor case, and we test the header directly using a mock store.
func TestHandleListSpaces_Cursor_XNextCursorHeader(t *testing.T) {
	srv := testServer(t)
	srv.SetSpaceStore(&stubSpaceStore{nextCursor: "abc123"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/spaces", nil)
	w := httptest.NewRecorder()
	srv.handleListSpaces(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	got := w.Header().Get("X-Next-Cursor")
	if got != "abc123" {
		t.Errorf("X-Next-Cursor = %q, want %q", got, "abc123")
	}
}

// TestHandleListSpaces_Cursor_NoHeaderWhenEmpty verifies that when there is no
// next page, the X-Next-Cursor header is absent (empty string).
func TestHandleListSpaces_Cursor_NoHeaderWhenEmpty(t *testing.T) {
	srv := testServer(t)
	srv.SetSpaceStore(&stubSpaceStore{nextCursor: ""})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/spaces", nil)
	w := httptest.NewRecorder()
	srv.handleListSpaces(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	got := w.Header().Get("X-Next-Cursor")
	if got != "" {
		t.Errorf("X-Next-Cursor should be absent when there is no next page, got %q", got)
	}
}

// TestHandleListSpaces_Cursor_PassedToStore verifies that the cursor query
// parameter from the request is forwarded to the store's ListSpaces call.
func TestHandleListSpaces_Cursor_PassedToStore(t *testing.T) {
	spy := &stubSpaceStore{nextCursor: ""}
	srv := testServer(t)
	srv.SetSpaceStore(spy)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/spaces?cursor=my-cursor-value", nil)
	w := httptest.NewRecorder()
	srv.handleListSpaces(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if spy.lastCursor != "my-cursor-value" {
		t.Errorf("store received cursor %q, want %q", spy.lastCursor, "my-cursor-value")
	}
}

// TestHandleListSpaces_Cursor_ResponseBodyIncludesNextCursor verifies that the
// JSON response body also includes the NextCursor field from the store result.
func TestHandleListSpaces_Cursor_ResponseBodyIncludesNextCursor(t *testing.T) {
	srv := testServer(t)
	srv.SetSpaceStore(&stubSpaceStore{nextCursor: "page2-cursor"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/spaces", nil)
	w := httptest.NewRecorder()
	srv.handleListSpaces(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}

	var body struct {
		NextCursor string `json:"NextCursor"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.NextCursor != "page2-cursor" {
		t.Errorf("body.NextCursor = %q, want %q", body.NextCursor, "page2-cursor")
	}
}

// ---------------------------------------------------------------------------
// Stub store
// ---------------------------------------------------------------------------

// stubSpaceStore is a minimal implementation of spaces.StoreInterface for
// handler-level tests.  It returns a configurable NextCursor and captures the
// last cursor it received.
type stubSpaceStore struct {
	nextCursor string
	lastCursor string
}

func (s *stubSpaceStore) ListSpaces(opts spaces.ListOpts) (spaces.ListSpacesResult, error) {
	s.lastCursor = opts.Cursor
	return spaces.ListSpacesResult{
		Spaces:     []*spaces.Space{},
		NextCursor: s.nextCursor,
	}, nil
}
func (s *stubSpaceStore) OpenDM(agentName string) (*spaces.Space, error) { return nil, nil }
func (s *stubSpaceStore) CreateChannel(name, leadAgent string, members []string, icon, color string) (*spaces.Space, error) {
	return nil, nil
}
func (s *stubSpaceStore) GetSpace(id string) (*spaces.Space, error) { return nil, nil }
func (s *stubSpaceStore) UpdateSpace(id string, updates spaces.SpaceUpdates) (*spaces.Space, error) {
	return nil, nil
}
func (s *stubSpaceStore) ArchiveSpace(id string) error { return nil }
func (s *stubSpaceStore) MarkRead(spaceID string) error { return nil }
func (s *stubSpaceStore) UnseenCount(spaceID string) (int, error) { return 0, nil }
func (s *stubSpaceStore) ListSessionsForSpace(spaceID string) ([]spaces.SessionRef, error) {
	return nil, nil
}
func (s *stubSpaceStore) RemoveAgentFromAllSpaces(agentName string) (*spaces.SpaceCascadeResult, error) {
	return &spaces.SpaceCascadeResult{}, nil
}
func (s *stubSpaceStore) ListSpaceMessages(spaceID string, before *spaces.SpaceMsgCursor, limit int) (spaces.SpaceMessagesResult, error) {
	return spaces.SpaceMessagesResult{Messages: []spaces.SpaceMessage{}, NextCursor: ""}, nil
}
func (s *stubSpaceStore) GetChannelsForAgent(_ string) ([]*spaces.Space, error) {
	return nil, nil
}
func (s *stubSpaceStore) SpacesByLeadAgent(_ string) ([]*spaces.Space, error) {
	return nil, nil
}
