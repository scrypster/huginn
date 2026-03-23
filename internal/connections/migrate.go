package connections

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// MigrateFromJSON reads connections from the JSON file at jsonPath,
// inserts them into SQLite, records the migration in _migrations, and
// renames the file to jsonPath+".bak".
//
// Idempotent: if M1_connections is already in _migrations, returns nil immediately.
// If the file does not exist (or is already a .bak), returns nil.
func MigrateFromJSON(jsonPath string, db *sqlitedb.DB) error {
	// Check if already migrated.
	done, err := migrationDone(db.Read(), "M1_connections")
	if err != nil {
		return fmt.Errorf("connections migrate: check: %w", err)
	}
	if done {
		return nil
	}

	// Read JSON file. Missing file = fresh install, treat as empty migration.
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return recordMigration(db.Write(), "M1_connections", 0, jsonPath)
		}
		return fmt.Errorf("connections migrate: read %s: %w", jsonPath, err)
	}

	var conns []Connection
	if len(data) > 0 {
		if err := json.Unmarshal(data, &conns); err != nil {
			return fmt.Errorf("connections migrate: parse JSON: %w", err)
		}
	}

	// Migrate in a single transaction.
	tx, err := db.Write().Begin()
	if err != nil {
		return fmt.Errorf("connections migrate: begin tx: %w", err)
	}

	for _, conn := range conns {
		if err := insertConnectionTx(tx, conn); err != nil {
			tx.Rollback()
			return fmt.Errorf("connections migrate: insert %q: %w", conn.ID, err)
		}
	}

	// Record migration within same transaction.
	if _, err := tx.Exec(
		`INSERT INTO _migrations (name, record_count, source_path) VALUES (?, ?, ?)`,
		"M1_connections", len(conns), jsonPath,
	); err != nil {
		tx.Rollback()
		return fmt.Errorf("connections migrate: record migration: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("connections migrate: commit: %w", err)
	}

	// Rename JSON to .bak after successful commit.
	bakPath := jsonPath + ".bak"
	if err := os.Rename(jsonPath, bakPath); err != nil {
		// Non-fatal: data is already safe in SQLite.
		fmt.Fprintf(os.Stderr, "connections migrate: rename %s: %v\n", jsonPath, err)
	}
	return nil
}

func migrationDone(db *sql.DB, name string) (bool, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM _migrations WHERE name = ?`, name).Scan(&count)
	return count > 0, err
}

func recordMigration(db *sql.DB, name string, count int, sourcePath string) error {
	_, err := db.Exec(
		`INSERT OR IGNORE INTO _migrations (name, record_count, source_path) VALUES (?, ?, ?)`,
		name, count, sourcePath,
	)
	return err
}

func insertConnectionTx(tx *sql.Tx, conn Connection) error {
	// Normalize scopes
	scopes := conn.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	scopesJSON, _ := json.Marshal(scopes)

	// Normalize metadata
	metaJSON, _ := marshalMetadata(conn.Metadata)

	// Normalize type
	connType := string(conn.Type)
	if connType == "" {
		connType = string(ConnectionTypeOAuth)
	}

	// Format timestamps
	var expiresAt *string
	if !conn.ExpiresAt.IsZero() {
		s := conn.ExpiresAt.UTC().Format(time.RFC3339)
		expiresAt = &s
	}
	createdAt := conn.CreatedAt.UTC().Format(time.RFC3339)
	if conn.CreatedAt.IsZero() {
		createdAt = time.Now().UTC().Format(time.RFC3339)
	}

	_, err := tx.Exec(`
		INSERT OR IGNORE INTO connections
			(id, provider, type, account_label, account_id, scopes, metadata, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		conn.ID, string(conn.Provider), connType,
		conn.AccountLabel, conn.AccountID,
		string(scopesJSON), metaJSON, createdAt, expiresAt,
	)
	return err
}
