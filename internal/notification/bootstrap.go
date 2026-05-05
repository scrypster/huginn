package notification

import (
	"fmt"

	"github.com/cockroachdb/pebble/v2"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

const (
	BackendPebble = "pebble"
	BackendSQLite = "sqlite"
)

// BootstrapOptions configures startup store selection.
type BootstrapOptions struct {
	Migration MigrationOptions
}

// BootstrapReport captures store-selection and migration results.
type BootstrapReport struct {
	Backend        string
	Migration      MigrationReport
	MigrationError string
}

// Bootstrap selects the runtime notification store and performs migration when
// SQLite is available. On migration error, Bootstrap safely falls back to
// Pebble for this process instead of switching to a partially-migrated view.
func Bootstrap(pdb *pebble.DB, sqlDB *sqlitedb.DB, opts BootstrapOptions) (StoreInterface, BootstrapReport, error) {
	if pdb == nil {
		return nil, BootstrapReport{}, fmt.Errorf("notification bootstrap: nil pebble db")
	}

	if sqlDB == nil {
		return NewStore(pdb), BootstrapReport{Backend: BackendPebble}, nil
	}

	// Preserve previous startup behavior: successful migrations clean source keys.
	// Callers can override only by invoking MigrateFromPebbleWithOptions directly.
	opts.Migration.DeleteSource = true

	migrationReport, migrationErr := MigrateFromPebbleWithOptions(pdb, sqlDB, opts.Migration)
	if migrationErr != nil {
		return NewStore(pdb), BootstrapReport{
			Backend:        BackendPebble,
			Migration:      migrationReport,
			MigrationError: migrationErr.Error(),
		}, nil
	}

	return NewSQLiteNotificationStore(sqlDB), BootstrapReport{
		Backend:   BackendSQLite,
		Migration: migrationReport,
	}, nil
}
