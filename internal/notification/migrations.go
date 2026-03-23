package notification

import (
	"database/sql"
	"strings"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// Migrations returns an empty list — all schema is now in the base schema DDL.
func Migrations() []sqlitedb.Migration {
	return nil
}

// migrateNotificationsV1 adds the step_position, step_name, and deliveries
// columns to the notifications table. These columns were added in Round 2 of
// the Huginn hardening pass and may not exist in databases created before that
// point.
//
// ALTER TABLE ADD COLUMN in SQLite returns an error if the column already
// exists. isNotifColumnExistsError() tolerates this so the migration is
// idempotent on re-run and on databases that already have these columns (e.g.
// fresh installs where ApplySchema() already created them).
func migrateNotificationsV1(tx *sql.Tx) error {
	alters := []string{
		// step_position: nullable INTEGER — NULL for workflow-level notifications.
		`ALTER TABLE notifications ADD COLUMN step_position INTEGER`,
		// step_name: TEXT with default '' — empty for workflow-level notifications.
		`ALTER TABLE notifications ADD COLUMN step_name TEXT NOT NULL DEFAULT ''`,
		// deliveries: TEXT JSON array of DeliveryRecord structs.
		`ALTER TABLE notifications ADD COLUMN deliveries TEXT NOT NULL DEFAULT '[]'`,
	}
	for _, stmt := range alters {
		if _, err := tx.Exec(stmt); err != nil {
			if isNotifColumnExistsError(err) {
				continue
			}
			return err
		}
	}
	return nil
}

func isNotifColumnExistsError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column") ||
		strings.Contains(msg, "already exists")
}
