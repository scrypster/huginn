// Package turnmetrics persists per-agent-turn latency telemetry so Huginn can
// see where turn time goes and prove future performance changes moved real
// numbers (not vibes). It is intentionally decoupled from internal/agent so
// the hot RunLoop path only ever touches a bounded channel — see Writer.
package turnmetrics

import (
	"database/sql"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// Migrations returns the incremental schema migrations for the turn_metrics
// table. Call sqlDB.Migrate(turnmetrics.Migrations()) once during startup,
// after ApplySchema, the same way threadmgr/session/notification do.
func Migrations() []sqlitedb.Migration {
	return []sqlitedb.Migration{
		{Name: "turn_metrics_v1", Up: migrateTurnMetricsV1},
	}
}

// migrateTurnMetricsV1 creates the turn_metrics table. Rows are pruned by the
// async Writer to a bounded retention (see Writer.retention), so no row ever
// needs a foreign key or cascade — this table is a rolling telemetry window,
// not a system of record.
func migrateTurnMetricsV1(tx *sql.Tx) error {
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS turn_metrics (
		    id                INTEGER PRIMARY KEY AUTOINCREMENT,
		    session_id        TEXT    NOT NULL DEFAULT '',
		    agent_name        TEXT    NOT NULL DEFAULT '',
		    model             TEXT    NOT NULL DEFAULT '',
		    provider          TEXT    NOT NULL DEFAULT '',
		    turn_kind         TEXT    NOT NULL DEFAULT '',
		    prompt_chars      INTEGER NOT NULL DEFAULT 0,
		    message_count     INTEGER NOT NULL DEFAULT 0,
		    tool_call_count   INTEGER NOT NULL DEFAULT 0,
		    had_first_token   INTEGER NOT NULL DEFAULT 0,
		    first_token_ms    INTEGER NOT NULL DEFAULT -1,
		    first_signal_ms   INTEGER NOT NULL DEFAULT -1,
		    complete_ms       INTEGER NOT NULL DEFAULT 0,
		    is_error          INTEGER NOT NULL DEFAULT 0,
		    t_request_unix_ms INTEGER NOT NULL DEFAULT 0,
		    created_at        INTEGER NOT NULL DEFAULT (unixepoch())
		)`); err != nil {
		return err
	}
	for _, ddl := range []string{
		`CREATE INDEX IF NOT EXISTS idx_turn_metrics_created ON turn_metrics (created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_turn_metrics_model ON turn_metrics (model, created_at DESC)`,
	} {
		if _, err := tx.Exec(ddl); err != nil {
			return err
		}
	}
	return nil
}
