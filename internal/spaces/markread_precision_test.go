package spaces_test

import (
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/spaces"
)

// TestMarkRead_SameSecondAsSessionUpdate_ClearsUnread is the regression test
// for the unread badge that would not clear.
//
// UnseenCount compares sessions.updated_at against space_read_positions.
// last_read_at with SQL's LEXICOGRAPHIC string comparison, not a date
// comparison. sessions.updated_at is written with time.RFC3339 (second
// precision, e.g. "2026-04-26T12:00:00Z"). MarkRead used to write
// time.RFC3339Nano, which emits fractional seconds ("2026-04-26T12:00:00.5Z").
//
// Within the same wall-clock second those two strings compare BACKWARDS:
// '.' (0x2E) sorts before 'Z' (0x5A), so "…12:00:00.5Z" < "…12:00:00Z".
// The just-written read position therefore looked OLDER than the message it
// was meant to acknowledge, and UnseenCount kept reporting the session as
// unread. Writing both sides at the same precision is what makes the
// comparison correct.
//
// This test pins the same-second case specifically: reverting MarkRead to
// RFC3339Nano makes it fail.
func TestMarkRead_SameSecondAsSessionUpdate_ClearsUnread(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	ch, err := store.CreateChannel("Unread Precision", "atlas", []string{}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	// A session updated at an exact second boundary, written the way the
	// session store writes it (RFC3339, no fractional part).
	updatedAt := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	if _, err := db.Write().Exec(
		`INSERT INTO sessions (id, title, status, version, created_at, updated_at, space_id)
		 VALUES (?, 'unread-precision', 'active', 1, ?, ?, ?)`,
		"sess-unread-precision", updatedAt, updatedAt, ch.ID,
	); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	before, err := store.UnseenCount(ch.ID)
	if err != nil {
		t.Fatalf("UnseenCount before MarkRead: %v", err)
	}
	if before != 1 {
		t.Fatalf("UnseenCount before MarkRead = %d, want 1 (test setup is wrong)", before)
	}

	if err := store.MarkRead(ch.ID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	after, err := store.UnseenCount(ch.ID)
	if err != nil {
		t.Fatalf("UnseenCount after MarkRead: %v", err)
	}
	if after != 0 {
		t.Errorf("UnseenCount after MarkRead = %d, want 0 — the read position did not "+
			"sort after a same-second session update (MarkRead precision must match "+
			"sessions.updated_at's RFC3339)", after)
	}
}
