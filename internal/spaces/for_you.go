package spaces

import (
	"fmt"
	"strings"
	"time"
)

// MarkForYou records a follow/@me (space_reply_mention) for the viewer so
// ListSpaces / GetSpace can return for_you on the wire. Spectator unseen
// is a separate count and is not written here.
func (s *SQLiteSpaceStore) MarkForYou(spaceID, viewer string) error {
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" {
		return &SpaceError{Code: "space_not_found", Message: "space not found"}
	}
	if _, err := s.GetSpace(spaceID); err != nil {
		return err
	}
	if viewer == "" {
		viewer = LocalViewer
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.Write().Exec(`
		INSERT INTO space_for_you(space_id, viewer, set_at)
		VALUES (?, ?, ?)
		ON CONFLICT(space_id, viewer) DO UPDATE SET set_at = excluded.set_at`,
		spaceID, viewer, now,
	); err != nil {
		return fmt.Errorf("spaces: mark for_you: %w", err)
	}
	return nil
}

// ClearForYou drops the follow/@me rail flag for the space. Missing rows
// and a missing table are success (idempotent / forward-compat).
func (s *SQLiteSpaceStore) ClearForYou(spaceID, viewer string) error {
	spaceID = strings.TrimSpace(spaceID)
	if spaceID == "" {
		return nil
	}
	if viewer == "" {
		viewer = LocalViewer
	}
	if _, err := s.db.Write().Exec(
		`DELETE FROM space_for_you WHERE space_id = ? AND viewer = ?`,
		spaceID, viewer,
	); err != nil {
		if isNoSuchColumnError(err) || strings.Contains(err.Error(), "no such table") {
			return nil
		}
		return fmt.Errorf("spaces: clear for_you: %w", err)
	}
	return nil
}

// HasForYou reports whether the viewer has an uncleared follow/@me on spaceID.
func (s *SQLiteSpaceStore) HasForYou(spaceID, viewer string) (bool, error) {
	if viewer == "" {
		viewer = LocalViewer
	}
	var n int
	err := s.db.Read().QueryRow(
		`SELECT COUNT(*) FROM space_for_you WHERE space_id = ? AND viewer = ?`,
		spaceID, viewer,
	).Scan(&n)
	if err != nil {
		if isNoSuchColumnError(err) || strings.Contains(err.Error(), "no such table") {
			return false, nil
		}
		return false, fmt.Errorf("spaces: has for_you: %w", err)
	}
	return n > 0, nil
}
