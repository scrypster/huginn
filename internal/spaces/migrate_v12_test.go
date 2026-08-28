package spaces_test

import (
	"regexp"
	"testing"

	"github.com/scrypster/huginn/internal/spaces"
)

// TestMigrateSpacesV12_ReadPositionDefault_IsWholeSecond guards against
// reintroducing the same-second unread bug (see markread_precision_test.go)
// through the back door: space_read_positions.last_read_at's column
// DEFAULT used to emit RFC3339Nano-style fractional seconds
// (strftime('%Y-%m-%dT%H:%M:%fZ','now')), which sort lexicographically
// BEFORE a bare "Z" at the same wall-clock second and so compare backwards
// against sessions.updated_at (written as whole-second RFC3339) in
// UnseenCount's plain string comparison. Any row that lands via the bare
// DEFAULT — not through store.go's MarkRead, which always writes the
// column explicitly — must come out whole-second, matching
// sessions.updated_at's format exactly.
func TestMigrateSpacesV12_ReadPositionDefault_IsWholeSecond(t *testing.T) {
	db := openTestDB(t)
	store := spaces.NewSQLiteSpaceStore(db)

	ch, err := store.CreateChannel("Default Precision", "atlas", []string{}, "", "")
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	// Insert relying on the column DEFAULT rather than store.go's MarkRead,
	// which mirrors the class of caller migrateSpacesV1's original DEFAULT
	// would have quietly mis-served.
	if _, err := db.Write().Exec(
		`INSERT INTO space_read_positions(space_id) VALUES(?)`,
		ch.ID,
	); err != nil {
		t.Fatalf("insert relying on DEFAULT: %v", err)
	}

	var lastReadAt string
	if err := db.Read().QueryRow(
		`SELECT last_read_at FROM space_read_positions WHERE space_id = ?`,
		ch.ID,
	).Scan(&lastReadAt); err != nil {
		t.Fatalf("select last_read_at: %v", err)
	}

	// Whole-second RFC3339 UTC: no fractional-second component before the
	// trailing "Z".
	wholeSecondUTC := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)
	if !wholeSecondUTC.MatchString(lastReadAt) {
		t.Fatalf("space_read_positions.last_read_at DEFAULT = %q, want whole-second RFC3339 "+
			"(no fractional seconds) — the same-second unread bug is back for any row that "+
			"lands via the bare column DEFAULT", lastReadAt)
	}
}
