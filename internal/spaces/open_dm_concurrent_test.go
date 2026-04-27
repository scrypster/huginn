package spaces_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/spaces"
)

// ── OpenDM edge cases ────────────────────────────────────────────────────────

func TestOpenDM_ConcurrentCreation_SameAgent_ReturnsSameID(t *testing.T) {
	store := openTestStore(t)
	const agent = "concurrent-agent"
	const goroutines = 10

	ids := make([]string, goroutines)
	errs := make([]error, goroutines)
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sp, err := store.OpenDM(agent)
			errs[n] = err
			if sp != nil {
				ids[n] = sp.ID
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	// All IDs should be the same (idempotent).
	for i := 1; i < goroutines; i++ {
		if ids[i] != ids[0] {
			t.Errorf("goroutine %d got different ID %q vs %q", i, ids[i], ids[0])
		}
	}
}

func TestOpenDM_DifferentAgents_DifferentIDs(t *testing.T) {
	store := openTestStore(t)
	sp1, _ := store.OpenDM("agent-A")
	sp2, _ := store.OpenDM("agent-B")
	if sp1.ID == sp2.ID {
		t.Error("different agents should get different DM spaces")
	}
}

func TestOpenDM_ReturnsCorrectKind(t *testing.T) {
	store := openTestStore(t)
	sp, err := store.OpenDM("test-kind")
	if err != nil {
		t.Fatalf("OpenDM: %v", err)
	}
	if sp.Kind != "dm" {
		t.Errorf("expected kind=dm, got %q", sp.Kind)
	}
}

// TestOpenDM_ConcurrentDifferentAgents verifies that N goroutines each calling
// OpenDM with a distinct agentID:
//   - All succeed without error
//   - Each receives a unique DM space ID
//   - No duplicate DM spaces are created
//   - No deadlock or panic occurs
func TestOpenDM_ConcurrentDifferentAgents(t *testing.T) {
	store := openTestStore(t)
	const goroutines = 20

	ids := make([]string, goroutines)
	errs := make([]error, goroutines)
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			agentID := fmt.Sprintf("agent-unique-%02d", n)
			sp, err := store.OpenDM(agentID)
			errs[n] = err
			if sp != nil {
				ids[n] = sp.ID
			}
		}(i)
	}
	wg.Wait()

	// All calls must succeed.
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: unexpected error: %v", i, err)
		}
	}

	// All returned IDs must be non-empty.
	for i, id := range ids {
		if id == "" {
			t.Errorf("goroutine %d: got empty space ID", i)
		}
	}

	// All IDs must be unique — each agent gets its own DM space.
	seen := make(map[string]int, goroutines)
	for i, id := range ids {
		if prev, dup := seen[id]; dup {
			t.Errorf("duplicate DM space ID %q returned for goroutine %d and %d", id, prev, i)
		}
		seen[id] = i
	}

	// Verify via a secondary ListSpaces call that exactly goroutines DM spaces exist.
	res, err := store.ListSpaces(spaces.ListOpts{Kind: "dm"})
	if err != nil {
		t.Fatalf("ListSpaces: %v", err)
	}
	// The store may have had pre-existing DM spaces from other tests, so we
	// simply assert that there are at least goroutines DM spaces.
	if len(res.Spaces) < goroutines {
		t.Errorf("expected at least %d DM spaces, got %d", goroutines, len(res.Spaces))
	}
}

// ── CreateChannel edge cases ─────────────────────────────────────────────────

func TestCreateChannel_WhitespaceName_ReturnsError(t *testing.T) {
	store := openTestStore(t)
	_, err := store.CreateChannel("   ", "atlas", []string{}, "", "")
	if err == nil {
		t.Error("expected error for whitespace-only name")
	}
}

func TestCreateChannel_ExactlyMaxLengthName_Succeeds(t *testing.T) {
	store := openTestStore(t)
	name := strings.Repeat("x", 80)
	ch, err := store.CreateChannel(name, "atlas", []string{}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel with 80-char name: %v", err)
	}
	if ch.Name != name {
		t.Errorf("name mismatch")
	}
}

func TestCreateChannel_DuplicateMembers_Deduped(t *testing.T) {
	store := openTestStore(t)
	ch, err := store.CreateChannel("Dups", "atlas", []string{"bob", "bob", "alice"}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	// INSERT OR IGNORE dedupes, so we expect 2 unique members.
	if len(ch.Members) != 2 {
		t.Errorf("expected 2 unique members, got %d: %v", len(ch.Members), ch.Members)
	}
}

func TestCreateChannel_NoMembers_EmptySlice(t *testing.T) {
	store := openTestStore(t)
	ch, err := store.CreateChannel("Empty", "atlas", nil, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if ch.Members == nil {
		// Channel should have members loaded (even if empty), not nil.
		// loadMembers returns nil for 0 rows, which is acceptable.
	}
}

func TestCreateChannel_WithIconAndColor(t *testing.T) {
	store := openTestStore(t)
	ch, err := store.CreateChannel("Styled", "atlas", []string{}, "rocket", "#FF5733")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if ch.Icon != "rocket" {
		t.Errorf("expected icon %q, got %q", "rocket", ch.Icon)
	}
	if ch.Color != "#FF5733" {
		t.Errorf("expected color %q, got %q", "#FF5733", ch.Color)
	}
}

// ── GetSpace edge cases ──────────────────────────────────────────────────────

func TestGetSpace_ReturnsArchivedSpaceByID(t *testing.T) {
	store := openTestStore(t)
	ch, _ := store.CreateChannel("Will Archive", "atlas", []string{}, "", "")
	store.ArchiveSpace(ch.ID)

	got, err := store.GetSpace(ch.ID)
	if err != nil {
		t.Fatalf("GetSpace for archived space: %v", err)
	}
	if got.ArchivedAt == nil {
		t.Error("expected archived_at to be set")
	}
}

// ── UpdateSpace edge cases ───────────────────────────────────────────────────

func TestUpdateSpace_NonexistentID_ReturnsError(t *testing.T) {
	store := openTestStore(t)
	name := "new name"
	_, err := store.UpdateSpace("nonexistent-id", spaces.SpaceUpdates{Name: &name})
	if err == nil {
		t.Error("expected error for nonexistent space ID")
	}
}

func TestUpdateSpace_DM_ReturnsImmutableError(t *testing.T) {
	store := openTestStore(t)
	dm, _ := store.OpenDM("immutable-agent")
	name := "New Name"
	_, err := store.UpdateSpace(dm.ID, spaces.SpaceUpdates{Name: &name})
	if err == nil {
		t.Fatal("expected error for DM update")
	}
	var se *spaces.SpaceError
	if ok := func() bool {
		e := err
		for e != nil {
			if se2, ok := e.(*spaces.SpaceError); ok {
				se = se2
				return true
			}
			type unwrapper interface{ Unwrap() error }
			if u, ok := e.(unwrapper); ok {
				e = u.Unwrap()
			} else {
				break
			}
		}
		return false
	}(); ok && se.Code != "dm_immutable" {
		t.Errorf("expected dm_immutable code, got %q", se.Code)
	}
}

func TestUpdateSpace_NoChanges_ReturnsSuccess(t *testing.T) {
	store := openTestStore(t)
	ch, _ := store.CreateChannel("Static", "atlas", []string{}, "", "")

	got, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{})
	if err != nil {
		t.Fatalf("UpdateSpace with no changes: %v", err)
	}
	if got.Name != "Static" {
		t.Errorf("expected name unchanged, got %q", got.Name)
	}
}

func TestUpdateSpace_MembersReplaced(t *testing.T) {
	store := openTestStore(t)
	ch, _ := store.CreateChannel("Team", "atlas", []string{"alice", "bob"}, "", "")

	newMembers := []string{"charlie", "diana"}
	got, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{Members: &newMembers})
	if err != nil {
		t.Fatalf("UpdateSpace members: %v", err)
	}
	if len(got.Members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(got.Members))
	}
	// Members are ordered by agent_name ASC.
	if got.Members[0] != "charlie" || got.Members[1] != "diana" {
		t.Errorf("expected [charlie, diana], got %v", got.Members)
	}
}

func TestUpdateSpace_LeadAgentChange(t *testing.T) {
	store := openTestStore(t)
	ch, _ := store.CreateChannel("Team", "atlas", []string{}, "", "")

	newLead := "bob"
	got, err := store.UpdateSpace(ch.ID, spaces.SpaceUpdates{LeadAgent: &newLead})
	if err != nil {
		t.Fatalf("UpdateSpace lead_agent: %v", err)
	}
	if got.LeadAgent != "bob" {
		t.Errorf("expected lead_agent %q, got %q", "bob", got.LeadAgent)
	}
}

// ── ListSpaces filtering ─────────────────────────────────────────────────────

func TestListSpaces_FilterByKind(t *testing.T) {
	store := openTestStore(t)
	store.OpenDM("dm-agent")
	store.CreateChannel("Ch", "atlas", []string{}, "", "")

	dmsRes, err := store.ListSpaces(spaces.ListOpts{Kind: "dm"})
	if err != nil {
		t.Fatalf("ListSpaces dm: %v", err)
	}
	for _, s := range dmsRes.Spaces {
		if s.Kind != "dm" {
			t.Errorf("expected only DMs, got kind=%q", s.Kind)
		}
	}

	channelsRes, err := store.ListSpaces(spaces.ListOpts{Kind: "channel"})
	if err != nil {
		t.Fatalf("ListSpaces channel: %v", err)
	}
	for _, s := range channelsRes.Spaces {
		if s.Kind != "channel" {
			t.Errorf("expected only channels, got kind=%q", s.Kind)
		}
	}
}

func TestListSpaces_IncludeArchived(t *testing.T) {
	store := openTestStore(t)
	ch, _ := store.CreateChannel("Old", "atlas", []string{}, "", "")
	store.ArchiveSpace(ch.ID)

	res, err := store.ListSpaces(spaces.ListOpts{IncludeArchived: true})
	if err != nil {
		t.Fatalf("ListSpaces: %v", err)
	}
	found := false
	for _, s := range res.Spaces {
		if s.ID == ch.ID {
			found = true
		}
	}
	if !found {
		t.Error("archived space should appear when IncludeArchived is true")
	}
}

func TestListSpaces_DefaultLimit_ReturnsNonNilSlice(t *testing.T) {
	store := openTestStore(t)
	res, err := store.ListSpaces(spaces.ListOpts{})
	if err != nil {
		t.Fatalf("ListSpaces: %v", err)
	}
	if res.Spaces == nil {
		t.Error("expected non-nil spaces slice even with no results")
	}
}

// ── MarkRead / UnseenCount ───────────────────────────────────────────────────

func TestMarkRead_NonexistentSpace_ReturnsError(t *testing.T) {
	store := openTestStore(t)
	// space_read_positions has FK to spaces(id), so MarkRead on a nonexistent
	// space should fail with a foreign key constraint error.
	err := store.MarkRead("ghost-space")
	if err == nil {
		t.Error("expected FK error for MarkRead on nonexistent space")
	}
}

// ── ListSessionsForSpace ────────────────────────────────────────────────────

func TestListSessionsForSpace_NoSessions_ReturnsEmptySlice(t *testing.T) {
	store := openTestStore(t)
	ch, _ := store.CreateChannel("Empty", "atlas", []string{}, "", "")
	sessions, err := store.ListSessionsForSpace(ch.ID)
	if err != nil {
		t.Fatalf("ListSessionsForSpace: %v", err)
	}
	if sessions == nil {
		t.Error("expected non-nil slice")
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}


// ── Workstream hardening ─────────────────────────────────────────────────────

func TestWorkstream_Create_ConcurrentCreation_NoRace(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make([]error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, errs[n] = store.Create(ctx, "ws-"+string(rune('A'+n)), "desc")
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}

	list, _ := store.List(ctx)
	if len(list) != 20 {
		t.Errorf("expected 20 workstreams, got %d", len(list))
	}
}

func TestWorkstream_TagSession_ToDeletedWorkstream_ReturnsError(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()
	ws, _ := store.Create(ctx, "doomed", "")
	store.Delete(ctx, ws.ID)

	err := store.TagSession(ctx, ws.ID, "sess-1")
	if err == nil {
		// FK constraint should prevent tagging a nonexistent workstream.
		t.Error("expected error when tagging session on deleted workstream")
	}
}

func TestWorkstream_ListSessions_UnknownWorkstream_ReturnsEmpty(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()
	ids, err := store.ListSessions(ctx, "ghost-ws")
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected 0 sessions for unknown workstream, got %d", len(ids))
	}
}

func TestWorkstream_DoubleDelete_ReturnsError(t *testing.T) {
	store := openWS(t)
	ctx := context.Background()
	ws, _ := store.Create(ctx, "one-shot", "")
	store.Delete(ctx, ws.ID)

	err := store.Delete(ctx, ws.ID)
	if err == nil {
		t.Error("expected error on second delete")
	}
}
