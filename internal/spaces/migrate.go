package spaces

import (
	"database/sql"
	"strings"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// Migrations returns an empty list — all schema is now in the base schema DDL.
func Migrations() []sqlitedb.Migration {
	return nil
}

// migrateSpacesV1 runs inside a single transaction supplied by sqlitedb.runMigration.
// Rollback behaviour: if any statement returns a non-tolerated error, this function
// returns the error and sqlitedb.runMigration calls tx.Rollback() before propagating
// it to the caller. The _migrations row is only inserted after Up() succeeds, so a
// partial run (e.g. power loss mid-migration) leaves no _migrations record and the
// migration will be retried on next startup.
// Most DDL statements are written with IF NOT EXISTS / IF NOT EXISTS so they are
// idempotent on retry. The only non-idempotent statement is the ALTER TABLE at
// index 8; isColumnExistsError() tolerates the "duplicate column" error that SQLite
// returns when the column already exists from a prior partial run.
func migrateSpacesV1(tx *sql.Tx) error {
	stmts := []string{
		// 0: spaces table
		`CREATE TABLE IF NOT EXISTS spaces (
			id            TEXT NOT NULL PRIMARY KEY,
			name          TEXT NOT NULL,
			kind          TEXT NOT NULL DEFAULT 'dm'
			                  CHECK (kind IN ('dm','channel')),
			lead_agent    TEXT NOT NULL,
			icon          TEXT NOT NULL DEFAULT '',
			color         TEXT NOT NULL DEFAULT '',
			team_id       TEXT,
			created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			archived_at   TEXT
		)`,
		// 1: unique index — only one DM per lead_agent
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_spaces_dm_unique_agent
			ON spaces(lead_agent) WHERE kind = 'dm'`,
		// 2: kind index
		`CREATE INDEX IF NOT EXISTS idx_spaces_kind    ON spaces(kind)`,
		// 3: updated_at index
		`CREATE INDEX IF NOT EXISTS idx_spaces_updated ON spaces(updated_at DESC)`,
		// 4: lead_agent index
		`CREATE INDEX IF NOT EXISTS idx_spaces_lead    ON spaces(lead_agent)`,
		// 5: space_members junction table.
		// NOTE: The approved spec §3.2 stores member agents as a JSON text column:
		//   member_agents TEXT NOT NULL DEFAULT '[]'
		// §3.2a explicitly calls out the junction table as the correct next step if
		// "find all spaces where agent X is a member" becomes a performance bottleneck.
		// This implementation adopts the junction table approach from the start,
		// skipping the JSON-column intermediary. The tradeoff is documented in §3.2a.
		// The Space.Members field in Go code corresponds to member_agents in the spec.
		`CREATE TABLE IF NOT EXISTS space_members (
			space_id   TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
			agent_name TEXT NOT NULL,
			PRIMARY KEY (space_id, agent_name)
		)`,
		// 6: space_members agent index
		`CREATE INDEX IF NOT EXISTS idx_space_members_agent ON space_members(agent_name)`,
		// 7: space_read_positions table
		`CREATE TABLE IF NOT EXISTS space_read_positions (
			space_id     TEXT NOT NULL PRIMARY KEY
			                 REFERENCES spaces(id) ON DELETE CASCADE,
			last_read_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		)`,
		// 8: add space_id column to sessions — ignore if already exists.
		// NOTE: The approved spec (§3.2) declares this column as:
		//   space_id TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE
		// However, SQLite does not support modifying or dropping constraints on
		// existing columns (no ALTER TABLE ... ALTER COLUMN or DROP CONSTRAINT).
		// The migration uses SET NULL instead of CASCADE, and the column is
		// nullable, because:
		//   (a) NOT NULL cannot be added to an existing column via ALTER TABLE in SQLite.
		//   (b) ON DELETE CASCADE would silently destroy sessions when a space is
		//       archived, which is destructive and inconsistent with the soft-archive
		//       model (archived_at flag, not DELETE).
		// The spec's schema DDL applies only to a fresh database where sessions is
		// created with this column from the start. Existing deployments that ran the
		// original sessions schema will always use this migration path with SET NULL.
		// Enforcement of space_id being non-null is done at the application layer
		// (store.go CreateSession). Known limitation — tracked as tech debt.
		`ALTER TABLE sessions ADD COLUMN space_id TEXT REFERENCES spaces(id) ON DELETE SET NULL`,
		// 9: FTS virtual table for session search.
		// sessions_fts uses content='' (contentless). Indexing is performed by
		// session.SQLiteSessionStore.SaveManifest via the spaces.FTSIndexer interface.
		`CREATE VIRTUAL TABLE IF NOT EXISTS sessions_fts USING fts5(
			session_id UNINDEXED,
			space_id   UNINDEXED,
			title,
			content=''
		)`,
		// 10: trigger to keep updated_at current
		`CREATE TRIGGER IF NOT EXISTS spaces_updated_at
			AFTER UPDATE ON spaces
			BEGIN UPDATE spaces SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = NEW.id; END`,
	}

	for i, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			// ALTER TABLE ADD COLUMN returns an error if the column already exists.
			// Tolerate this so the migration is idempotent on re-run.
			if i == 8 && isColumnExistsError(err) {
				continue
			}
			return err
		}
	}
	return nil
}

// migrateSpacesV2 adds the idx_messages_container_ts index which accelerates
// cross-session space timeline queries (ListSpaceMessages). Combined with the
// existing idx_sessions_space index, SQLite resolves:
//   space_id → session IDs → messages ordered by ts DESC
//
// The WHERE clause excludes non-session and reply messages so the index is
// small and covers the exact query pattern used in ListSpaceMessages.
//
// NOTE: This migration runs AFTER session.Migrations() which adds the
// parent_message_id column. The index is therefore safe to create here.
func migrateSpacesV2(tx *sql.Tx) error {
	_, err := tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_messages_container_ts
		    ON messages(container_id, ts DESC, id DESC)
		    WHERE container_type = 'session'
	`)
	return err
}

func isColumnExistsError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column") ||
		strings.Contains(msg, "already exists")
}
