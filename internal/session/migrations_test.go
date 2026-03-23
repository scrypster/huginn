package session_test

import (
	"database/sql"
	"testing"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// TestMigrations_Idempotent verifies that calling Migrations() and Migrate()
// twice produces no errors (migrations are squashed into ApplySchema).
func TestMigrations_Idempotent(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)

	// Migrations() returns nil (squashed into base schema), so both calls are no-ops.
	if err := db.Migrate(session.Migrations()); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := db.Migrate(session.Migrations()); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}

	// The _migrations table should have 0 rows (no rolling migrations registered).
	var count int
	if err := db.Read().QueryRow(`SELECT COUNT(*) FROM _migrations`).Scan(&count); err != nil {
		t.Fatalf("query migrations count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 migration rows (all squashed into schema), got %d", count)
	}
}

// TestMigration_RollbackOnFailure verifies that migrations are rolled back if they fail.
func TestMigration_RollbackOnFailure(t *testing.T) {
	t.Parallel()
	db := openSessTestDB(t)

	// Apply the initial schema (this creates _migrations table)
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}

	// Define a failing migration that tries to create a table twice
	failingMigration := session.Migrations()
	failingMigration = append(failingMigration, sqlitedb.Migration{
		Name: "test_migration_failure",
		Up: func(tx *sql.Tx) error {
			// Create a table
			if _, err := tx.Exec(`CREATE TABLE test_table (id INTEGER PRIMARY KEY)`); err != nil {
				return err
			}
			// Try to create the same table again — this will fail
			if _, err := tx.Exec(`CREATE TABLE test_table (id INTEGER PRIMARY KEY)`); err != nil {
				return err
			}
			return nil
		},
	})

	// First, apply the non-failing migrations
	if err := db.Migrate(session.Migrations()); err != nil {
		t.Fatalf("initial Migrate: %v", err)
	}

	// Record the migration count before the failing migration
	var countBefore int
	err := db.Read().QueryRow(`SELECT COUNT(*) FROM _migrations`).Scan(&countBefore)
	if err != nil {
		t.Fatalf("query migrations before: %v", err)
	}

	// Try to apply the failing migration
	failingMigs := failingMigration[len(session.Migrations()):]
	err = db.Migrate(failingMigs)
	if err == nil {
		t.Fatal("expected migration to fail, but it succeeded")
	}
	t.Logf("migration failed as expected: %v", err)

	// Verify that the migration was NOT recorded (rolled back)
	var countAfter int
	errAfter := db.Read().QueryRow(`SELECT COUNT(*) FROM _migrations`).Scan(&countAfter)
	if errAfter != nil {
		t.Fatalf("query migrations after: %v", errAfter)
	}

	if countAfter != countBefore {
		t.Errorf("migration count changed despite rollback: before %d, after %d", countBefore, countAfter)
	}

	// Verify that test_table was NOT created (rolled back)
	var tableExists int
	err = db.Read().QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='test_table'`,
	).Scan(&tableExists)
	if err != nil {
		t.Fatalf("query table existence: %v", err)
	}
	if tableExists > 0 {
		t.Error("test_table should not exist after failed migration (rollback)")
	}
}
