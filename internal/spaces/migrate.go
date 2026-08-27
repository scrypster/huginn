package spaces

import (
	"database/sql"
	"strings"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// Migrations returns the spaces package DDL migrations. Each runs inside a
// transaction; the framework records applied migrations in _migrations so they
// execute exactly once per database file.
func Migrations() []sqlitedb.Migration {
	return []sqlitedb.Migration{
		{Name: "spaces_v1_initial_schema", Up: migrateSpacesV1},
		{Name: "spaces_v2_messages_container_ts_index", Up: migrateSpacesV2},
		{Name: "spaces_v3_channel_name_unique_index", Up: migrateSpacesV3},
		{Name: "spaces_v4_companies", Up: migrateSpacesV4},
		{Name: "spaces_v5_message_parent_id", Up: migrateSpacesV5},
		{Name: "spaces_v6_thread_reads", Up: migrateSpacesV6},
		{Name: "spaces_v7_company_unique_and_casefold", Up: migrateSpacesV7},
		{Name: "spaces_v8_channel_name_per_company", Up: migrateSpacesV8},
		{Name: "spaces_v9_channel_name_active_only", Up: migrateSpacesV9},
		{Name: "spaces_v10_company_lead", Up: migrateSpacesV10},
		{Name: "spaces_v11_space_for_you", Up: migrateSpacesV11},
	}
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
//
//	space_id → session IDs → messages ordered by ts DESC
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

// migrateSpacesV3 adds a case-insensitive unique index on channel names to
// eliminate the TOCTOU race in handleCreateSpace. The partial index applies
// only to rows where kind = 'channel'; DM spaces are unaffected.
func migrateSpacesV3(tx *sql.Tx) error {
	_, err := tx.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_spaces_channel_name_unique
		ON spaces(LOWER(name)) WHERE kind = 'channel'
	`)
	return err
}

// migrateSpacesV4 creates the companies isolation tables and attaches
// company_id on spaces. Desk-level spaces keep a NULL/empty company_id.
// Empty company.vault is allowed (no silent vault substitution).
func migrateSpacesV4(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS companies (
			id         TEXT NOT NULL PRIMARY KEY,
			name       TEXT NOT NULL,
			vault      TEXT NOT NULL DEFAULT '',
			icon       TEXT NOT NULL DEFAULT '',
			color      TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		)`,
		`CREATE TABLE IF NOT EXISTS company_members (
			company_id TEXT NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
			agent_name TEXT NOT NULL,
			PRIMARY KEY (company_id, agent_name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_company_members_agent ON company_members(agent_name)`,
		`CREATE INDEX IF NOT EXISTS idx_companies_name ON companies(name)`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return err
		}
	}
	// Existing databases already have spaces; tolerate duplicate-column on
	// a retry or a fresh schema that already included company_id.
	if _, err := tx.Exec(`ALTER TABLE spaces ADD COLUMN company_id TEXT REFERENCES companies(id) ON DELETE SET NULL`); err != nil {
		if !isColumnExistsError(err) {
			return err
		}
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_spaces_company ON spaces(company_id)`); err != nil {
		return err
	}
	return nil
}

// migrateSpacesV5 adds parent_id on messages for Slack-style space reply
// threads. Empty parent_id means the message is a channel/DM root.
// Distinct from parent_message_id (work-inspector / threadmgr).
func migrateSpacesV5(tx *sql.Tx) error {
	if _, err := tx.Exec(`ALTER TABLE messages ADD COLUMN parent_id TEXT NOT NULL DEFAULT ''`); err != nil {
		if !isColumnExistsError(err) {
			return err
		}
	}
	_, err := tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_messages_space_parent
		    ON messages(parent_id, ts ASC)
		    WHERE parent_id != ''
	`)
	return err
}

// migrateSpacesV6 persists last-seen Slack-style thread replies per viewer.
// Only participants (human posted root or a reply) get a row; spectators stay silent.
func migrateSpacesV6(tx *sql.Tx) error {
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS space_thread_reads (
			space_id     TEXT NOT NULL,
			parent_id    TEXT NOT NULL,
			viewer       TEXT NOT NULL DEFAULT 'local',
			last_read_ts TEXT NOT NULL,
			PRIMARY KEY (space_id, parent_id, viewer)
		)
	`); err != nil {
		return err
	}
	_, err := tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_space_thread_reads_parent
		    ON space_thread_reads(space_id, parent_id)
	`)
	return err
}

// migrateSpacesV7 locks company names case-insensitively (concurrent
// same-name create → unique) and seats case-fold (Winston/winston one row).
func migrateSpacesV7(tx *sql.Tx) error {
	if _, err := tx.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_companies_name_unique
		ON companies(LOWER(name))
	`); err != nil {
		return err
	}
	_, err := tx.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_company_members_lower
		ON company_members(company_id, LOWER(agent_name))
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

// migrateSpacesV8 scopes channel-name uniqueness per company (and desk).
// Two companies may both have "#eng". Desk names stay unique among desk.
func migrateSpacesV8(tx *sql.Tx) error {
	if _, err := tx.Exec(`DROP INDEX IF EXISTS idx_spaces_channel_name_unique`); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_spaces_channel_name_desk
		ON spaces(LOWER(name))
		WHERE kind = 'channel' AND COALESCE(company_id, '') = ''
	`); err != nil {
		return err
	}
	_, err := tx.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_spaces_channel_name_company
		ON spaces(company_id, LOWER(name))
		WHERE kind = 'channel' AND COALESCE(company_id, '') != ''
	`)
	return err
}

// migrateSpacesV9 limits channel-name uniqueness to active rows. Archived
// leftovers must not block company delete (ON DELETE SET NULL would otherwise
// collide two archived "#eng" channels on the desk unique index).
func migrateSpacesV9(tx *sql.Tx) error {
	for _, name := range []string{
		"idx_spaces_channel_name_desk",
		"idx_spaces_channel_name_company",
	} {
		if _, err := tx.Exec(`DROP INDEX IF EXISTS ` + name); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_spaces_channel_name_desk
		ON spaces(LOWER(name))
		WHERE kind = 'channel' AND archived_at IS NULL AND COALESCE(company_id, '') = ''
	`); err != nil {
		return err
	}
	_, err := tx.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_spaces_channel_name_company
		ON spaces(company_id, LOWER(name))
		WHERE kind = 'channel' AND archived_at IS NULL AND COALESCE(company_id, '') != ''
	`)
	return err
}

// migrateSpacesV10 adds optional company.lead (the company CoS). Empty means
// DefaultCompanyLead (Winston if seated, else first seated member).
func migrateSpacesV10(tx *sql.Tx) error {
	if _, err := tx.Exec(`ALTER TABLE companies ADD COLUMN lead TEXT NOT NULL DEFAULT ''`); err != nil {
		if !isColumnExistsError(err) {
			return err
		}
	}
	return nil
}

// migrateSpacesV11 persists follow/@me (space_reply_mention) per viewer so
// the spaces list can return for_you on the wire. Spectator unseen stays a
// count on the space/thread; this table is the rail badge only.
func migrateSpacesV11(tx *sql.Tx) error {
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS space_for_you (
			space_id TEXT NOT NULL,
			viewer   TEXT NOT NULL DEFAULT 'local',
			set_at   TEXT NOT NULL,
			PRIMARY KEY (space_id, viewer)
		)
	`); err != nil {
		return err
	}
	_, err := tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_space_for_you_space
		    ON space_for_you(space_id)
	`)
	return err
}
