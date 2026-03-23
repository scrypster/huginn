package spaces_test

import (
	"testing"

	"github.com/scrypster/huginn/internal/spaces"
)

func newTestStore(t *testing.T) *spaces.SQLiteSpaceStore {
	t.Helper()
	db := openTestDB(t)
	return spaces.NewSQLiteSpaceStore(db)
}

// TestSpaceMembers_UnknownSpace_ReturnsNilNil verifies the deny-all default.
func TestSpaceMembers_UnknownSpace_ReturnsNilNil(t *testing.T) {
	s := newTestStore(t)
	members, err := s.SpaceMembers("does-not-exist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if members != nil {
		t.Fatalf("expected nil members for unknown space, got %v", members)
	}
}

// TestSpaceMembers_DMSpace_LeadAgentAllowed verifies that the lead agent of a DM
// space is included even though DM spaces have no junction-table members.
func TestSpaceMembers_DMSpace_LeadAgentAllowed(t *testing.T) {
	s := newTestStore(t)
	sp, err := s.OpenDM("claude")
	if err != nil {
		t.Fatalf("OpenDM: %v", err)
	}

	members, err := s.SpaceMembers(sp.ID)
	if err != nil {
		t.Fatalf("SpaceMembers: %v", err)
	}
	found := false
	for _, m := range members {
		if m == "claude" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected lead agent %q in members, got %v", "claude", members)
	}
}

// TestSpaceMembers_ArchivedSpace_DeniesAll verifies that an archived channel
// returns (nil, nil) so callers deny all agents.
func TestSpaceMembers_ArchivedSpace_DeniesAll(t *testing.T) {
	s := newTestStore(t)
	sp, err := s.CreateChannel("archived-ch", "lead", []string{"alice"}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if err := s.ArchiveSpace(sp.ID); err != nil {
		t.Fatalf("ArchiveSpace: %v", err)
	}

	members, err := s.SpaceMembers(sp.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if members != nil {
		t.Errorf("expected nil members for archived space, got %v", members)
	}
}

// TestSpaceMembers_ChannelSpace_IncludesMembersAndLead verifies that both the
// lead agent and explicit channel members are returned.
func TestSpaceMembers_ChannelSpace_IncludesMembersAndLead(t *testing.T) {
	s := newTestStore(t)
	sp, err := s.CreateChannel("eng", "lead", []string{"alice", "bob"}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	members, err := s.SpaceMembers(sp.ID)
	if err != nil {
		t.Fatalf("SpaceMembers: %v", err)
	}

	want := map[string]bool{"lead": true, "alice": true, "bob": true}
	for _, m := range members {
		delete(want, m)
	}
	if len(want) != 0 {
		t.Errorf("missing members: %v (got %v)", want, members)
	}
}

// TestSpaceMembers_LeadNotDuplicated verifies that the lead agent is not listed
// twice when it also appears in the junction-table members.
func TestSpaceMembers_LeadNotDuplicated(t *testing.T) {
	s := newTestStore(t)
	// CreateChannel may or may not add the lead to space_members — either way
	// we should not see duplicates.
	sp, err := s.CreateChannel("eng", "lead", []string{"lead", "alice"}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	members, err := s.SpaceMembers(sp.ID)
	if err != nil {
		t.Fatalf("SpaceMembers: %v", err)
	}

	seen := make(map[string]int)
	for _, m := range members {
		seen[m]++
	}
	if seen["lead"] > 1 {
		t.Errorf("lead agent listed %d times, want at most 1", seen["lead"])
	}
}
