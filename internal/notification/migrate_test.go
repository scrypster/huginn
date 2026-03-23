package notification_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/scrypster/huginn/internal/notification"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

func openMigratePebble(t *testing.T) *pebble.DB {
	t.Helper()
	db, err := pebble.Open(t.TempDir(), &pebble.Options{})
	if err != nil {
		t.Fatalf("pebble.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func openMigrateSQLite(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func makeTestNotif(id, routineID, runID string) *notification.Notification {
	now := time.Now().UTC().Truncate(time.Second)
	return &notification.Notification{
		ID:        id,
		RoutineID: routineID,
		RunID:     runID,
		Summary:   "Summary for " + id,
		Detail:    "Detail body for " + id,
		Severity:  notification.SeverityInfo,
		Status:    notification.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// TestMigrateNotifFromPebble_MigratesRecords seeds 3 notifications into Pebble
// and verifies all 3 appear in SQLite after migration.
func TestMigrateNotifFromPebble_MigratesRecords(t *testing.T) {
	t.Parallel()

	pdb := openMigratePebble(t)
	sqlDB := openMigrateSQLite(t)

	// Seed 3 notifications into Pebble via the existing Pebble Store.
	pstore := notification.NewStore(pdb)
	n1 := makeTestNotif(notification.NewID(), "routine-1", "run-1")
	n2 := makeTestNotif(notification.NewID(), "routine-1", "run-2")
	n3 := makeTestNotif(notification.NewID(), "routine-2", "run-3")
	for _, n := range []*notification.Notification{n1, n2, n3} {
		if err := pstore.Put(n); err != nil {
			t.Fatalf("pstore.Put: %v", err)
		}
	}

	if err := notification.MigrateFromPebble(pdb, sqlDB); err != nil {
		t.Fatalf("MigrateFromPebble: %v", err)
	}

	sstore := notification.NewSQLiteNotificationStore(sqlDB)
	// All 3 are pending, so ListPending returns them.
	list, err := sstore.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if got := len(list); got != 3 {
		t.Errorf("ListPending count = %d, want 3", got)
	}
}

// TestMigrateNotifFromPebble_Idempotent verifies running the migration twice
// still results in exactly 1 record in SQLite.
func TestMigrateNotifFromPebble_Idempotent(t *testing.T) {
	t.Parallel()

	pdb := openMigratePebble(t)
	sqlDB := openMigrateSQLite(t)

	pstore := notification.NewStore(pdb)
	n := makeTestNotif(notification.NewID(), "routine-1", "run-1")
	if err := pstore.Put(n); err != nil {
		t.Fatalf("pstore.Put: %v", err)
	}

	// First migration run.
	if err := notification.MigrateFromPebble(pdb, sqlDB); err != nil {
		t.Fatalf("MigrateFromPebble (1st): %v", err)
	}

	// Second migration run — must be idempotent.
	if err := notification.MigrateFromPebble(pdb, sqlDB); err != nil {
		t.Fatalf("MigrateFromPebble (2nd): %v", err)
	}

	sstore := notification.NewSQLiteNotificationStore(sqlDB)
	list, err := sstore.ListPending()
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if got := len(list); got != 1 {
		t.Errorf("ListPending count after 2 migrations = %d, want 1", got)
	}
}

// TestMigrateNotifFromPebble_EmptyPebble verifies that migrating an empty
// Pebble store produces no error and records the migration.
func TestMigrateNotifFromPebble_EmptyPebble(t *testing.T) {
	t.Parallel()

	pdb := openMigratePebble(t)
	sqlDB := openMigrateSQLite(t)

	if err := notification.MigrateFromPebble(pdb, sqlDB); err != nil {
		t.Fatalf("MigrateFromPebble (empty): %v", err)
	}

	// Verify the migration row was written.
	var count int
	if err := sqlDB.Read().QueryRow(
		`SELECT COUNT(*) FROM _migrations WHERE name = 'M3_notifications'`,
	).Scan(&count); err != nil {
		t.Fatalf("query _migrations: %v", err)
	}
	if count != 1 {
		t.Errorf("_migrations count = %d, want 1", count)
	}
}

// TestMigrateNotifFromPebble_RecordsMigration verifies that _migrations has
// exactly 1 row with name='M3_notifications' after migration.
func TestMigrateNotifFromPebble_RecordsMigration(t *testing.T) {
	t.Parallel()

	pdb := openMigratePebble(t)
	sqlDB := openMigrateSQLite(t)

	pstore := notification.NewStore(pdb)
	n := makeTestNotif(notification.NewID(), "routine-1", "run-1")
	if err := pstore.Put(n); err != nil {
		t.Fatalf("pstore.Put: %v", err)
	}

	if err := notification.MigrateFromPebble(pdb, sqlDB); err != nil {
		t.Fatalf("MigrateFromPebble: %v", err)
	}

	var name string
	var recordCount int
	if err := sqlDB.Read().QueryRow(
		`SELECT name, record_count FROM _migrations WHERE name = 'M3_notifications'`,
	).Scan(&name, &recordCount); err != nil {
		t.Fatalf("query _migrations: %v", err)
	}
	if name != "M3_notifications" {
		t.Errorf("migration name = %q, want 'M3_notifications'", name)
	}
	if recordCount != 1 {
		t.Errorf("migration record_count = %d, want 1", recordCount)
	}
}

// TestMigrateNotifFromPebble_PreservesFields seeds a notification with
// WorkflowID set and verifies all fields round-trip correctly.
func TestMigrateNotifFromPebble_PreservesFields(t *testing.T) {
	t.Parallel()

	pdb := openMigratePebble(t)
	sqlDB := openMigrateSQLite(t)

	pstore := notification.NewStore(pdb)

	now := time.Now().UTC().Truncate(time.Second)
	exp := now.Add(24 * time.Hour)
	original := &notification.Notification{
		ID:            notification.NewID(),
		RoutineID:     "routine-abc",
		RunID:         "run-xyz",
		SatelliteID:   "sat-1",
		WorkflowID:    "workflow-99",
		WorkflowRunID: "wfrun-99",
		Summary:       "Important alert",
		Detail:        "Detailed markdown here",
		Severity:      notification.SeverityUrgent,
		Status:        notification.StatusPending,
		SessionID:     "sess-42",
		ProposedActions: []notification.ProposedAction{
			{
				ID:          "action-1",
				Label:       "Run fix",
				ToolName:    "bash",
				ToolParams:  map[string]any{"cmd": "echo hello"},
				Destructive: true,
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: &exp,
	}

	if err := pstore.Put(original); err != nil {
		t.Fatalf("pstore.Put: %v", err)
	}

	if err := notification.MigrateFromPebble(pdb, sqlDB); err != nil {
		t.Fatalf("MigrateFromPebble: %v", err)
	}

	sstore := notification.NewSQLiteNotificationStore(sqlDB)
	got, err := sstore.Get(original.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.ID != original.ID {
		t.Errorf("ID: got %q, want %q", got.ID, original.ID)
	}
	if got.RoutineID != original.RoutineID {
		t.Errorf("RoutineID: got %q, want %q", got.RoutineID, original.RoutineID)
	}
	if got.RunID != original.RunID {
		t.Errorf("RunID: got %q, want %q", got.RunID, original.RunID)
	}
	if got.WorkflowID != original.WorkflowID {
		t.Errorf("WorkflowID: got %q, want %q", got.WorkflowID, original.WorkflowID)
	}
	if got.WorkflowRunID != original.WorkflowRunID {
		t.Errorf("WorkflowRunID: got %q, want %q", got.WorkflowRunID, original.WorkflowRunID)
	}
	if got.Summary != original.Summary {
		t.Errorf("Summary: got %q, want %q", got.Summary, original.Summary)
	}
	if got.Detail != original.Detail {
		t.Errorf("Detail: got %q, want %q", got.Detail, original.Detail)
	}
	if got.Severity != original.Severity {
		t.Errorf("Severity: got %q, want %q", got.Severity, original.Severity)
	}
	if got.Status != original.Status {
		t.Errorf("Status: got %q, want %q", got.Status, original.Status)
	}
	if got.SessionID != original.SessionID {
		t.Errorf("SessionID: got %q, want %q", got.SessionID, original.SessionID)
	}
	if got.SatelliteID != original.SatelliteID {
		t.Errorf("SatelliteID: got %q, want %q", got.SatelliteID, original.SatelliteID)
	}
	if !got.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("CreatedAt: got %v, want %v", got.CreatedAt, original.CreatedAt)
	}
	if !got.UpdatedAt.Equal(original.UpdatedAt) {
		t.Errorf("UpdatedAt: got %v, want %v", got.UpdatedAt, original.UpdatedAt)
	}
	if got.ExpiresAt == nil {
		t.Error("ExpiresAt: got nil, want non-nil")
	} else if !got.ExpiresAt.Equal(exp) {
		t.Errorf("ExpiresAt: got %v, want %v", got.ExpiresAt, exp)
	}
	if len(got.ProposedActions) != 1 {
		t.Errorf("ProposedActions len: got %d, want 1", len(got.ProposedActions))
	} else {
		pa := got.ProposedActions[0]
		if pa.Label != "Run fix" {
			t.Errorf("ProposedAction.Label: got %q, want 'Run fix'", pa.Label)
		}
		if pa.ToolName != "bash" {
			t.Errorf("ProposedAction.ToolName: got %q, want 'bash'", pa.ToolName)
		}
		if !pa.Destructive {
			t.Error("ProposedAction.Destructive: got false, want true")
		}
	}
}
