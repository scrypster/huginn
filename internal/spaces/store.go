package spaces

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// SQLiteSpaceStore implements StoreInterface backed by a SQLite database.
type SQLiteSpaceStore struct {
	db *sqlitedb.DB
}

// NewSQLiteSpaceStore returns a new SQLiteSpaceStore using the provided DB.
// Call db.Migrate(spaces.Migrations()) before using the store.
func NewSQLiteSpaceStore(db *sqlitedb.DB) *SQLiteSpaceStore {
	return &SQLiteSpaceStore{db: db}
}

// newID generates a random 32-character hex ID (128 bits of entropy).
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// isNoSuchColumnError returns true if err is a SQLite "no such column" error.
func isNoSuchColumnError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "no such column")
}

// OpenDM returns the existing DM space for agentName, or creates one.
// Idempotent: calling it N times with the same agentName always returns the same Space.
func (s *SQLiteSpaceStore) OpenDM(agentName string) (*Space, error) {
	if agentName == "" {
		return nil, &SpaceError{Code: "invalid_agent", Message: "agent name is required"}
	}
	var id string
	err := s.db.Read().QueryRow(
		`SELECT id FROM spaces WHERE kind = 'dm' AND lead_agent = ? AND archived_at IS NULL`,
		agentName,
	).Scan(&id)

	if err == nil {
		// DM already exists — return it.
		return s.loadSpace(id)
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("spaces: open DM lookup: %w", err)
	}

	// Does not exist — insert idempotently.
	spaceID := newID()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.Write().Exec(
		`INSERT INTO spaces(id, name, kind, lead_agent, icon, color, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?)
		 ON CONFLICT(lead_agent) WHERE kind='dm' DO NOTHING`,
		spaceID, agentName, KindDM, agentName, "", "", now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("spaces: create DM: %w", err)
	}

	// Re-fetch to return the winner (handles concurrent inserts).
	return s.OpenDM(agentName)
}

// CreateChannel creates a new channel space with the given name, lead agent, and members.
// The entire operation (space row + member rows) is wrapped in a transaction so a
// partial failure never leaves an orphaned channel with missing members.
func (s *SQLiteSpaceStore) CreateChannel(name, leadAgent string, members []string, icon, color string) (*Space, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("spaces: channel name cannot be empty or whitespace-only")
	}
	if len(name) > 80 {
		return nil, fmt.Errorf("spaces: channel name exceeds 80-character limit")
	}
	id := newID()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	tx, err := s.db.Write().Begin()
	if err != nil {
		return nil, fmt.Errorf("spaces: begin tx for create channel: %w", err)
	}
	defer tx.Rollback() // noop if committed

	if _, err := tx.Exec(
		`INSERT INTO spaces(id, name, kind, lead_agent, icon, color, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?)`,
		id, name, KindChannel, leadAgent, icon, color, now, now,
	); err != nil {
		return nil, fmt.Errorf("spaces: create channel: %w", err)
	}
	for _, m := range members {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO space_members(space_id, agent_name) VALUES (?,?)`, id, m,
		); err != nil {
			return nil, fmt.Errorf("spaces: add member %q: %w", m, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("spaces: commit create channel: %w", err)
	}
	return s.loadSpace(id)
}

// GetSpace fetches a single space by ID.
func (s *SQLiteSpaceStore) GetSpace(id string) (*Space, error) {
	return s.loadSpace(id)
}

// DefaultListSpacesLimit is the number of spaces returned when Limit is not specified.
const DefaultListSpacesLimit = 200

// MaxListSpacesLimit is the maximum number of spaces that can be requested in a single call.
const MaxListSpacesLimit = 1000

// ListSpaces returns a page of spaces matching opts, ordered by (updated_at DESC, id DESC).
// When opts.Cursor is set the results begin after the position encoded in the cursor.
// The returned ListSpacesResult.NextCursor is non-empty when a subsequent page exists.
func (s *SQLiteSpaceStore) ListSpaces(opts ListOpts) (ListSpacesResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultListSpacesLimit
	}
	if limit > MaxListSpacesLimit {
		limit = MaxListSpacesLimit
	}

	query := `SELECT id, updated_at FROM spaces WHERE 1=1`
	args := []any{}

	if !opts.IncludeArchived {
		query += ` AND archived_at IS NULL`
	}
	if opts.Kind != "" {
		query += ` AND kind = ?`
		args = append(args, opts.Kind)
	}

	// Apply keyset pagination when a cursor is provided.
	// The condition advances past (updated_at, id) in descending order:
	//   updated_at < cursor_ts  OR  (updated_at = cursor_ts AND id < cursor_id)
	if opts.Cursor != "" {
		cursorUpdatedAt, cursorID, err := DecodeCursor(opts.Cursor)
		if err != nil {
			return ListSpacesResult{}, fmt.Errorf("spaces: list: invalid cursor: %w", err)
		}
		cursorTS := cursorUpdatedAt.UTC().Format(time.RFC3339Nano)
		query += ` AND (updated_at < ? OR (updated_at = ? AND id < ?))`
		args = append(args, cursorTS, cursorTS, cursorID)
	}

	// Fetch limit+1 rows to detect whether a next page exists without a COUNT query.
	query += ` ORDER BY updated_at DESC, id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.Read().Query(query, args...)
	if err != nil {
		return ListSpacesResult{}, fmt.Errorf("spaces: list: %w", err)
	}
	defer rows.Close()

	type rawRow struct {
		id        string
		updatedAt string
	}
	var rawRows []rawRow
	for rows.Next() {
		var r rawRow
		if err := rows.Scan(&r.id, &r.updatedAt); err != nil {
			return ListSpacesResult{}, err
		}
		rawRows = append(rawRows, r)
	}
	if err := rows.Err(); err != nil {
		return ListSpacesResult{}, fmt.Errorf("spaces: list rows: %w", err)
	}

	// Trim the sentinel row used to detect the next page.
	hasMore := len(rawRows) > limit
	if hasMore {
		rawRows = rawRows[:limit]
	}

	result := make([]*Space, 0, len(rawRows))
	for _, r := range rawRows {
		sp, err := s.loadSpace(r.id)
		if err != nil {
			return ListSpacesResult{}, err
		}
		result = append(result, sp)
	}

	// Build next-page cursor from the last item's (updated_at, id).
	var nextCursor string
	if hasMore && len(rawRows) > 0 {
		last := rawRows[len(rawRows)-1]
		t, _ := time.Parse(time.RFC3339Nano, last.updatedAt)
		nextCursor = EncodeCursor(t, last.id)
	}

	return ListSpacesResult{Spaces: result, NextCursor: nextCursor}, nil
}

// UpdateSpace applies the given updates to a space.
// Returns ErrImmutableDM if called on a DM space.
// All writes run inside a single transaction so a multi-field update cannot
// produce a partially-committed state if a later statement fails.
func (s *SQLiteSpaceStore) UpdateSpace(id string, updates SpaceUpdates) (*Space, error) {
	sp, err := s.loadSpace(id)
	if err != nil {
		return nil, err
	}
	if sp.Kind == KindDM {
		return nil, ErrImmutableDM
	}

	// Validate all inputs before opening a transaction.
	if updates.Name != nil {
		if strings.TrimSpace(*updates.Name) == "" {
			return nil, &SpaceError{Code: "invalid_name", Message: "name cannot be empty"}
		}
		if len(*updates.Name) > 80 {
			return nil, &SpaceError{Code: "invalid_name", Message: "name must be 80 characters or fewer"}
		}
	}

	// Nothing to do.
	if updates.Name == nil && updates.Icon == nil && updates.Color == nil && updates.Members == nil && updates.LeadAgent == nil {
		return s.loadSpace(id)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	tx, err := s.db.Write().Begin()
	if err != nil {
		return nil, fmt.Errorf("spaces: begin update tx: %w", err)
	}
	defer tx.Rollback() // noop if committed

	if updates.Name != nil {
		if _, err := tx.Exec(`UPDATE spaces SET name=?, updated_at=? WHERE id=?`, *updates.Name, now, id); err != nil {
			return nil, fmt.Errorf("spaces: update name: %w", err)
		}
	}
	if updates.Icon != nil {
		if _, err := tx.Exec(`UPDATE spaces SET icon=?, updated_at=? WHERE id=?`, *updates.Icon, now, id); err != nil {
			return nil, fmt.Errorf("spaces: update icon: %w", err)
		}
	}
	if updates.Color != nil {
		if _, err := tx.Exec(`UPDATE spaces SET color=?, updated_at=? WHERE id=?`, *updates.Color, now, id); err != nil {
			return nil, fmt.Errorf("spaces: update color: %w", err)
		}
	}
	if updates.LeadAgent != nil {
		if _, err := tx.Exec(`UPDATE spaces SET lead_agent=?, updated_at=? WHERE id=?`, *updates.LeadAgent, now, id); err != nil {
			return nil, fmt.Errorf("spaces: update lead_agent: %w", err)
		}
	}
	if updates.Members != nil {
		if _, err := tx.Exec(`DELETE FROM space_members WHERE space_id=?`, id); err != nil {
			return nil, fmt.Errorf("spaces: clear members: %w", err)
		}
		for _, m := range *updates.Members {
			if _, err := tx.Exec(
				`INSERT OR IGNORE INTO space_members(space_id, agent_name) VALUES (?,?)`, id, m,
			); err != nil {
				return nil, fmt.Errorf("spaces: add member %q: %w", m, err)
			}
		}
		// Bump updated_at for member-only changes (name/icon/color/lead already bumped above).
		if updates.Name == nil && updates.Icon == nil && updates.Color == nil && updates.LeadAgent == nil {
			if _, err := tx.Exec(`UPDATE spaces SET updated_at=? WHERE id=?`, now, id); err != nil {
				return nil, fmt.Errorf("spaces: bump updated_at for member change: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("spaces: commit update: %w", err)
	}
	return s.loadSpace(id)
}

// ArchiveSpace soft-deletes a channel by setting archived_at.
// Returns ErrImmutableDM if called on a DM space.
func (s *SQLiteSpaceStore) ArchiveSpace(id string) error {
	sp, err := s.loadSpace(id)
	if err != nil {
		return err
	}
	if sp.Kind == KindDM {
		return ErrImmutableDM
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = s.db.Write().Exec(`UPDATE spaces SET archived_at=? WHERE id=?`, now, id); err != nil {
		return fmt.Errorf("spaces: archive: %w", err)
	}
	return nil
}

// MarkRead records that the user has read all messages in the space up to now.
func (s *SQLiteSpaceStore) MarkRead(spaceID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.Write().Exec(
		`INSERT INTO space_read_positions(space_id, last_read_at) VALUES(?,?)
		 ON CONFLICT(space_id) DO UPDATE SET last_read_at=excluded.last_read_at`,
		spaceID, now,
	); err != nil {
		return fmt.Errorf("spaces: mark read: %w", err)
	}
	return nil
}

// UnseenCount returns the number of sessions in the space updated after the
// user's last read position. Returns 0 without error if the space_id column
// is not present on sessions (forward-compat guard).
func (s *SQLiteSpaceStore) UnseenCount(spaceID string) (int, error) {
	var count int
	err := s.db.Read().QueryRow(
		`SELECT COUNT(*) FROM sessions
		 WHERE space_id = ?
		   AND updated_at > COALESCE(
		       (SELECT last_read_at FROM space_read_positions WHERE space_id = ?),
		       '1970-01-01T00:00:00Z'
		   )`,
		spaceID, spaceID,
	).Scan(&count)
	if err != nil {
		if isNoSuchColumnError(err) {
			return 0, nil // space_id column not yet in schema — graceful degradation
		}
		return 0, fmt.Errorf("spaces: unseen count: %w", err)
	}
	return count, nil
}

// loadSpace fetches a space by ID including its members and unseen count.
func (s *SQLiteSpaceStore) loadSpace(id string) (*Space, error) {
	var sp Space
	var createdAt, updatedAt string
	var archivedAt sql.NullString

	err := s.db.Read().QueryRow(
		`SELECT id, name, kind, lead_agent, icon, color, created_at, updated_at, archived_at
		 FROM spaces WHERE id=?`, id,
	).Scan(&sp.ID, &sp.Name, &sp.Kind, &sp.LeadAgent, &sp.Icon, &sp.Color,
		&createdAt, &updatedAt, &archivedAt)

	if err == sql.ErrNoRows {
		return nil, &SpaceError{Code: "space_not_found", Message: "space not found"}
	}
	if err != nil {
		return nil, fmt.Errorf("spaces: load space %q: %w", id, err)
	}

	sp.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	sp.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	if archivedAt.Valid {
		t, _ := time.Parse(time.RFC3339Nano, archivedAt.String)
		sp.ArchivedAt = &t
	}

	if sp.Kind == KindChannel {
		members, err := s.loadMembers(id)
		if err != nil {
			return nil, err
		}
		sp.Members = members
	}

	unseenCount, _ := s.UnseenCount(id) // non-fatal
	sp.UnseenCount = unseenCount
	return &sp, nil
}

// ListSessionsForSpace returns a lightweight list of sessions belonging to the
// given space, ordered by updated_at DESC, limited to 100 rows.
func (s *SQLiteSpaceStore) ListSessionsForSpace(spaceID string) ([]SessionRef, error) {
	rows, err := s.db.Read().Query(
		`SELECT id, title, status, created_at, updated_at, space_id
		 FROM sessions
		 WHERE space_id = ?
		 ORDER BY updated_at DESC
		 LIMIT 100`,
		spaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("spaces: list sessions: %w", err)
	}
	defer rows.Close()
	var result []SessionRef
	for rows.Next() {
		var r SessionRef
		var spID *string
		if err := rows.Scan(&r.ID, &r.Title, &r.Status, &r.CreatedAt, &r.UpdatedAt, &spID); err != nil {
			return nil, fmt.Errorf("spaces: scan session: %w", err)
		}
		if spID != nil {
			r.SpaceID = *spID
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("spaces: sessions rows: %w", err)
	}
	if result == nil {
		result = []SessionRef{}
	}
	return result, nil
}

// ListSpaceMessages returns up to limit messages across all sessions in the space,
// ordered chronologically (oldest first). When before is non-nil, only messages
// older than (before.Ts, before.ID) are returned, enabling infinite scroll upward.
//
// Query design:
//   - idx_sessions_space resolves space_id → session IDs (existing index).
//   - idx_messages_container_ts resolves each session's messages by ts DESC (new index).
//   - The subquery reverses the result to return messages in chronological order.
//
// Cursor stability: ts is assigned by SQLite (strftime('%Y-%m-%dT%H:%M:%fZ','now')).
// SQLite is single-writer; ts values are monotonically non-decreasing and
// lexicographic string comparison is safe for pagination.
func (s *SQLiteSpaceStore) ListSpaceMessages(spaceID string, before *SpaceMsgCursor, limit int) (SpaceMessagesResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	args := []any{spaceID}
	cursorClause := ""
	if before != nil {
		cursorClause = " AND (m.ts < ? OR (m.ts = ? AND m.id < ?))"
		args = append(args, before.Ts, before.Ts, before.ID)
	}
	// Fetch limit+1 rows to detect whether older messages exist (hasMore).
	args = append(args, limit+1)

	// The inner query returns the limit+1 NEWEST messages (DESC) matching the
	// cursor condition. The outer query reverses them into chronological order.
	query := fmt.Sprintf(`
		SELECT id, session_id, seq, ts, role, content, agent, tool_calls_json FROM (
			SELECT m.id, m.container_id AS session_id, m.seq, m.ts,
			       m.role, m.content, COALESCE(m.agent, '') AS agent,
			       m.tool_calls_json
			FROM messages m
			JOIN sessions s ON s.id = m.container_id
			WHERE s.space_id = ?
			  AND m.container_type = 'session'
			  AND m.role IN ('user', 'assistant')
			  AND (m.parent_message_id IS NULL OR m.parent_message_id = '')%s
			ORDER BY m.ts DESC, m.id DESC
			LIMIT ?
		) sub
		ORDER BY ts ASC, id ASC
	`, cursorClause)

	rows, err := s.db.Read().Query(query, args...)
	if err != nil {
		// tool_calls_json or parent_message_id may not exist on older databases — retry without them.
		if isNoSuchColumnError(err) {
			query = fmt.Sprintf(`
				SELECT id, session_id, seq, ts, role, content, agent, NULL FROM (
					SELECT m.id, m.container_id AS session_id, m.seq, m.ts,
					       m.role, m.content, COALESCE(m.agent, '') AS agent
					FROM messages m
					JOIN sessions s ON s.id = m.container_id
					WHERE s.space_id = ?
					  AND m.container_type = 'session'
					  AND m.role IN ('user', 'assistant')%s
					ORDER BY m.ts DESC, m.id DESC
					LIMIT ?
				) sub
				ORDER BY ts ASC, id ASC
			`, cursorClause)
			rows, err = s.db.Read().Query(query, args...)
		}
		if err != nil {
			return SpaceMessagesResult{}, fmt.Errorf("spaces: list messages: %w", err)
		}
	}
	defer rows.Close()

	var msgs []SpaceMessage
	for rows.Next() {
		var m SpaceMessage
		var toolCallsJSON sql.NullString
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Seq, &m.Ts, &m.Role, &m.Content, &m.Agent, &toolCallsJSON); err != nil {
			return SpaceMessagesResult{}, fmt.Errorf("spaces: scan message: %w", err)
		}
		if toolCallsJSON.Valid && toolCallsJSON.String != "" {
			_ = json.Unmarshal([]byte(toolCallsJSON.String), &m.ToolCalls)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return SpaceMessagesResult{}, fmt.Errorf("spaces: messages rows: %w", err)
	}

	// The inner DESC query fetched limit+1 rows then the outer ASC sort reversed
	// them. The extra (sentinel) row is the chronologically OLDEST one — msgs[0].
	// Trimming msgs[0] keeps the limit newest messages and sets the cursor to the
	// oldest remaining message (new msgs[0]) for the next scroll-up request.
	hasMore := len(msgs) > limit
	if hasMore {
		msgs = msgs[1:] // drop the sentinel (oldest) row
	}

	var nextCursor string
	if hasMore && len(msgs) > 0 {
		// Cursor encodes the oldest message in the result. The next request
		// with before=cursor will load messages older than msgs[0].
		nextCursor = EncodeSpaceMsgCursor(msgs[0].Ts, msgs[0].ID)
	}

	if msgs == nil {
		msgs = []SpaceMessage{}
	}
	return SpaceMessagesResult{Messages: msgs, NextCursor: nextCursor}, nil
}

// BuildChannelContext returns a system prompt addendum for the lead agent
// listing member agents and their purpose, enabling intelligent delegation.
// Returns empty string for non-channel spaces or spaces with no members.
func BuildChannelContext(leadAgent string, members []string, agentDescriptions map[string]string) string {
	if len(members) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n[Team Context]\nYou are the lead agent in a multi-agent channel. ")
	sb.WriteString("You can delegate tasks to the following team members:\n")
	for _, m := range members {
		desc := agentDescriptions[m]
		if desc == "" {
			desc = "specialist agent"
		}
		fmt.Fprintf(&sb, "- %s: %s\n", m, desc)
	}
	sb.WriteString("\nDelegate specialized subtasks to appropriate team members and synthesize their results.")
	return sb.String()
}

// RemoveAgentFromAllSpaces removes an agent from all space membership lists,
// archives any spaces where the agent is the lead, and returns a
// SpaceCascadeResult describing the side effects so callers can emit WS events.
//
// The entire operation runs inside a single transaction so a partial failure
// never leaves spaces in an inconsistent state.
func (s *SQLiteSpaceStore) RemoveAgentFromAllSpaces(agentName string) (*SpaceCascadeResult, error) {
	if agentName == "" {
		return nil, &SpaceError{Code: "invalid_agent", Message: "agent name is required"}
	}
	result := &SpaceCascadeResult{}

	tx, err := s.db.Write().Begin()
	if err != nil {
		return nil, fmt.Errorf("spaces: begin remove-agent tx: %w", err)
	}
	defer tx.Rollback() // noop if committed

	// Collect channel IDs where the agent is a member but NOT the lead —
	// these channels survive but their roster changes (UpdatedSpaceIDs).
	rows, err := tx.Query(
		`SELECT DISTINCT sm.space_id FROM space_members sm
		 JOIN spaces sp ON sp.id = sm.space_id
		 WHERE sm.agent_name = ?
		   AND sp.lead_agent != ?
		   AND sp.archived_at IS NULL`,
		agentName, agentName,
	)
	if err != nil {
		return nil, fmt.Errorf("spaces: query member channels for agent: %w", err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("spaces: scan member channel id: %w", err)
		}
		result.UpdatedSpaceIDs = append(result.UpdatedSpaceIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("spaces: member channel rows: %w", err)
	}

	// Collect all spaces (DMs and channels) where the agent is the lead —
	// these will be archived (ArchivedSpaceIDs).
	rows2, err := tx.Query(
		`SELECT id FROM spaces WHERE lead_agent = ? AND archived_at IS NULL`,
		agentName,
	)
	if err != nil {
		return nil, fmt.Errorf("spaces: query lead spaces for agent: %w", err)
	}
	for rows2.Next() {
		var id string
		if err := rows2.Scan(&id); err != nil {
			rows2.Close()
			return nil, fmt.Errorf("spaces: scan lead space id: %w", err)
		}
		result.ArchivedSpaceIDs = append(result.ArchivedSpaceIDs, id)
	}
	rows2.Close()
	if err := rows2.Err(); err != nil {
		return nil, fmt.Errorf("spaces: lead space rows: %w", err)
	}

	// Remove the agent from all space_members rows.
	if _, err := tx.Exec(
		`DELETE FROM space_members WHERE agent_name = ?`, agentName,
	); err != nil {
		return nil, fmt.Errorf("spaces: delete space_members for agent: %w", err)
	}

	// Archive all spaces where the agent was the lead.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.Exec(
		`UPDATE spaces SET archived_at=? WHERE lead_agent=? AND archived_at IS NULL`,
		now, agentName,
	); err != nil {
		return nil, fmt.Errorf("spaces: archive lead spaces for agent: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("spaces: commit remove-agent tx: %w", err)
	}
	return result, nil
}

// SpaceMembers implements threadmgr.SpaceMembershipChecker.
// Returns (nil, nil) when the space is not found or archived (deny-all by default).
// The lead agent is always included in the returned list, even for DM spaces where
// the junction-table Members slice is empty.
func (s *SQLiteSpaceStore) SpaceMembers(spaceID string) ([]string, error) {
	sp, err := s.GetSpace(spaceID)
	if err != nil {
		var se *SpaceError
		if errors.As(err, &se) && se.Code == "space_not_found" {
			return nil, nil
		}
		return nil, err
	}
	// Archived spaces are treated as not-found: deny-all.
	if sp.ArchivedAt != nil {
		return nil, nil
	}
	// Always include the lead agent.  For DM spaces the junction-table Members
	// slice is nil, so without this the lead agent would always be denied.
	out := make([]string, 0, len(sp.Members)+1)
	out = append(out, sp.LeadAgent)
	for _, m := range sp.Members {
		if !strings.EqualFold(m, sp.LeadAgent) {
			out = append(out, m)
		}
	}
	return out, nil
}

// loadMembers returns the agent names for a channel space.
func (s *SQLiteSpaceStore) loadMembers(spaceID string) ([]string, error) {
	rows, err := s.db.Read().Query(
		`SELECT agent_name FROM space_members WHERE space_id=? ORDER BY agent_name`, spaceID,
	)
	if err != nil {
		return nil, fmt.Errorf("spaces: load members: %w", err)
	}
	defer rows.Close()

	var members []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, fmt.Errorf("spaces: scan member: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("spaces: members rows: %w", err)
	}
	return members, nil
}

// GetChannelsForAgent returns all non-archived channel spaces where agentName
// is either the lead_agent or a member. Used for DM cross-space awareness so a
// lead agent in a DM can know about the channels and teams they participate in.
func (s *SQLiteSpaceStore) GetChannelsForAgent(agentName string) ([]*Space, error) {
	rows, err := s.db.Read().Query(
		`SELECT DISTINCT s.id FROM spaces s
		 LEFT JOIN space_members sm ON sm.space_id = s.id
		 WHERE s.kind = 'channel'
		   AND s.archived_at IS NULL
		   AND (s.lead_agent = ? OR sm.agent_name = ?)
		 ORDER BY s.name`,
		agentName, agentName,
	)
	if err != nil {
		return nil, fmt.Errorf("spaces: channels for agent: %w", err)
	}
	defer rows.Close()

	var result []*Space
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		sp, loadErr := s.loadSpace(id)
		if loadErr != nil {
			continue // skip broken spaces
		}
		result = append(result, sp)
	}
	return result, rows.Err()
}
