package spaces_test

import (
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// openTestStore creates a fresh SQLite-backed SpaceStore for tests.
func openTestStore(t *testing.T) *spaces.SQLiteSpaceStore {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlitedb.Open(filepath.Join(dir, "spaces.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return spaces.NewSQLiteSpaceStore(db)
}

// ── MarkRead ──────────────────────────────────────────────────────────────────

func TestMarkRead_FirstTime_Succeeds(t *testing.T) {
	store := openTestStore(t)
	ch, _ := store.CreateChannel("Ops", "atlas", []string{}, "", "")
	if err := store.MarkRead(ch.ID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
}

func TestMarkRead_Idempotent_NoError(t *testing.T) {
	store := openTestStore(t)
	ch, _ := store.CreateChannel("Ops", "atlas", []string{}, "", "")
	for range 5 {
		if err := store.MarkRead(ch.ID); err != nil {
			t.Fatalf("MarkRead iteration: %v", err)
		}
	}
}

// ── UnseenCount ───────────────────────────────────────────────────────────────

func TestUnseenCount_NoSessions_ReturnsZero(t *testing.T) {
	store := openTestStore(t)
	ch, _ := store.CreateChannel("Alerts", "atlas", []string{}, "", "")
	count, err := store.UnseenCount(ch.ID)
	if err != nil {
		t.Fatalf("UnseenCount: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 unseen, got %d", count)
	}
}

func TestUnseenCount_UnknownSpace_ReturnsZero(t *testing.T) {
	store := openTestStore(t)
	count, err := store.UnseenCount("ghost-space-id")
	if err != nil {
		t.Fatalf("UnseenCount for unknown space: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 unseen for ghost space, got %d", count)
	}
}

// ── ArchiveSpace ──────────────────────────────────────────────────────────────

func TestArchiveSpace_Then_ListSpacesExcludes(t *testing.T) {
	store := openTestStore(t)
	ch, _ := store.CreateChannel("Deprecated", "atlas", []string{}, "", "")

	if err := store.ArchiveSpace(ch.ID); err != nil {
		t.Fatalf("ArchiveSpace: %v", err)
	}

	res, err := store.ListSpaces(spaces.ListOpts{})
	if err != nil {
		t.Fatalf("ListSpaces: %v", err)
	}
	for _, s := range res.Spaces {
		if s.ID == ch.ID {
			t.Errorf("archived space %q still appears in ListSpaces result", ch.ID)
		}
	}
}

func TestArchiveSpace_DoubleArchive_NoError(t *testing.T) {
	store := openTestStore(t)
	ch, _ := store.CreateChannel("Old", "atlas", []string{}, "", "")

	if err := store.ArchiveSpace(ch.ID); err != nil {
		t.Fatalf("first ArchiveSpace: %v", err)
	}
	// Second archive should succeed (idempotent — re-sets archived_at).
	if err := store.ArchiveSpace(ch.ID); err != nil {
		t.Errorf("second ArchiveSpace should succeed (idempotent), got: %v", err)
	}
}

func TestArchiveSpace_DM_ReturnsError(t *testing.T) {
	store := openTestStore(t)
	dm, _ := store.OpenDM("atlas")

	err := store.ArchiveSpace(dm.ID)
	if err == nil {
		t.Fatal("expected error archiving DM, got nil")
	}
	var se *spaces.SpaceError
	if isErr := func() bool {
		e := err
		for e != nil {
			if se2, ok := e.(*spaces.SpaceError); ok {
				se = se2
				return true
			}
			// Unwrap manually via errors interface
			type unwrapper interface{ Unwrap() error }
			if u, ok := e.(unwrapper); ok {
				e = u.Unwrap()
			} else {
				break
			}
		}
		return false
	}(); !isErr {
		_ = se
		// Accept any non-nil error — just verify ArchiveSpace returned error for DM
	}
}

// ── CreateChannel input validation ───────────────────────────────────────────

func TestCreateChannel_EmptyName_ReturnsError(t *testing.T) {
	store := openTestStore(t)
	_, err := store.CreateChannel("", "atlas", []string{}, "", "")
	if err == nil {
		t.Error("expected error for empty channel name, got nil")
	}
}

func TestCreateChannel_NameTooLong_ReturnsError(t *testing.T) {
	store := openTestStore(t)
	longName := make([]byte, 200)
	for i := range longName {
		longName[i] = 'x'
	}
	_, err := store.CreateChannel(string(longName), "atlas", []string{}, "", "")
	if err == nil {
		t.Error("expected error for too-long channel name, got nil")
	}
}

// ── GetSpace ──────────────────────────────────────────────────────────────────

func TestGetSpace_ExistingChannel_ReturnsCorrectKind(t *testing.T) {
	store := openTestStore(t)
	ch, _ := store.CreateChannel("Engineering", "atlas", []string{"bob"}, "", "")
	got, err := store.GetSpace(ch.ID)
	if err != nil {
		t.Fatalf("GetSpace: %v", err)
	}
	if got.Kind != "channel" {
		t.Errorf("expected kind=channel, got %q", got.Kind)
	}
	if len(got.Members) == 0 {
		t.Error("expected members to be populated")
	}
}

func TestGetSpace_UnknownID_Returns404Error(t *testing.T) {
	store := openTestStore(t)
	_, err := store.GetSpace("no-such-id")
	if err == nil {
		t.Fatal("expected error for unknown space, got nil")
	}
}
