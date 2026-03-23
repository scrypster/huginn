// Package artifact provides a SQLite-backed store for agent-produced artifacts.
package artifact

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/scrypster/huginn/internal/workforce"
)

const (
	// sizeThreshold is the inline/file split boundary (256 KB).
	sizeThreshold = 256 * 1024

	// MaxArtifactSize is the hard upper bound for any artifact (10 MB).
	MaxArtifactSize = 10 * 1024 * 1024

	// defaultListLimit is used when callers pass limit ≤ 0.
	defaultListLimit = 50

	// maxListLimit caps runaway pagination requests.
	maxListLimit = 200
)

// ErrArtifactTooLarge is returned by Write when Content exceeds MaxArtifactSize.
var ErrArtifactTooLarge = errors.New("artifact content exceeds maximum size")

// Store defines the artifact persistence interface.
type Store interface {
	Write(ctx context.Context, a *workforce.Artifact) error
	Read(ctx context.Context, id string) (*workforce.Artifact, error)
	// ListBySession returns artifacts for sessionID. limit ≤ 0 defaults to 50
	// (max 200). afterID, if non-empty, returns results after that ULID cursor.
	ListBySession(ctx context.Context, sessionID string, limit int, afterID string) ([]*workforce.Artifact, error)
	// ListByAgent returns artifacts produced by agentName with created_at >=
	// since. limit/afterID work the same as ListBySession.
	ListByAgent(ctx context.Context, agentName string, since time.Time, limit int, afterID string) ([]*workforce.Artifact, error)
	UpdateStatus(ctx context.Context, id string, status workforce.ArtifactStatus, reason string) error
	Supersede(ctx context.Context, oldID, newID string) error
	// DeleteBySession removes all artifacts (and associated files) for a session.
	// Wire into session cleanup to prevent unbounded disk growth.
	DeleteBySession(ctx context.Context, sessionID string) error
	Archive(ctx context.Context, olderThan time.Duration) (int, error)
}

// SQLiteStore is a SQLite-backed implementation of Store.
type SQLiteStore struct {
	db           *sql.DB
	artifactsDir string

	entropyMu sync.Mutex
	entropy   *ulid.MonotonicEntropy
}

// NewStore creates a new SQLiteStore using the given write-connection and
// artifacts directory for large-content offloading.
func NewStore(db *sql.DB, artifactsDir string) *SQLiteStore {
	return &SQLiteStore{
		db:           db,
		artifactsDir: artifactsDir,
		entropy:      ulid.Monotonic(rand.Reader, 0),
	}
}

// NewMemoryStore creates an in-memory SQLiteStore backed by a temporary SQLite
// database. Intended for tests — callers must call Close() to release resources.
func NewMemoryStore(t interface {
	TempDir() string
	Helper()
	Fatalf(format string, args ...any)
}) (*SQLiteStore, func()) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("NewMemoryStore: open: %v", err)
	}
	if _, err := db.Exec(createArtifactsTableSQL); err != nil {
		t.Fatalf("NewMemoryStore: create table: %v", err)
	}
	store := NewStore(db, t.TempDir())
	return store, func() { db.Close() }
}

// createArtifactsTableSQL is the minimal schema for the in-memory test store.
const createArtifactsTableSQL = `
CREATE TABLE IF NOT EXISTS artifacts (
    id                      TEXT    NOT NULL PRIMARY KEY,
    kind                    TEXT    NOT NULL,
    title                   TEXT    NOT NULL,
    mime_type               TEXT,
    content                 BLOB,
    content_ref             TEXT,
    metadata_json           TEXT,
    agent_name              TEXT    NOT NULL,
    thread_id               TEXT,
    session_id              TEXT    NOT NULL,
    triggering_message_id   TEXT,
    status                  TEXT    NOT NULL DEFAULT 'draft',
    rejection_reason        TEXT,
    created_at              TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at              TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
)`

func (s *SQLiteStore) newID() string {
	s.entropyMu.Lock()
	defer s.entropyMu.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now()), s.entropy).String()
}

// Write persists a new artifact. If a.ID is empty a new ULID is assigned.
// Content larger than 256 KB is written to disk; content_ref is set and
// the inline content column is left NULL.
// Returns ErrArtifactTooLarge if len(a.Content) > MaxArtifactSize.
func (s *SQLiteStore) Write(ctx context.Context, a *workforce.Artifact) error {
	if len(a.Content) > MaxArtifactSize {
		return ErrArtifactTooLarge
	}

	if a.ID == "" {
		a.ID = s.newID()
	}
	now := time.Now().UTC()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	if a.UpdatedAt.IsZero() {
		a.UpdatedAt = now
	}
	if a.Status == "" {
		a.Status = workforce.StatusDraft
	}

	var contentVal []byte
	var contentRef string

	if len(a.Content) > sizeThreshold {
		// Write to file, store relative ref.
		dir := filepath.Join(s.artifactsDir, a.SessionID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("artifact.Write: mkdir %s: %w", dir, err)
		}
		fpath := filepath.Join(dir, a.ID)
		if err := os.WriteFile(fpath, a.Content, 0o644); err != nil {
			return fmt.Errorf("artifact.Write: write file %s: %w", fpath, err)
		}
		contentRef = a.SessionID + "/" + a.ID
		contentVal = nil
	} else {
		contentVal = a.Content
	}

	var metaJSON []byte
	if a.Metadata != nil {
		var err error
		metaJSON, err = json.Marshal(a.Metadata)
		if err != nil {
			return fmt.Errorf("artifact.Write: marshal metadata: %w", err)
		}
	}

	createdStr := a.CreatedAt.UTC().Format(time.RFC3339Nano)
	updatedStr := a.UpdatedAt.UTC().Format(time.RFC3339Nano)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO artifacts (
			id, kind, title, mime_type, content, content_ref, metadata_json,
			agent_name, thread_id, session_id, triggering_message_id,
			status, rejection_reason, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID,
		string(a.Kind),
		a.Title,
		nullableString(a.MimeType),
		nullableBytes(contentVal),
		nullableString(contentRef),
		nullableBytes(metaJSON),
		a.AgentName,
		nullableString(a.ThreadID),
		a.SessionID,
		nullableString(a.TriggeringMessageID),
		string(a.Status),
		nullableString(a.RejectionReason),
		createdStr,
		updatedStr,
	)
	if err != nil {
		return fmt.Errorf("artifact.Write: insert: %w", err)
	}

	// Reflect back what was stored.
	a.ContentRef = contentRef
	if contentRef != "" {
		a.Content = nil // not stored inline
	}

	return nil
}

// Read fetches a single artifact by ID. If content_ref is set the content is
// loaded from disk. Returns workforce.ErrArtifactNotFound when not present.
func (s *SQLiteStore) Read(ctx context.Context, id string) (*workforce.Artifact, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, kind, title, mime_type, content, content_ref, metadata_json,
		       agent_name, thread_id, session_id, triggering_message_id,
		       status, rejection_reason, created_at, updated_at
		FROM artifacts
		WHERE id = ?`, id)

	a, err := scanArtifact(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, workforce.ErrArtifactNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("artifact.Read: %w", err)
	}

	if a.ContentRef != "" {
		data, err := os.ReadFile(filepath.Join(s.artifactsDir, a.ContentRef))
		if err != nil {
			return nil, fmt.Errorf("artifact.Read: load content_ref %s: %w", a.ContentRef, err)
		}
		a.Content = data
	}

	return a, nil
}

// ReadMetaOnly fetches artifact metadata by ID without loading file-backed
// content. a.Content is always nil — callers use OpenContent for streaming.
// Returns workforce.ErrArtifactNotFound when the artifact does not exist or
// has been soft-deleted.
func (s *SQLiteStore) ReadMetaOnly(ctx context.Context, id string) (*workforce.Artifact, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, kind, title, mime_type, content_ref, metadata_json,
		       agent_name, thread_id, session_id, triggering_message_id,
		       status, rejection_reason, created_at, updated_at
		FROM artifacts
		WHERE id = ? AND status != 'deleted'`, id)

	var (
		artID, kind, title string
		mimeType           sql.NullString
		contentRef         sql.NullString
		metaJSON           []byte
		agentName          string
		threadID           sql.NullString
		sessionID          string
		trigMsgID          sql.NullString
		status             string
		rejReason          sql.NullString
		createdStr         string
		updatedStr         string
	)
	err := row.Scan(
		&artID, &kind, &title, &mimeType, &contentRef, &metaJSON,
		&agentName, &threadID, &sessionID, &trigMsgID,
		&status, &rejReason, &createdStr, &updatedStr,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, workforce.ErrArtifactNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("artifact.ReadMetaOnly: %w", err)
	}

	a := &workforce.Artifact{
		ID:                  artID,
		Kind:                workforce.ArtifactKind(kind),
		Title:               title,
		MimeType:            mimeType.String,
		ContentRef:          contentRef.String,
		AgentName:           agentName,
		ThreadID:            threadID.String,
		SessionID:           sessionID,
		TriggeringMessageID: trigMsgID.String,
		Status:              workforce.ArtifactStatus(status),
		RejectionReason:     rejReason.String,
		// Content is intentionally nil — no file I/O.
	}
	if len(metaJSON) > 0 {
		if err := json.Unmarshal(metaJSON, &a.Metadata); err != nil {
			return nil, fmt.Errorf("artifact.ReadMetaOnly: unmarshal metadata: %w", err)
		}
	}
	if t, err := time.Parse(time.RFC3339Nano, createdStr); err == nil {
		a.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339Nano, updatedStr); err == nil {
		a.UpdatedAt = t
	}
	return a, nil
}

// OpenContent returns a streaming reader for the artifact's file-backed content.
// Returns workforce.ErrArtifactNotFound if the artifact has no file-backed
// content (content is stored inline — callers should use Read() instead).
// The caller must close the returned ReadCloser.
//
// Path traversal is prevented by ensuring the resolved path is prefixed by
// the artifacts base directory.
func (s *SQLiteStore) OpenContent(ctx context.Context, id string) (io.ReadCloser, error) {
	var contentRef sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT content_ref FROM artifacts WHERE id = ?`, id).Scan(&contentRef)
	if errors.Is(err, sql.ErrNoRows) || !contentRef.Valid || contentRef.String == "" {
		return nil, workforce.ErrArtifactNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("artifact.OpenContent: query: %w", err)
	}

	// Path traversal defense: resolved path must be prefixed by artifactsDir.
	base := filepath.Clean(s.artifactsDir) + string(filepath.Separator)
	resolved := filepath.Join(base, contentRef.String)
	if !strings.HasPrefix(resolved, base) {
		return nil, fmt.Errorf("artifact.OpenContent: path traversal blocked for id %s", id)
	}

	return os.Open(resolved)
}

// ListBySession returns artifacts belonging to the given session, ordered by
// created_at ASC. limit ≤ 0 defaults to 50, capped at 200. afterID is an
// exclusive cursor (ULID string); use "" to start from the beginning.
func (s *SQLiteStore) ListBySession(ctx context.Context, sessionID string, limit int, afterID string) ([]*workforce.Artifact, error) {
	limit = clampLimit(limit)
	var rows *sql.Rows
	var err error
	if afterID == "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, kind, title, mime_type, content, content_ref, metadata_json,
			       agent_name, thread_id, session_id, triggering_message_id,
			       status, rejection_reason, created_at, updated_at
			FROM artifacts
			WHERE session_id = ?
			ORDER BY created_at ASC
			LIMIT ?`, sessionID, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, kind, title, mime_type, content, content_ref, metadata_json,
			       agent_name, thread_id, session_id, triggering_message_id,
			       status, rejection_reason, created_at, updated_at
			FROM artifacts
			WHERE session_id = ?
			  AND id > ?
			ORDER BY created_at ASC
			LIMIT ?`, sessionID, afterID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("artifact.ListBySession: query: %w", err)
	}
	defer rows.Close()
	return scanArtifacts(rows)
}

// ListByAgent returns artifacts produced by agentName with created_at >= since,
// ordered by created_at ASC. limit/afterID work the same as ListBySession.
func (s *SQLiteStore) ListByAgent(ctx context.Context, agentName string, since time.Time, limit int, afterID string) ([]*workforce.Artifact, error) {
	limit = clampLimit(limit)
	sinceStr := since.UTC().Format(time.RFC3339Nano)
	var rows *sql.Rows
	var err error
	if afterID == "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, kind, title, mime_type, content, content_ref, metadata_json,
			       agent_name, thread_id, session_id, triggering_message_id,
			       status, rejection_reason, created_at, updated_at
			FROM artifacts
			WHERE agent_name = ?
			  AND created_at >= ?
			ORDER BY created_at ASC
			LIMIT ?`, agentName, sinceStr, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, kind, title, mime_type, content, content_ref, metadata_json,
			       agent_name, thread_id, session_id, triggering_message_id,
			       status, rejection_reason, created_at, updated_at
			FROM artifacts
			WHERE agent_name = ?
			  AND created_at >= ?
			  AND id > ?
			ORDER BY created_at ASC
			LIMIT ?`, agentName, sinceStr, afterID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("artifact.ListByAgent: query: %w", err)
	}
	defer rows.Close()
	return scanArtifacts(rows)
}

// UpdateStatus updates the status (and optionally rejection_reason) of an
// artifact. Only valid transitions are allowed. Idempotent: if the requested
// status equals the current status, returns nil without a DB write.
//
//	draft     → accepted | rejected | superseded | failed
//	accepted  → superseded
func (s *SQLiteStore) UpdateStatus(ctx context.Context, id string, status workforce.ArtifactStatus, reason string) error {
	// Fetch current status.
	var current string
	err := s.db.QueryRowContext(ctx, `SELECT status FROM artifacts WHERE id = ?`, id).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return workforce.ErrArtifactNotFound
	}
	if err != nil {
		return fmt.Errorf("artifact.UpdateStatus: fetch current: %w", err)
	}

	if err := validateTransition(workforce.ArtifactStatus(current), status); err != nil {
		return err
	}
	// Idempotent: same state, no-op.
	if workforce.ArtifactStatus(current) == status {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `
		UPDATE artifacts
		SET status = ?, rejection_reason = ?, updated_at = ?
		WHERE id = ?`,
		string(status),
		nullableString(reason),
		now,
		id,
	)
	if err != nil {
		return fmt.Errorf("artifact.UpdateStatus: update: %w", err)
	}
	return nil
}

// Supersede marks oldID as superseded. The caller is responsible for setting
// any status on newID.
func (s *SQLiteStore) Supersede(ctx context.Context, oldID, newID string) error {
	return s.UpdateStatus(ctx, oldID, workforce.StatusSuperseded, "")
}

// DeleteBySession removes all artifact rows for a session and their associated
// large-content files on disk. Wire into your session cleanup path.
func (s *SQLiteStore) DeleteBySession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM artifacts WHERE session_id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("artifact.DeleteBySession: %w", err)
	}
	// Remove large-content files for the session.
	dir := filepath.Join(s.artifactsDir, sessionID)
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("artifact.DeleteBySession: remove dir %s: %w", dir, err)
	}
	return nil
}

// Archive deletes artifacts in terminal states (rejected, failed, superseded)
// whose updated_at is older than olderThan. Returns the number of rows deleted.
func (s *SQLiteStore) Archive(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := time.Now().UTC().Add(-olderThan).Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM artifacts
		WHERE status IN ('rejected', 'failed', 'superseded')
		  AND updated_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("artifact.Archive: delete: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("artifact.Archive: rows affected: %w", err)
	}
	return int(n), nil
}

// --- helpers ---

type scanner interface {
	Scan(dest ...any) error
}

func scanArtifact(row scanner) (*workforce.Artifact, error) {
	var (
		id, kind, title        string
		mimeType               sql.NullString
		content                []byte
		contentRef             sql.NullString
		metaJSON               []byte
		agentName, sessionID   string
		threadID               sql.NullString
		trigMsgID              sql.NullString
		status                 string
		rejReason              sql.NullString
		createdStr, updatedStr string
	)

	err := row.Scan(
		&id, &kind, &title, &mimeType, &content, &contentRef, &metaJSON,
		&agentName, &threadID, &sessionID, &trigMsgID,
		&status, &rejReason, &createdStr, &updatedStr,
	)
	if err != nil {
		return nil, err
	}

	a := &workforce.Artifact{
		ID:                  id,
		Kind:                workforce.ArtifactKind(kind),
		Title:               title,
		MimeType:            mimeType.String,
		Content:             content,
		ContentRef:          contentRef.String,
		AgentName:           agentName,
		ThreadID:            threadID.String,
		SessionID:           sessionID,
		TriggeringMessageID: trigMsgID.String,
		Status:              workforce.ArtifactStatus(status),
		RejectionReason:     rejReason.String,
	}

	if len(metaJSON) > 0 {
		if err := json.Unmarshal(metaJSON, &a.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal metadata_json: %w", err)
		}
	}

	if t, err := time.Parse(time.RFC3339Nano, createdStr); err == nil {
		a.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339Nano, updatedStr); err == nil {
		a.UpdatedAt = t
	}

	return a, nil
}

func scanArtifacts(rows *sql.Rows) ([]*workforce.Artifact, error) {
	var out []*workforce.Artifact
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, fmt.Errorf("artifact.scan: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("artifact.scan rows: %w", err)
	}
	return out, nil
}

func validateTransition(from, to workforce.ArtifactStatus) error {
	// Idempotent: same state is always valid.
	if from == to {
		return nil
	}
	switch from {
	case workforce.StatusDraft:
		switch to {
		case workforce.StatusAccepted, workforce.StatusRejected,
			workforce.StatusSuperseded, workforce.StatusFailed,
			workforce.StatusDeleted:
			return nil
		}
	case workforce.StatusAccepted:
		switch to {
		case workforce.StatusSuperseded, workforce.StatusDeleted:
			return nil
		}
	case workforce.StatusRejected, workforce.StatusFailed, workforce.StatusSuperseded:
		if to == workforce.StatusDeleted {
			return nil
		}
	}
	return fmt.Errorf("artifact: invalid status transition %s → %s", from, to)
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultListLimit
	}
	if limit > maxListLimit {
		return maxListLimit
	}
	return limit
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullableBytes(b []byte) interface{} {
	if len(b) == 0 {
		return nil
	}
	return b
}
