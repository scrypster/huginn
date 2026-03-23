package spaces_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// openWorkstreamStoreDirect opens a fresh temp SQLite DB and applies the
// workstream migration, returning a WorkstreamStore ready for tests.
func openWorkstreamStoreDirect(t *testing.T, dir string) *spaces.WorkstreamStore {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(dir, "ws.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	if err := db.Migrate(spaces.WorkstreamMigrations()); err != nil {
		t.Fatalf("migrate workstreams: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return spaces.NewWorkstreamStore(db)
}

// openWS is the canonical per-test helper.
func openWS(t *testing.T) *spaces.WorkstreamStore {
	t.Helper()
	return openWorkstreamStoreDirect(t, t.TempDir())
}

// ── Create ────────────────────────────────────────────────────────────────────

func TestWorkstream_Create_Basic(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	ws, err := store.Create(ctx, "ops-2026", "Ops initiative Q1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ws.ID == "" {
		t.Error("expected non-empty ID")
	}
	if ws.Name != "ops-2026" {
		t.Errorf("expected name %q, got %q", "ops-2026", ws.Name)
	}
	if ws.Description != "Ops initiative Q1" {
		t.Errorf("expected description %q, got %q", "Ops initiative Q1", ws.Description)
	}
	if ws.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
}

func TestWorkstream_Create_EmptyName_ReturnsError(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	_, err := store.Create(ctx, "", "some desc")
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
}

func TestWorkstream_Create_NoDescription(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	ws, err := store.Create(ctx, "minimal", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ws.Description != "" {
		t.Errorf("expected empty description, got %q", ws.Description)
	}
}

// ── List ──────────────────────────────────────────────────────────────────────

func TestWorkstream_List_EmptyDB_ReturnsEmptySlice(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list == nil {
		t.Fatal("expected non-nil slice, got nil")
	}
	if len(list) != 0 {
		t.Errorf("expected 0, got %d", len(list))
	}
}

func TestWorkstream_List_MultipleEntries(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	for _, name := range []string{"alpha", "beta", "gamma"} {
		if _, err := store.Create(ctx, name, ""); err != nil {
			t.Fatalf("Create %q: %v", name, err)
		}
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("expected 3 workstreams, got %d", len(list))
	}
}

// ── Get ───────────────────────────────────────────────────────────────────────

func TestWorkstream_Get_ReturnsCorrectEntry(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	created, _ := store.Create(ctx, "my-project", "the one project")
	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID mismatch: %s vs %s", got.ID, created.ID)
	}
	if got.Name != "my-project" {
		t.Errorf("Name mismatch: %q", got.Name)
	}
	if got.Description != "the one project" {
		t.Errorf("Description mismatch: %q", got.Description)
	}
}

func TestWorkstream_Get_UnknownID_ReturnsError(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	_, err := store.Get(ctx, "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for unknown ID, got nil")
	}
}

// ── Delete ────────────────────────────────────────────────────────────────────

func TestWorkstream_Delete_RemovesEntry(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	ws, _ := store.Create(ctx, "to-delete", "")
	if err := store.Delete(ctx, ws.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := store.Get(ctx, ws.ID)
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestWorkstream_Delete_UnknownID_ReturnsError(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	err := store.Delete(ctx, "ghost-id")
	if err == nil {
		t.Fatal("expected error for unknown ID, got nil")
	}
}

func TestWorkstream_Delete_DoesNotAffectOthers(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	ws1, _ := store.Create(ctx, "keep-me", "")
	ws2, _ := store.Create(ctx, "delete-me", "")

	_ = store.Delete(ctx, ws2.ID)

	list, _ := store.List(ctx)
	if len(list) != 1 {
		t.Fatalf("expected 1 remaining, got %d", len(list))
	}
	if list[0].ID != ws1.ID {
		t.Errorf("remaining workstream should be %s, got %s", ws1.ID, list[0].ID)
	}
}

// ── TagSession / ListSessions ─────────────────────────────────────────────────

func TestWorkstream_TagSession_Basic(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	ws, _ := store.Create(ctx, "project-x", "")
	if err := store.TagSession(ctx, ws.ID, "sess-001"); err != nil {
		t.Fatalf("TagSession: %v", err)
	}
}

func TestWorkstream_TagSession_Idempotent(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	ws, _ := store.Create(ctx, "project-y", "")
	for range 3 {
		if err := store.TagSession(ctx, ws.ID, "sess-002"); err != nil {
			t.Fatalf("TagSession (idempotent): %v", err)
		}
	}
}

func TestWorkstream_ListSessions_Empty(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	ws, _ := store.Create(ctx, "empty-project", "")
	ids, err := store.ListSessions(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if ids == nil {
		t.Fatal("expected non-nil slice")
	}
	if len(ids) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(ids))
	}
}

func TestWorkstream_ListSessions_Multiple(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	ws, _ := store.Create(ctx, "busy-project", "")
	for _, sessID := range []string{"sess-A", "sess-B", "sess-C"} {
		if err := store.TagSession(ctx, ws.ID, sessID); err != nil {
			t.Fatalf("TagSession %q: %v", sessID, err)
		}
	}

	ids, err := store.ListSessions(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(ids) != 3 {
		t.Errorf("expected 3 sessions, got %d", len(ids))
	}
}

func TestWorkstream_Delete_CascadesSessionTags(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	ws, _ := store.Create(ctx, "cascade-test", "")
	_ = store.TagSession(ctx, ws.ID, "sess-X")
	_ = store.TagSession(ctx, ws.ID, "sess-Y")

	if err := store.Delete(ctx, ws.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// After cascading delete, listing sessions for the deleted workstream should
	// return an empty slice (the rows were deleted by CASCADE).
	ids, err := store.ListSessions(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ListSessions after delete: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected 0 sessions after cascade delete, got %d", len(ids))
	}
}

func TestWorkstream_SessionCanBelongToMultipleWorkstreams(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()

	ws1, _ := store.Create(ctx, "ws-one", "")
	ws2, _ := store.Create(ctx, "ws-two", "")
	sharedSession := "sess-shared"

	_ = store.TagSession(ctx, ws1.ID, sharedSession)
	_ = store.TagSession(ctx, ws2.ID, sharedSession)

	ids1, _ := store.ListSessions(ctx, ws1.ID)
	ids2, _ := store.ListSessions(ctx, ws2.ID)

	if len(ids1) != 1 || ids1[0] != sharedSession {
		t.Errorf("ws1 sessions: got %v", ids1)
	}
	if len(ids2) != 1 || ids2[0] != sharedSession {
		t.Errorf("ws2 sessions: got %v", ids2)
	}
}
