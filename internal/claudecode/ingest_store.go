package claudecode

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// IngestStore persists per-transcript resume state in the claude_ingest table.
type IngestStore struct {
	db *sqlitedb.DB
}

// NewIngestStore returns a store backed by db.
func NewIngestStore(db *sqlitedb.DB) *IngestStore { return &IngestStore{db: db} }

// Get returns the Huginn session id and resume state for a Claude Code
// session. found is false when the session has never been ingested.
func (s *IngestStore) Get(externalID string) (string, TailState, bool, error) {
	rdb := s.db.Read()
	if rdb == nil {
		return "", TailState{}, false, fmt.Errorf("claudecode: no read connection")
	}
	var (
		sid string
		st  TailState
	)
	err := rdb.QueryRow(`
		SELECT huginn_session_id, path, size, byte_offset, last_uuid
		FROM claude_ingest WHERE external_id = ?`, externalID,
	).Scan(&sid, &st.Path, &st.Size, &st.ByteOffset, &st.LastUUID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", TailState{}, false, nil
	}
	if err != nil {
		return "", TailState{}, false, fmt.Errorf("claudecode: get ingest state: %w", err)
	}
	return sid, st, true, nil
}

// Put upserts the resume state for a Claude Code session.
func (s *IngestStore) Put(externalID, huginnSessionID string, st TailState) error {
	wdb := s.db.Write()
	if wdb == nil {
		return fmt.Errorf("claudecode: no write connection")
	}
	_, err := wdb.Exec(`
		INSERT INTO claude_ingest
			(external_id, huginn_session_id, path, size, byte_offset, last_uuid, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(external_id) DO UPDATE SET
			huginn_session_id=excluded.huginn_session_id,
			path=excluded.path, size=excluded.size,
			byte_offset=excluded.byte_offset, last_uuid=excluded.last_uuid,
			updated_at=excluded.updated_at`,
		externalID, huginnSessionID, st.Path, st.Size, st.ByteOffset, st.LastUUID,
		time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("claudecode: put ingest state: %w", err)
	}
	return nil
}

// Delete removes the resume state for a Claude Code session.
func (s *IngestStore) Delete(externalID string) error {
	wdb := s.db.Write()
	if wdb == nil {
		return fmt.Errorf("claudecode: no write connection")
	}
	if _, err := wdb.Exec(`DELETE FROM claude_ingest WHERE external_id = ?`, externalID); err != nil {
		return fmt.Errorf("claudecode: delete ingest state: %w", err)
	}
	return nil
}
