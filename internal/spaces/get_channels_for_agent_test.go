package spaces_test

import (
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// openChannelsTestDB creates a fresh SQLite-backed database for GetChannelsForAgent tests.
func openChannelsTestDB(t *testing.T) *sqlitedb.DB {
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

// TestGetChannelsForAgent_ReturnsLeadChannels verifies that GetChannelsForAgent
// returns channels where the agent is the lead_agent.
func TestGetChannelsForAgent_ReturnsLeadChannels(t *testing.T) {
	db := openChannelsTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	// Create a channel where "atlas" is the lead agent
	ch, err := store.CreateChannel("Team Alpha", "atlas", []string{}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	channels, err := store.GetChannelsForAgent("atlas")
	if err != nil {
		t.Fatalf("GetChannelsForAgent: %v", err)
	}

	if len(channels) != 1 {
		t.Errorf("expected 1 channel, got %d", len(channels))
	}
	if channels[0].ID != ch.ID {
		t.Errorf("expected channel ID %q, got %q", ch.ID, channels[0].ID)
	}
}

// TestGetChannelsForAgent_ReturnsMemberChannels verifies that GetChannelsForAgent
// returns channels where the agent is a member (not lead).
func TestGetChannelsForAgent_ReturnsMemberChannels(t *testing.T) {
	db := openChannelsTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	// Create a channel where "atlas" is a member but not the lead
	ch, err := store.CreateChannel("Team Beta", "alice", []string{"atlas"}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	channels, err := store.GetChannelsForAgent("atlas")
	if err != nil {
		t.Fatalf("GetChannelsForAgent: %v", err)
	}

	if len(channels) != 1 {
		t.Errorf("expected 1 channel, got %d", len(channels))
	}
	if channels[0].ID != ch.ID {
		t.Errorf("expected channel ID %q, got %q", ch.ID, channels[0].ID)
	}
}

// TestGetChannelsForAgent_ExcludesArchivedChannels verifies that GetChannelsForAgent
// does NOT return archived channels.
func TestGetChannelsForAgent_ExcludesArchivedChannels(t *testing.T) {
	db := openChannelsTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	// Create and archive a channel where "atlas" is the lead
	ch, err := store.CreateChannel("Archived Team", "atlas", []string{}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	if err := store.ArchiveSpace(ch.ID); err != nil {
		t.Fatalf("ArchiveSpace: %v", err)
	}

	channels, err := store.GetChannelsForAgent("atlas")
	if err != nil {
		t.Fatalf("GetChannelsForAgent: %v", err)
	}

	if len(channels) != 0 {
		t.Errorf("expected 0 channels, got %d (archived channel should not appear)", len(channels))
	}
}

// TestGetChannelsForAgent_ExcludesDMs verifies that GetChannelsForAgent
// does NOT return DMs, only channels.
func TestGetChannelsForAgent_ExcludesDMs(t *testing.T) {
	db := openChannelsTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	// Create a DM for "atlas"
	dm, err := store.OpenDM("atlas")
	if err != nil {
		t.Fatalf("OpenDM: %v", err)
	}

	channels, err := store.GetChannelsForAgent("atlas")
	if err != nil {
		t.Fatalf("GetChannelsForAgent: %v", err)
	}

	if len(channels) != 0 {
		t.Errorf("expected 0 channels, got %d (DM should not be returned)", len(channels))
	}

	// Verify the DM actually exists to ensure the test is valid
	if dm == nil || dm.ID == "" {
		t.Error("DM was not created properly")
	}
}

// TestGetChannelsForAgent_NoChannels_EmptyResult verifies that GetChannelsForAgent
// returns an empty slice (not an error) when the agent is not in any channels.
func TestGetChannelsForAgent_NoChannels_EmptyResult(t *testing.T) {
	db := openChannelsTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	channels, err := store.GetChannelsForAgent("unused-agent")
	if err != nil {
		t.Fatalf("GetChannelsForAgent: %v", err)
	}

	if len(channels) != 0 {
		t.Errorf("expected 0 channels, got %d", len(channels))
	}
}

// TestGetChannelsForAgent_MultipleChannels verifies that GetChannelsForAgent
// returns all channels the agent participates in (as lead or member).
func TestGetChannelsForAgent_MultipleChannels(t *testing.T) {
	db := openChannelsTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	// Create a channel where "atlas" is the lead
	ch1, err := store.CreateChannel("Team Alpha", "atlas", []string{}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel 1: %v", err)
	}

	// Create a channel where "atlas" is a member
	ch2, err := store.CreateChannel("Team Beta", "alice", []string{"atlas"}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel 2: %v", err)
	}

	// Create a channel where "atlas" is not involved
	_, err = store.CreateChannel("Team Gamma", "bob", []string{}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel 3: %v", err)
	}

	channels, err := store.GetChannelsForAgent("atlas")
	if err != nil {
		t.Fatalf("GetChannelsForAgent: %v", err)
	}

	if len(channels) != 2 {
		t.Errorf("expected 2 channels, got %d", len(channels))
	}

	// Verify the returned channels are the expected ones
	ids := make(map[string]bool)
	for _, ch := range channels {
		ids[ch.ID] = true
	}

	if !ids[ch1.ID] {
		t.Errorf("expected channel 1 (ID %q) in results", ch1.ID)
	}
	if !ids[ch2.ID] {
		t.Errorf("expected channel 2 (ID %q) in results", ch2.ID)
	}
}
