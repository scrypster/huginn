package spaces

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// MaxSpaceMessageBytes is the maximum accepted size for a posted space message.
// Matches the session store cap so 20k-char replies succeed.
const MaxSpaceMessageBytes = 64 * 1024

// PostSpaceMessage persists a user message in spaceID. Empty parentID creates a
// channel/DM root. A non-empty parentID attaches a Slack-style reply.
// Replies to replies are flattened onto the root (one level). Does not invoke
// the orchestrator.
func (s *SQLiteSpaceStore) PostSpaceMessage(spaceID, content, parentID string) (*SpaceMessage, error) {
	content = strings.TrimSpace(content)
	parentID = strings.TrimSpace(parentID)
	if content == "" {
		return nil, &SpaceError{Code: "invalid_content", Message: "content is required"}
	}
	if len(content) > MaxSpaceMessageBytes {
		return nil, &SpaceError{Code: "invalid_content", Message: "content too long"}
	}
	sp, err := s.requireActiveSpace(spaceID)
	if err != nil {
		return nil, err
	}
	if cid := strings.TrimSpace(sp.CompanyID); cid != "" {
		if _, err := s.loadCompany(cid); err != nil {
			return nil, err
		}
	}

	id := newID()
	if parentID != "" && parentID == id {
		return nil, &SpaceError{Code: "invalid_parent", Message: "message cannot be its own parent"}
	}

	sessionID := ""
	resolvedParent := ""
	if parentID != "" {
		parent, err := s.loadSpaceMessage(spaceID, parentID)
		if err != nil {
			return nil, err
		}
		root, err := s.resolveReplyRoot(spaceID, parent)
		if err != nil {
			return nil, err
		}
		resolvedParent = root.ID
		sessionID = root.SessionID
		if resolvedParent == id {
			return nil, &SpaceError{Code: "invalid_parent", Message: "message cannot be its own parent"}
		}
	}
	if sessionID == "" {
		sid, err := s.ensureSpaceSession(spaceID)
		if err != nil {
			return nil, err
		}
		sessionID = sid
	}

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		msg, err := s.insertSpaceMessage(id, sessionID, content, resolvedParent)
		if err == nil {
			return msg, nil
		}
		if !isUniqueConstraintError(err) {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("spaces: post message: %w", lastErr)
}

// ListSpaceReplies returns Slack-style replies for parentID in spaceID, oldest first.
func (s *SQLiteSpaceStore) ListSpaceReplies(spaceID, parentID string) ([]SpaceMessage, error) {
	if _, err := s.requireActiveSpace(spaceID); err != nil {
		return nil, err
	}
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return nil, &SpaceError{Code: "invalid_parent", Message: "parent_id is required"}
	}
	if _, err := s.loadSpaceMessage(spaceID, parentID); err != nil {
		return nil, err
	}

	rows, err := s.db.Read().Query(`
		SELECT m.id, m.container_id, m.seq, m.ts, m.role, m.content,
		       COALESCE(m.agent, ''), COALESCE(m.parent_id, '')
		FROM messages m
		JOIN sessions s ON s.id = m.container_id
		WHERE s.space_id = ?
		  AND m.container_type = 'session'
		  AND m.role IN ('user', 'assistant')
		  AND m.parent_id = ?
		ORDER BY m.ts ASC, m.seq ASC, m.id ASC`,
		spaceID, parentID,
	)
	if err != nil {
		if isNoSuchColumnError(err) {
			return []SpaceMessage{}, nil
		}
		return nil, fmt.Errorf("spaces: list replies: %w", err)
	}
	defer rows.Close()

	out := []SpaceMessage{}
	for rows.Next() {
		var m SpaceMessage
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Seq, &m.Ts, &m.Role, &m.Content, &m.Agent, &m.ParentID); err != nil {
			return nil, fmt.Errorf("spaces: scan reply: %w", err)
		}
		m.EnsureCreatedAt()
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("spaces: reply rows: %w", err)
	}
	return out, nil
}

func (s *SQLiteSpaceStore) requireActiveSpace(id string) (*Space, error) {
	sp, err := s.GetSpace(id)
	if err != nil {
		return nil, err
	}
	if sp.ArchivedAt != nil {
		return nil, &SpaceError{Code: "space_not_found", Message: "space not found"}
	}
	return sp, nil
}

// GetSpaceMessage loads one space message by ID (the Slack-thread root).
func (s *SQLiteSpaceStore) GetSpaceMessage(spaceID, msgID string) (*SpaceMessage, error) {
	if _, err := s.requireActiveSpace(spaceID); err != nil {
		return nil, err
	}
	msgID = strings.TrimSpace(msgID)
	if msgID == "" {
		return nil, &SpaceError{Code: "parent_not_found", Message: "parent message not found"}
	}
	return s.loadSpaceMessage(spaceID, msgID)
}

func (s *SQLiteSpaceStore) loadSpaceMessage(spaceID, msgID string) (*SpaceMessage, error) {
	var m SpaceMessage
	var parentID sql.NullString
	err := s.db.Read().QueryRow(`
		SELECT m.id, m.container_id, m.seq, m.ts, m.role, m.content,
		       COALESCE(m.agent, ''), COALESCE(m.parent_id, '')
		FROM messages m
		JOIN sessions s ON s.id = m.container_id
		WHERE m.id = ? AND s.space_id = ? AND m.container_type = 'session'`,
		msgID, spaceID,
	).Scan(&m.ID, &m.SessionID, &m.Seq, &m.Ts, &m.Role, &m.Content, &m.Agent, &parentID)
	if err == sql.ErrNoRows {
		var exists int
		exErr := s.db.Read().QueryRow(`SELECT 1 FROM messages WHERE id = ?`, msgID).Scan(&exists)
		if exErr == nil {
			return nil, &SpaceError{Code: "parent_wrong_space", Message: "parent message is not in this space"}
		}
		return nil, &SpaceError{Code: "parent_not_found", Message: "parent message not found"}
	}
	if err != nil {
		if isNoSuchColumnError(err) {
			return nil, &SpaceError{Code: "parent_not_found", Message: "parent message not found"}
		}
		return nil, fmt.Errorf("spaces: load message: %w", err)
	}
	if parentID.Valid {
		m.ParentID = parentID.String
	}
	m.EnsureCreatedAt()
	return &m, nil
}

// resolveReplyRoot walks parent_id to the channel root (Slack one-level).
// Self-loops and cycles return invalid_parent without panicking.
func (s *SQLiteSpaceStore) resolveReplyRoot(spaceID string, msg *SpaceMessage) (*SpaceMessage, error) {
	if msg == nil {
		return nil, &SpaceError{Code: "parent_not_found", Message: "parent message not found"}
	}
	if msg.ParentID == "" {
		return msg, nil
	}
	seen := map[string]bool{msg.ID: true}
	cur := msg
	for cur.ParentID != "" {
		if cur.ParentID == cur.ID || seen[cur.ParentID] {
			return nil, &SpaceError{Code: "invalid_parent", Message: "message cannot be its own parent"}
		}
		seen[cur.ParentID] = true
		next, err := s.loadSpaceMessage(spaceID, cur.ParentID)
		if err != nil {
			// Broken chain: hang the reply off the last reachable message.
			return cur, nil
		}
		cur = next
	}
	return cur, nil
}

func (s *SQLiteSpaceStore) ensureSpaceSession(spaceID string) (string, error) {
	var id string
	err := s.db.Read().QueryRow(
		`SELECT id FROM sessions WHERE space_id = ? ORDER BY updated_at DESC LIMIT 1`,
		spaceID,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", fmt.Errorf("spaces: lookup session: %w", err)
	}
	id = newID()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.Write().Exec(
		`INSERT INTO sessions (id, title, status, version, created_at, updated_at, space_id)
		 VALUES (?, 'space-chat', 'active', 1, ?, ?, ?)`,
		id, now, now, spaceID,
	); err != nil {
		return "", fmt.Errorf("spaces: create session: %w", err)
	}
	return id, nil
}

func (s *SQLiteSpaceStore) insertSpaceMessage(id, sessionID, content, parentID string) (*SpaceMessage, error) {
	tx, err := s.db.Write().Begin()
	if err != nil {
		return nil, fmt.Errorf("spaces: begin post tx: %w", err)
	}
	defer tx.Rollback()

	var maxSeq int64
	if err := tx.QueryRow(
		`SELECT COALESCE(MAX(seq), 0) FROM messages WHERE container_id = ?`,
		sessionID,
	).Scan(&maxSeq); err != nil {
		return nil, fmt.Errorf("spaces: next seq: %w", err)
	}
	seq := maxSeq + 1
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.Exec(`
		INSERT INTO messages
			(id, container_type, container_id, seq, ts, role, content, agent, parent_id)
		VALUES (?, 'session', ?, ?, ?, 'user', ?, '', ?)`,
		id, sessionID, seq, now, content, parentID,
	); err != nil {
		return nil, fmt.Errorf("spaces: insert message: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("spaces: commit post: %w", err)
	}
	msg := &SpaceMessage{
		ID:        id,
		SessionID: sessionID,
		Seq:       seq,
		Ts:        now,
		CreatedAt: now,
		Role:      "user",
		Content:   content,
		ParentID:  parentID,
	}
	return msg, nil
}

// DeleteSpaceMessage removes one message in spaceID. A hallway root takes its
// Slack-style thread (parent_id = msgID) with it. Thread-read rows keyed on
// the deleted id are cleaned up. Unknown or cross-space IDs fail closed.
func (s *SQLiteSpaceStore) DeleteSpaceMessage(spaceID, msgID string) error {
	if _, err := s.requireActiveSpace(spaceID); err != nil {
		return err
	}
	msgID = strings.TrimSpace(msgID)
	if msgID == "" {
		return &SpaceError{Code: "parent_not_found", Message: "parent message not found"}
	}
	if _, err := s.loadSpaceMessage(spaceID, msgID); err != nil {
		return err
	}

	tx, err := s.db.Write().Begin()
	if err != nil {
		return fmt.Errorf("spaces: begin delete message tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		DELETE FROM messages
		WHERE container_type = 'session'
		  AND container_id IN (SELECT id FROM sessions WHERE space_id = ?)
		  AND (id = ? OR parent_id = ?)`,
		spaceID, msgID, msgID,
	); err != nil {
		return fmt.Errorf("spaces: delete message: %w", err)
	}
	if _, err := tx.Exec(
		`DELETE FROM space_thread_reads WHERE space_id = ? AND parent_id = ?`,
		spaceID, msgID,
	); err != nil {
		return fmt.Errorf("spaces: delete thread reads: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("spaces: commit delete message: %w", err)
	}
	return nil
}

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}
