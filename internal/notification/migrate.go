package notification

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

const notifMigrationName = "M3_notifications"

// MigrationOptions configures the Pebble -> SQLite migration behavior.
type MigrationOptions struct {
	// Strict causes malformed Pebble records to fail the migration instead of
	// being skipped.
	Strict bool
	// DeleteSource controls whether notifications/* keys are removed from Pebble
	// after a successful SQLite commit.
	DeleteSource bool
}

// MigrationReport captures migration outcomes for operator observability.
type MigrationReport struct {
	Name             string
	ScannedRecords   int
	MigratedRecords  int
	SkippedMalformed int
	AlreadyComplete  bool
	DeletedSource    bool
	StartedAt        time.Time
	CompletedAt      time.Time
	Duration         time.Duration
}

// MigrateFromPebble migrates all notifications from Pebble KV into SQLite.
// It is idempotent: if M3_notifications is already recorded in _migrations,
// it returns nil immediately. After a successful commit, all notifications/
// keys are deleted from Pebble (non-fatal if deletion fails).
func MigrateFromPebble(pdb *pebble.DB, sqlDB *sqlitedb.DB) error {
	_, err := MigrateFromPebbleWithOptions(pdb, sqlDB, MigrationOptions{DeleteSource: true})
	return err
}

// MigrateFromPebbleWithOptions migrates all notifications from Pebble KV into SQLite.
// Returns a structured report with counts and timing metadata.
func MigrateFromPebbleWithOptions(pdb *pebble.DB, sqlDB *sqlitedb.DB, opts MigrationOptions) (report MigrationReport, err error) {
	report = MigrationReport{
		Name:      notifMigrationName,
		StartedAt: time.Now().UTC(),
	}
	defer func() {
		report.CompletedAt = time.Now().UTC()
		report.Duration = report.CompletedAt.Sub(report.StartedAt)
	}()

	if pdb == nil {
		return report, fmt.Errorf("notification migrate: nil pebble db")
	}
	if sqlDB == nil {
		return report, fmt.Errorf("notification migrate: nil sqlite db")
	}

	// 1. Idempotency check.
	done, err := notifMigDone(sqlDB.Read(), notifMigrationName)
	if err != nil {
		return report, fmt.Errorf("notification migrate: check: %w", err)
	}
	if done {
		slog.Debug("notification migrate: already complete")
		report.AlreadyComplete = true
		return report, nil
	}

	// 2. Scan notifications/id/ prefix to collect all canonical records.
	prefix := []byte(pfxByID)
	upper := keyUpperBound(prefix)

	iter, err := pdb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: upper,
	})
	if err != nil {
		return report, fmt.Errorf("notification migrate: open iter: %w", err)
	}
	defer iter.Close()

	var notifications []*Notification
	for iter.First(); iter.Valid(); iter.Next() {
		report.ScannedRecords++
		var n Notification
		if err := json.Unmarshal(iter.Value(), &n); err != nil {
			report.SkippedMalformed++
			if opts.Strict {
				return report, fmt.Errorf("notification migrate: malformed record at key %q: %w", string(iter.Key()), err)
			}
			slog.Warn("notification migrate: skip malformed record",
				"key", string(iter.Key()), "err", err)
			continue
		}
		notifications = append(notifications, &n)
	}
	if err := iter.Error(); err != nil {
		return report, fmt.Errorf("notification migrate: iter error: %w", err)
	}
	report.MigratedRecords = len(notifications)

	// 3. Single transaction: INSERT OR IGNORE all records + INSERT _migrations.
	tx, err := sqlDB.Write().Begin()
	if err != nil {
		return report, fmt.Errorf("notification migrate: begin tx: %w", err)
	}

	for _, n := range notifications {
		if err := notifMigInsert(tx, n); err != nil {
			tx.Rollback()
			return report, fmt.Errorf("notification migrate: insert %q: %w", n.ID, err)
		}
	}

	if _, err := tx.Exec(
		`INSERT INTO _migrations (name, record_count, source_path) VALUES (?, ?, ?)`,
		notifMigrationName, len(notifications), "pebble",
	); err != nil {
		tx.Rollback()
		return report, fmt.Errorf("notification migrate: record migration: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return report, fmt.Errorf("notification migrate: commit: %w", err)
	}

	slog.Info("notification migrate: complete", "count", len(notifications))

	// 4. Delete all notifications/ Pebble keys (non-fatal).
	if opts.DeleteSource {
		allPrefix := []byte("notifications/")
		b := pdb.NewBatch()
		if err := b.DeleteRange(allPrefix, keyUpperBound(allPrefix), pebble.Sync); err != nil {
			b.Close()
			slog.Warn("notification migrate: failed to delete pebble keys", "err", err)
			return report, nil
		}
		if err := b.Commit(pebble.Sync); err != nil {
			slog.Warn("notification migrate: failed to commit pebble delete", "err", err)
		} else {
			report.DeletedSource = true
		}
		b.Close()
	}

	return report, nil
}

// notifMigDone checks whether a migration with the given name has been recorded.
func notifMigDone(db *sql.DB, name string) (bool, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM _migrations WHERE name = ?`, name).Scan(&count)
	return count > 0, err
}

// notifMigInsert inserts a single Notification into SQLite within a transaction.
// Uses INSERT OR IGNORE so re-running is safe.
func notifMigInsert(tx *sql.Tx, n *Notification) error {
	actions := n.ProposedActions
	if actions == nil {
		actions = []ProposedAction{}
	}
	actionsJSON, err := json.Marshal(actions)
	if err != nil {
		return fmt.Errorf("marshal proposed_actions: %w", err)
	}

	createdAt := n.CreatedAt.UTC().Format(time.RFC3339)
	if n.CreatedAt.IsZero() {
		createdAt = time.Now().UTC().Format(time.RFC3339)
	}
	updatedAt := n.UpdatedAt.UTC().Format(time.RFC3339)
	if n.UpdatedAt.IsZero() {
		updatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	var expiresAt *string
	if n.ExpiresAt != nil && !n.ExpiresAt.IsZero() {
		s := n.ExpiresAt.UTC().Format(time.RFC3339)
		expiresAt = &s
	}

	severity := string(n.Severity)
	if severity == "" {
		severity = string(SeverityInfo)
	}
	status := string(n.Status)
	if status == "" {
		status = string(StatusPending)
	}

	deliveries := n.Deliveries
	if deliveries == nil {
		deliveries = []DeliveryRecord{}
	}
	deliveriesJSON, err := json.Marshal(deliveries)
	if err != nil {
		return fmt.Errorf("marshal deliveries: %w", err)
	}

	_, err = tx.Exec(`
		INSERT OR IGNORE INTO notifications
			(id, routine_id, run_id, satellite_id, workflow_id, workflow_run_id,
			 summary, detail, severity, status, session_id, proposed_actions,
			 step_position, step_name, deliveries,
			 created_at, updated_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.RoutineID, n.RunID, n.SatelliteID, n.WorkflowID, n.WorkflowRunID,
		n.Summary, n.Detail, severity, status, n.SessionID, string(actionsJSON),
		n.StepPosition, n.StepName, string(deliveriesJSON),
		createdAt, updatedAt, expiresAt,
	)
	return err
}
