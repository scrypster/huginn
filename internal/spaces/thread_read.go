package spaces

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// LocalViewer is the single human viewer key until multi-user auth exists.
const LocalViewer = "local"

// HasThreadParticipation reports whether the human has posted the root or a
// reply in this thread. Spectators (never commented) are false — two agents
// talking in a thread the human never joined must not create unreads.
func (s *SQLiteSpaceStore) HasThreadParticipation(spaceID, parentID, viewer string) (bool, error) {
	if _, err := s.requireActiveSpace(spaceID); err != nil {
		return false, err
	}
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return false, &SpaceError{Code: "invalid_parent", Message: "parent_id is required"}
	}
	_ = viewer // single local human: any user-role row in the thread counts
	var n int
	err := s.db.Read().QueryRow(`
		SELECT COUNT(*) FROM messages m
		JOIN sessions sess ON sess.id = m.container_id
		WHERE sess.space_id = ?
		  AND m.container_type = 'session'
		  AND m.role = 'user'
		  AND (m.id = ? OR m.parent_id = ?)`,
		spaceID, parentID, parentID,
	).Scan(&n)
	if err != nil {
		if isNoSuchColumnError(err) {
			return false, nil
		}
		return false, fmt.Errorf("spaces: thread participation: %w", err)
	}
	return n > 0, nil
}

// MarkThreadRead records last-seen for a participant (or an explicit drawer
// open). Spectators who have never commented are a no-op — opening once
// from an @human ping does not start unread tracking.
func (s *SQLiteSpaceStore) MarkThreadRead(spaceID, parentID, viewer string) error {
	if _, err := s.requireActiveSpace(spaceID); err != nil {
		return err
	}
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return &SpaceError{Code: "invalid_parent", Message: "parent_id is required"}
	}
	if viewer == "" {
		viewer = LocalViewer
	}
	joined, err := s.HasThreadParticipation(spaceID, parentID, viewer)
	if err != nil {
		return err
	}
	if !joined {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.Write().Exec(`
		INSERT INTO space_thread_reads(space_id, parent_id, viewer, last_read_ts)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(space_id, parent_id, viewer) DO UPDATE SET last_read_ts = excluded.last_read_ts`,
		spaceID, parentID, viewer, now,
	); err != nil {
		return fmt.Errorf("spaces: mark thread read: %w", err)
	}
	return nil
}

// ThreadUnseenForViewer returns unseen Slack-style replies for a participant.
// Spectators always get 0 — chip stays "N replies" with no "new" badge.
func (s *SQLiteSpaceStore) ThreadUnseenForViewer(spaceID, parentID, viewer string) (int, error) {
	if _, err := s.requireActiveSpace(spaceID); err != nil {
		return 0, err
	}
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return 0, &SpaceError{Code: "invalid_parent", Message: "parent_id is required"}
	}
	if viewer == "" {
		viewer = LocalViewer
	}
	joined, err := s.HasThreadParticipation(spaceID, parentID, viewer)
	if err != nil {
		return 0, err
	}
	if !joined {
		return 0, nil
	}
	var lastRead sql.NullString
	err = s.db.Read().QueryRow(`
		SELECT last_read_ts FROM space_thread_reads
		WHERE space_id = ? AND parent_id = ? AND viewer = ?`,
		spaceID, parentID, viewer,
	).Scan(&lastRead)
	if err != nil && err != sql.ErrNoRows {
		if isNoSuchColumnError(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("spaces: thread last read: %w", err)
	}
	since := "1970-01-01T00:00:00Z"
	if lastRead.Valid && lastRead.String != "" {
		since = lastRead.String
	}
	var n int
	err = s.db.Read().QueryRow(`
		SELECT COUNT(*) FROM messages m
		JOIN sessions sess ON sess.id = m.container_id
		WHERE sess.space_id = ?
		  AND m.container_type = 'session'
		  AND m.role IN ('user', 'assistant')
		  AND m.parent_id = ?
		  AND m.ts > ?`,
		spaceID, parentID, since,
	).Scan(&n)
	if err != nil {
		if isNoSuchColumnError(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("spaces: thread unseen: %w", err)
	}
	return n, nil
}

// FirstUnseenReplyID returns the oldest reply newer than last-read for a
// participant, or empty when there is nothing unseen.
func (s *SQLiteSpaceStore) FirstUnseenReplyID(spaceID, parentID, viewer string) (string, error) {
	if viewer == "" {
		viewer = LocalViewer
	}
	joined, err := s.HasThreadParticipation(spaceID, parentID, viewer)
	if err != nil || !joined {
		return "", err
	}
	var lastRead sql.NullString
	err = s.db.Read().QueryRow(`
		SELECT last_read_ts FROM space_thread_reads
		WHERE space_id = ? AND parent_id = ? AND viewer = ?`,
		spaceID, parentID, viewer,
	).Scan(&lastRead)
	if err != nil && err != sql.ErrNoRows {
		if isNoSuchColumnError(err) {
			return "", nil
		}
		return "", err
	}
	since := "1970-01-01T00:00:00Z"
	if lastRead.Valid && lastRead.String != "" {
		since = lastRead.String
	}
	var id string
	err = s.db.Read().QueryRow(`
		SELECT m.id FROM messages m
		JOIN sessions sess ON sess.id = m.container_id
		WHERE sess.space_id = ?
		  AND m.container_type = 'session'
		  AND m.role IN ('user', 'assistant')
		  AND m.parent_id = ?
		  AND m.ts > ?
		ORDER BY m.ts ASC, m.seq ASC, m.id ASC
		LIMIT 1`,
		spaceID, parentID, since,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		if isNoSuchColumnError(err) {
			return "", nil
		}
		return "", fmt.Errorf("spaces: first unseen: %w", err)
	}
	return id, nil
}
