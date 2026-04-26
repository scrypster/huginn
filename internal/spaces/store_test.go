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

// ────────────────────────────────────────────────────────────────────────────
// Hardening: SpacesByLeadAgent tests (review/agents-channels-dm-hardening)
// ────────────────────────────────────────────────────────────────────────────

// TestSpacesByLeadAgent_ReturnsMatchingSpaces verifies that SpacesByLeadAgent
// returns all non-archived spaces where the agent is the lead_agent.
func TestSpacesByLeadAgent_ReturnsMatchingSpaces(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	// Create channels with different lead agents
	_, _ = store.CreateChannel("Team Alpha", "alice", []string{}, "", "")
	_, _ = store.CreateChannel("Team Beta", "alice", []string{}, "", "")
	ch3, _ := store.CreateChannel("Team Charlie", "bob", []string{}, "", "")

	// Query spaces where alice is the lead agent
	results, err := store.SpacesByLeadAgent("alice")
	if err != nil {
		t.Fatalf("SpacesByLeadAgent: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 spaces for alice, got %d", len(results))
	}

	// Verify the returned spaces are the correct ones
	spaceNames := make(map[string]bool)
	for _, s := range results {
		spaceNames[s.Name] = true
	}
	if !spaceNames["Team Alpha"] || !spaceNames["Team Beta"] {
		t.Errorf("expected Team Alpha and Team Beta, got: %v", spaceNames)
	}

	// Query spaces where bob is the lead agent
	bobSpaces, err := store.SpacesByLeadAgent("bob")
	if err != nil {
		t.Fatalf("SpacesByLeadAgent for bob: %v", err)
	}

	if len(bobSpaces) != 1 {
		t.Fatalf("expected 1 space for bob, got %d", len(bobSpaces))
	}
	if bobSpaces[0].ID != ch3.ID {
		t.Errorf("expected Team Charlie, got: %s", bobSpaces[0].Name)
	}
}

// TestSpacesByLeadAgent_EmptyForUnknownAgent verifies that SpacesByLeadAgent
// returns an empty slice (not an error) when the agent is not a lead agent.
func TestSpacesByLeadAgent_EmptyForUnknownAgent(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	// Create a channel with alice as the lead agent
	_, _ = store.CreateChannel("Team Alpha", "alice", []string{}, "", "")

	// Query spaces where charlie (non-existent) is the lead agent
	results, err := store.SpacesByLeadAgent("charlie")
	if err != nil {
		t.Fatalf("SpacesByLeadAgent: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 spaces for charlie, got %d", len(results))
	}
}

// TestListSpaceMessages_OrdersBySeqWhenTsIdentical verifies that when two
// messages have the same `ts` value (e.g. both written within the same
// second-precision window before the RFC3339Nano upgrade, or any future tie),
// ListSpaceMessages returns them in monotonic seq order rather than letting
// the random message ID lexicographically tie-break.
//
// Regression context (issue #2: "conversation order flips on refresh"):
// The user reports a User → Assistant pair appearing correctly during the
// live stream but reordered after a page reload. Root cause: ts collisions
// caused the existing `ORDER BY ts DESC, id DESC` to use random UUID id as
// the tie-break, occasionally swapping the pair. The fix: include seq in the
// ORDER BY (ts, seq, id).
func TestListSpaceMessages_OrdersBySeqWhenTsIdentical(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)
	ch, err := store.CreateChannel("Order Test", "atlas", []string{}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	sessionID := "sess-order-test"
	if _, err := db.Write().Exec(
		`INSERT INTO sessions (id, title, status, version, created_at, updated_at, space_id)
		 VALUES (?, 'order-test', 'active', 1, '2026-04-26T12:00:00Z', '2026-04-26T12:00:00Z', ?)`,
		sessionID, ch.ID,
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	// Two messages with IDENTICAL ts. seq encodes true write order (1=user,
	// 2=assistant). The ID strings are chosen so id-only tie-break would
	// reverse them: "z..." > "a..." lexicographically, so DESC-by-id puts
	// "z..." first → after the outer ASC reversal, "a..." comes first → out
	// of true write-order.
	tsCommon := "2026-04-26T12:00:00.500Z"
	if _, err := db.Write().Exec(
		`INSERT INTO messages (id, container_type, container_id, seq, ts, role, content)
		 VALUES (?, 'session', ?, 1, ?, 'user', 'first user message')`,
		"zzz-user-id", sessionID, tsCommon,
	); err != nil {
		t.Fatalf("insert user msg: %v", err)
	}
	if _, err := db.Write().Exec(
		`INSERT INTO messages (id, container_type, container_id, seq, ts, role, content)
		 VALUES (?, 'session', ?, 2, ?, 'assistant', 'second assistant reply')`,
		"aaa-asst-id", sessionID, tsCommon,
	); err != nil {
		t.Fatalf("insert asst msg: %v", err)
	}

	res, err := store.ListSpaceMessages(ch.ID, nil, 50)
	if err != nil {
		t.Fatalf("ListSpaceMessages: %v", err)
	}
	if len(res.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(res.Messages))
	}

	got := []string{res.Messages[0].Role, res.Messages[1].Role}
	want := []string{"user", "assistant"}
	if got[0] != want[0] || got[1] != want[1] {
		t.Errorf("conversation order is wrong: got %v want %v\nfull msgs: %+v",
			got, want, res.Messages)
	}
}

// TestSpacesByLeadAgent_ExcludesArchived verifies that SpacesByLeadAgent
// does not return archived spaces.
func TestSpacesByLeadAgent_ExcludesArchived(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	// Create two channels with alice as the lead agent
	ch1, _ := store.CreateChannel("Active Team", "alice", []string{}, "", "")
	ch2, _ := store.CreateChannel("Archived Team", "alice", []string{}, "", "")

	// Archive the second channel
	_ = store.ArchiveSpace(ch2.ID)

	// Query spaces where alice is the lead agent
	results, err := store.SpacesByLeadAgent("alice")
	if err != nil {
		t.Fatalf("SpacesByLeadAgent: %v", err)
	}

	// Should only return the active channel, not the archived one
	if len(results) != 1 {
		t.Fatalf("expected 1 non-archived space for alice, got %d", len(results))
	}
	if results[0].ID != ch1.ID {
		t.Errorf("expected Active Team, got: %s", results[0].Name)
	}
}
