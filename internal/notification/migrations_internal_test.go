package notification

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

func openMigrationsSQLite(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "notif-migrations.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrations_ReturnsNil(t *testing.T) {
	if got := Migrations(); got != nil {
		t.Fatalf("Migrations() = %#v, want nil (schema is in base DDL)", got)
	}
}

func TestMigrateNotificationsV1_IdempotentAgainstBaseSchema(t *testing.T) {
	db := openMigrationsSQLite(t)
	tx, err := db.Write().Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Base schema already includes these columns; migration must ignore duplicate
	// column errors and return nil.
	if err := migrateNotificationsV1(tx); err != nil {
		t.Fatalf("migrateNotificationsV1 should be idempotent on base schema: %v", err)
	}
}

func TestIsNotifColumnExistsError_DetectsDuplicateColumnText(t *testing.T) {
	if !isNotifColumnExistsError(errors.New("duplicate column name: deliveries")) {
		t.Fatal("expected duplicate column message to be treated as idempotent")
	}
	if !isNotifColumnExistsError(errors.New("column already exists: step_name")) {
		t.Fatal("expected already-exists message to be treated as idempotent")
	}
	if isNotifColumnExistsError(errors.New("database is locked")) {
		t.Fatal("non-column-exists errors must not be swallowed")
	}
}
