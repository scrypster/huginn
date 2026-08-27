package spaces_test

import (
	"errors"
	"testing"

	"github.com/scrypster/huginn/internal/spaces"
)

// TestMigrateSpacesV3_ChannelNameUnique verifies desk channel names stay
// case-insensitive unique (v8: per-company; desk is the empty-company slice).
func TestMigrateSpacesV3_ChannelNameUnique(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	_, err := store.CreateChannel("Alpha", "atlas", []string{}, "", "")
	if err != nil {
		t.Fatalf("first CreateChannel: %v", err)
	}

	_, err = store.CreateChannel("alpha", "atlas", []string{}, "", "")
	if err == nil {
		t.Fatal("expected error creating duplicate channel name (different case), got nil")
	}
	if !errors.Is(err, spaces.ErrChannelNameTaken) {
		t.Errorf("expected ErrChannelNameTaken, got: %v", err)
	}
}

// TestMigrateSpacesV3_ExactDuplicateChannelName verifies that exact-case
// duplicate desk channel names are also rejected.
func TestMigrateSpacesV3_ExactDuplicateChannelName(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	_, err := store.CreateChannel("Engineering", "atlas", []string{}, "", "")
	if err != nil {
		t.Fatalf("first CreateChannel: %v", err)
	}

	_, err = store.CreateChannel("Engineering", "atlas", []string{}, "", "")
	if err == nil {
		t.Fatal("expected error creating exact duplicate channel name, got nil")
	}
	if !errors.Is(err, spaces.ErrChannelNameTaken) {
		t.Errorf("expected ErrChannelNameTaken, got: %v", err)
	}
}

// TestMigrateSpacesV3_DMsNotConstrained verifies that the unique index does
// not apply to DM spaces (partial index only covers kind = 'channel').
func TestMigrateSpacesV3_DMsNotConstrained(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	_, err := store.OpenDM("atlas")
	if err != nil {
		t.Fatalf("OpenDM atlas: %v", err)
	}
	_, err = store.OpenDM("coder")
	if err != nil {
		t.Fatalf("OpenDM coder: %v", err)
	}
}
