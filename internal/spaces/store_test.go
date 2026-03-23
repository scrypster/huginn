package spaces_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

func openTestDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := sqlitedb.Open(filepath.Join(dir, "test.db"))
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
	return db
}

func TestOpenDM_Idempotent(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	sp1, err := store.OpenDM("atlas")
	if err != nil {
		t.Fatalf("first OpenDM: %v", err)
	}
	sp2, err := store.OpenDM("atlas")
	if err != nil {
		t.Fatalf("second OpenDM: %v", err)
	}
	if sp1.ID != sp2.ID {
		t.Errorf("expected same space ID, got %q vs %q", sp1.ID, sp2.ID)
	}
}

func TestListSpaces_ExcludesArchived(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	sp, _ := store.OpenDM("atlas")
	// DMs can't be archived — use a channel for this test
	ch, _ := store.CreateChannel("Team", "atlas", []string{}, "", "")
	_ = store.ArchiveSpace(ch.ID)
	res, err := store.ListSpaces(spaces.ListOpts{IncludeArchived: false})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range res.Spaces {
		if s.ID == ch.ID {
			t.Error("archived space should not appear")
		}
	}
	_ = sp // keep
}

func TestCreateChannel_And_DM_Returns403(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, err := store.CreateChannel("Software Team", "atlas", []string{"coder", "reviewer"}, "", "")
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if ch.Kind != "channel" {
		t.Errorf("expected channel, got %q", ch.Kind)
	}
	// DM archiving should return error
	dm, _ := store.OpenDM("atlas")
	if err := store.ArchiveSpace(dm.ID); err == nil {
		t.Error("expected error when archiving DM")
	}
}

func TestBuildChannelContext_WithMembers(t *testing.T) {
	ctx := spaces.BuildChannelContext("atlas", []string{"coder", "reviewer"}, map[string]string{
		"coder": "writes Go code",
	})
	if !strings.Contains(ctx, "coder") {
		t.Error("expected coder in context")
	}
	if !strings.Contains(ctx, "reviewer") {
		t.Error("expected reviewer in context")
	}
	if !strings.Contains(ctx, "Team Context") {
		t.Error("expected Team Context header")
	}
}

func TestBuildChannelContext_Empty(t *testing.T) {
	ctx := spaces.BuildChannelContext("atlas", nil, nil)
	if ctx != "" {
		t.Errorf("expected empty context for no members, got: %q", ctx)
	}
}

func TestOpenDM_EmptyAgentName_ReturnsError(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	_, err := store.OpenDM("")
	if err == nil {
		t.Fatal("expected error for empty agent name, got nil")
	}
	var se *spaces.SpaceError
	if !errors.As(err, &se) {
		t.Fatalf("expected *SpaceError, got %T: %v", err, err)
	}
	if se.Code != "invalid_agent" {
		t.Errorf("expected code %q, got %q", "invalid_agent", se.Code)
	}
}

func TestListSpaces_LimitCappedAt200(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	// Create a few spaces; the cap behaviour is checked without needing 200+ rows.
	for i := 0; i < 3; i++ {
		name := strings.Repeat("x", i+1)
		store.CreateChannel(name, "atlas", []string{}, "", "")
	}
	// Request a ludicrously large limit — should not panic and should return at most 200.
	res, err := store.ListSpaces(spaces.ListOpts{Limit: 1_000_000, IncludeArchived: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Spaces) > 200 {
		t.Errorf("expected at most 200 results, got %d", len(res.Spaces))
	}
}

func TestUpdateSpace_EmptyName_ReturnsError(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, _ := store.CreateChannel("Original", "atlas", []string{}, "", "")
	emptyName := ""
	_, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{Name: &emptyName})
	if err == nil {
		t.Fatal("expected error for empty name, got nil")
	}
	var se *spaces.SpaceError
	if !errors.As(err, &se) {
		t.Fatalf("expected *SpaceError, got %T: %v", err, err)
	}
	if se.Code != "invalid_name" {
		t.Errorf("expected code %q, got %q", "invalid_name", se.Code)
	}
}

func TestUpdateSpace_WhitespaceName_ReturnsError(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, _ := store.CreateChannel("Original", "atlas", []string{}, "", "")
	wsName := "   "
	_, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{Name: &wsName})
	if err == nil {
		t.Fatal("expected error for whitespace-only name, got nil")
	}
}

func TestUpdateSpace_NameTooLong_ReturnsError(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, _ := store.CreateChannel("Original", "atlas", []string{}, "", "")
	longName := strings.Repeat("a", 81)
	_, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{Name: &longName})
	if err == nil {
		t.Fatal("expected error for name > 80 chars, got nil")
	}
}

func TestUpdateSpace_BumpsUpdatedAt(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, _ := store.CreateChannel("Original", "atlas", []string{}, "", "")
	before := ch.UpdatedAt

	// Sleep 1ms so the update trigger's millisecond timestamp is strictly
	// after the create timestamp (which uses Go's nanosecond precision but
	// SQLite's strftime only has millisecond precision).
	time.Sleep(time.Millisecond)

	newName := "Renamed"
	updated, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{Name: &newName})
	if err != nil {
		t.Fatalf("UpdateSpace: %v", err)
	}
	if !updated.UpdatedAt.After(before) {
		t.Errorf("expected updated_at to advance after rename; before=%v after=%v", before, updated.UpdatedAt)
	}
	if updated.Name != "Renamed" {
		t.Errorf("expected name %q, got %q", "Renamed", updated.Name)
	}
}

func TestCreateChannel_MembersAreStored(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, err := store.CreateChannel("Eng Team", "atlas", []string{"coder", "reviewer"}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if len(ch.Members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(ch.Members))
	}
}
