package notification_test

import (
	"path/filepath"
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/scrypster/huginn/internal/notification"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

func TestBootstrap_SQLiteStoreOnSuccessfulMigration(t *testing.T) {
	t.Parallel()
	pdb := openMigratePebble(t)
	sqlDB := openMigrateSQLite(t)

	store, report, err := notification.Bootstrap(pdb, sqlDB, notification.BootstrapOptions{})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if report.Backend != notification.BackendSQLite {
		t.Fatalf("backend = %q, want %q", report.Backend, notification.BackendSQLite)
	}
	if report.MigrationError != "" {
		t.Fatalf("unexpected migration error: %s", report.MigrationError)
	}
	if _, ok := store.(*notification.SQLiteNotificationStore); !ok {
		t.Fatalf("store type = %T, want *notification.SQLiteNotificationStore", store)
	}
}

func TestBootstrap_MigrationFailureFallsBackToPebble(t *testing.T) {
	t.Parallel()
	pdb := openMigratePebble(t)

	// Open SQLite without schema so migration fails in idempotency check.
	sqlDB, err := sqlitedb.Open(filepath.Join(t.TempDir(), "broken.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	store, report, err := notification.Bootstrap(pdb, sqlDB, notification.BootstrapOptions{})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if report.Backend != notification.BackendPebble {
		t.Fatalf("backend = %q, want %q", report.Backend, notification.BackendPebble)
	}
	if report.MigrationError == "" {
		t.Fatal("expected migration error to be reported")
	}
	if _, ok := store.(*notification.Store); !ok {
		t.Fatalf("store type = %T, want *notification.Store", store)
	}
}

func TestBootstrap_NilPebbleReturnsError(t *testing.T) {
	t.Parallel()
	sqlDB := openMigrateSQLite(t)
	_, _, err := notification.Bootstrap((*pebble.DB)(nil), sqlDB, notification.BootstrapOptions{})
	if err == nil {
		t.Fatal("expected error for nil pebble db")
	}
}
