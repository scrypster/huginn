package sqlitedb

import "strings"

// IsColumnExistsError returns true when an ALTER TABLE ADD COLUMN statement
// fails because the column already exists. SQLite reports this as
// "duplicate column name" via modernc.org/sqlite.
//
// Use this in migrations that use ALTER TABLE ADD COLUMN to make them
// idempotent — safe to re-run on databases where the column was already added
// by a previous migration run or by ApplySchema.
func IsColumnExistsError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column") ||
		strings.Contains(msg, "already exists")
}
