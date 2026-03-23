package sqlitedb

import (
	"database/sql"
	"fmt"
	"log/slog"
)

// Migration is a named, transactional schema or data migration.
// Up receives an open transaction; return an error to roll back.
type Migration struct {
	Name string
	Up   func(tx *sql.Tx) error
}

// Migrate runs any pending migrations from the provided list.
// Each migration runs inside a transaction. On success, a row is inserted
// into _migrations. On failure, the transaction is rolled back and the error
// is returned — subsequent migrations in the list are not attempted.
//
// Call ApplySchema before Migrate to ensure _migrations exists.
func (d *DB) Migrate(migrations []Migration) error {
	for _, m := range migrations {
		done, err := d.migrationDone(m.Name)
		if err != nil {
			return fmt.Errorf("sqlitedb: migrate %q: check: %w", m.Name, err)
		}
		if done {
			slog.Debug("sqlitedb: migration already applied", "name", m.Name)
			continue
		}

		slog.Info("sqlitedb: applying migration", "name", m.Name)
		if err := d.runMigration(m); err != nil {
			return fmt.Errorf("sqlitedb: migrate %q: %w", m.Name, err)
		}
		slog.Info("sqlitedb: migration applied", "name", m.Name)
	}
	return nil
}

func (d *DB) migrationDone(name string) (bool, error) {
	var count int
	err := d.read.QueryRow(
		`SELECT COUNT(*) FROM _migrations WHERE name = ?`, name,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (d *DB) runMigration(m Migration) error {
	tx, err := d.write.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	if err := m.Up(tx); err != nil {
		tx.Rollback()
		return err
	}

	if _, err := tx.Exec(
		`INSERT INTO _migrations (name, record_count, source_path) VALUES (?, 0, '')`,
		m.Name,
	); err != nil {
		tx.Rollback()
		return fmt.Errorf("record migration: %w", err)
	}

	return tx.Commit()
}
