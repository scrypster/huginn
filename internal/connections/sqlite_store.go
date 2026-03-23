package connections

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// SQLiteConnectionStore implements StoreInterface backed by a SQLite database.
type SQLiteConnectionStore struct {
	db *sqlitedb.DB
}

// NewSQLiteConnectionStore creates a new SQLiteConnectionStore.
func NewSQLiteConnectionStore(db *sqlitedb.DB) *SQLiteConnectionStore {
	return &SQLiteConnectionStore{db: db}
}

// Compile-time assertion.
var _ StoreInterface = (*SQLiteConnectionStore)(nil)

func (s *SQLiteConnectionStore) Add(conn Connection) error {
	// Normalize scopes: nil becomes []string{}
	scopes := conn.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return fmt.Errorf("connections: marshal scopes: %w", err)
	}

	metaJSON, err := marshalMetadata(conn.Metadata)
	if err != nil {
		return fmt.Errorf("connections: marshal metadata: %w", err)
	}

	connType := string(conn.Type)
	if connType == "" {
		connType = string(ConnectionTypeOAuth)
	}

	var expiresAt *string
	if !conn.ExpiresAt.IsZero() {
		s := conn.ExpiresAt.UTC().Format(time.RFC3339)
		expiresAt = &s
	}

	createdAt := conn.CreatedAt.UTC().Format(time.RFC3339)
	if conn.CreatedAt.IsZero() {
		createdAt = time.Now().UTC().Format(time.RFC3339)
	}

	_, err = s.db.Write().Exec(`
		INSERT INTO connections (id, provider, type, account_label, account_id, scopes, metadata, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		conn.ID, string(conn.Provider), connType,
		conn.AccountLabel, conn.AccountID,
		string(scopesJSON), string(metaJSON),
		createdAt, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("connections: add %q: %w", conn.ID, err)
	}
	return nil
}

func (s *SQLiteConnectionStore) Remove(id string) error {
	res, err := s.db.Write().Exec(`DELETE FROM connections WHERE id = ?`, id)
	if err != nil {
		return errStorage("Remove", fmt.Sprintf("delete connection %q", id), err)
	}
	_ = res
	return nil
}

func (s *SQLiteConnectionStore) Get(id string) (Connection, bool) {
	row := s.db.Read().QueryRow(`
		SELECT id, provider, type, account_label, account_id, scopes, metadata, created_at, expires_at
		FROM connections WHERE id = ?`, id)
	conn, err := scanConnection(row)
	if err != nil {
		return Connection{}, false
	}
	return conn, true
}

func (s *SQLiteConnectionStore) List() ([]Connection, error) {
	rows, err := s.db.Read().Query(`
		SELECT id, provider, type, account_label, account_id, scopes, metadata, created_at, expires_at
		FROM connections WHERE tenant_id = '' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanConnections(rows)
}

func (s *SQLiteConnectionStore) ListByProvider(p Provider) ([]Connection, error) {
	rows, err := s.db.Read().Query(`
		SELECT id, provider, type, account_label, account_id, scopes, metadata, created_at, expires_at
		FROM connections WHERE tenant_id = '' AND provider = ? ORDER BY created_at`, string(p))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanConnections(rows)
}

// SetDefault makes the given connection the first (default) for its provider
// by setting its created_at to 1 second before the current earliest for that provider.
func (s *SQLiteConnectionStore) SetDefault(id string) error {
	conn, ok := s.Get(id)
	if !ok {
		return errNotFound("SetDefault", fmt.Sprintf("connection %q not found", id))
	}
	// Find earliest created_at for this provider
	var earliestStr string
	err := s.db.Read().QueryRow(`
		SELECT created_at FROM connections WHERE provider = ? AND tenant_id = '' ORDER BY created_at LIMIT 1`,
		string(conn.Provider)).Scan(&earliestStr)
	if err != nil {
		return fmt.Errorf("connections: find earliest for provider: %w", err)
	}
	earliest, err := time.Parse(time.RFC3339, earliestStr)
	if err != nil {
		return fmt.Errorf("connections: parse earliest created_at: %w", err)
	}
	newCreatedAt := earliest.Add(-time.Second)
	_, err = s.db.Write().Exec(`
		UPDATE connections SET created_at = ? WHERE id = ?`,
		newCreatedAt.UTC().Format(time.RFC3339), id)
	return err
}

func (s *SQLiteConnectionStore) UpdateExpiry(id string, expiresAt time.Time) error {
	var expiresStr *string
	if !expiresAt.IsZero() {
		str := expiresAt.UTC().Format(time.RFC3339)
		expiresStr = &str
	}
	res, err := s.db.Write().Exec(`
		UPDATE connections SET expires_at = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ?`, expiresStr, id)
	if err != nil {
		return errStorage("UpdateExpiry", fmt.Sprintf("update expiry for %q", id), err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errNotFound("UpdateExpiry", fmt.Sprintf("connection %q not found", id))
	}
	return nil
}

// UpdateRefreshError persists (or clears) the refresh failure state for a connection.
// Pass empty errMsg to clear a previously recorded error on successful refresh.
func (s *SQLiteConnectionStore) UpdateRefreshError(id string, errMsg string) error {
	var failedAt *string
	if errMsg != "" {
		now := time.Now().UTC().Format(time.RFC3339)
		failedAt = &now
	}
	res, err := s.db.Write().Exec(`
		UPDATE connections
		SET refresh_failed_at = ?, last_refresh_error = ?,
		    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ?`, failedAt, errMsg, id)
	if err != nil {
		return errStorage("UpdateRefreshError", fmt.Sprintf("update refresh error for %q", id), err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errNotFound("UpdateRefreshError", fmt.Sprintf("connection %q not found", id))
	}
	return nil
}

// scanConnection reads a single Connection from a sql.Row.
func scanConnection(row *sql.Row) (Connection, error) {
	var conn Connection
	var providerStr, typeStr, scopesJSON, metaJSON, createdAtStr string
	var expiresAtStr *string
	err := row.Scan(&conn.ID, &providerStr, &typeStr, &conn.AccountLabel, &conn.AccountID,
		&scopesJSON, &metaJSON, &createdAtStr, &expiresAtStr)
	if err != nil {
		return Connection{}, err
	}
	return hydrateConnection(conn, providerStr, typeStr, scopesJSON, metaJSON, createdAtStr, expiresAtStr)
}

// scanConnections reads all Connections from sql.Rows.
func scanConnections(rows *sql.Rows) ([]Connection, error) {
	var out []Connection
	for rows.Next() {
		var conn Connection
		var providerStr, typeStr, scopesJSON, metaJSON, createdAtStr string
		var expiresAtStr *string
		if err := rows.Scan(&conn.ID, &providerStr, &typeStr, &conn.AccountLabel, &conn.AccountID,
			&scopesJSON, &metaJSON, &createdAtStr, &expiresAtStr); err != nil {
			return nil, err
		}
		hydrated, err := hydrateConnection(conn, providerStr, typeStr, scopesJSON, metaJSON, createdAtStr, expiresAtStr)
		if err != nil {
			return nil, err
		}
		out = append(out, hydrated)
	}
	return out, rows.Err()
}

func hydrateConnection(conn Connection, providerStr, typeStr, scopesJSON, metaJSON, createdAtStr string, expiresAtStr *string) (Connection, error) {
	conn.Provider = Provider(providerStr)
	conn.Type = ConnectionType(typeStr)

	if err := json.Unmarshal([]byte(scopesJSON), &conn.Scopes); err != nil {
		conn.Scopes = nil
	}

	conn.Metadata = unmarshalMetadata(metaJSON)

	if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
		conn.CreatedAt = t
	}

	if expiresAtStr != nil {
		if t, err := time.Parse(time.RFC3339, *expiresAtStr); err == nil {
			conn.ExpiresAt = t
		}
	}

	return conn, nil
}

func marshalMetadata(m map[string]string) (string, error) {
	if m == nil {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	return string(b), err
}

func unmarshalMetadata(s string) map[string]string {
	if s == "" || s == "{}" {
		return nil
	}
	var m map[string]string
	json.Unmarshal([]byte(s), &m)
	return m
}
