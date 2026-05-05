package spaces_test

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/spaces"
)

// TestMigrateSpacesV3_ChannelNameUnique verifies that the V3 migration
// creates a case-insensitive unique index on channel names, preventing
// two channels with names that differ only in case from coexisting.
func TestMigrateSpacesV3_ChannelNameUnique(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	// First create should succeed.
	_, err := store.CreateChannel("Alpha", "atlas", []string{}, "", "")
	if err != nil {
		t.Fatalf("first CreateChannel: %v", err)
	}

	// Second create with a different case should fail with a UNIQUE constraint error.
	_, err = store.CreateChannel("alpha", "atlas", []string{}, "", "")
	if err == nil {
		t.Fatal("expected error creating duplicate channel name (different case), got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unique constraint failed") {
		t.Errorf("expected UNIQUE constraint error, got: %v", err)
	}
}

// TestMigrateSpacesV3_ExactDuplicateChannelName verifies that exact-case
// duplicate channel names are also rejected.
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
	if !strings.Contains(strings.ToLower(err.Error()), "unique constraint failed") {
		t.Errorf("expected UNIQUE constraint error, got: %v", err)
	}
}

// TestMigrateSpacesV3_DMsNotConstrained verifies that the unique index does
// not apply to DM spaces (partial index only covers kind = 'channel').
func TestMigrateSpacesV3_DMsNotConstrained(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	// DMs are keyed by lead_agent, not name — multiple DMs can coexist.
	_, err := store.OpenDM("atlas")
	if err != nil {
		t.Fatalf("OpenDM atlas: %v", err)
	}
	_, err = store.OpenDM("coder")
	if err != nil {
		t.Fatalf("OpenDM coder: %v", err)
	}
}
