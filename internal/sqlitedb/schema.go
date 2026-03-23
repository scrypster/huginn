package sqlitedb

import (
	_ "embed"
	"fmt"
)

//go:embed schema/huginn-sqlite-schema.sql
var schemaSQL string

// ApplySchema applies the embedded DDL to the database.
// All CREATE TABLE and CREATE INDEX statements use IF NOT EXISTS,
// so this is safe to call on every startup — existing data is never touched.
// After applying the schema, PRAGMA optimize is run once to ensure fresh
// query planner statistics on new databases (SQLite 3.18+).
func (d *DB) ApplySchema() error {
	if _, err := d.write.Exec(schemaSQL); err != nil {
		return fmt.Errorf("sqlitedb: apply schema: %w", err)
	}
	// Run PRAGMA optimize once at startup so the query planner has fresh
	// statistics immediately, rather than waiting for the hourly background job.
	if _, err := d.write.Exec("PRAGMA optimize"); err != nil {
		// Non-fatal: log but do not abort schema application.
		// Older SQLite builds (< 3.18) may not support this pragma.
		_ = err
	}
	return nil
}
