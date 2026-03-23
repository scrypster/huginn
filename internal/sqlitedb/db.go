// Package sqlitedb provides the SQLite database connection for huginn.
// It opens two *sql.DB pools against the same WAL-mode database file:
// one serialized write connection and one read pool.
package sqlitedb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	_ "modernc.org/sqlite" // registers "sqlite" driver
)

// pragmas are applied to every new connection on open.
// They are NOT stored in the database file — must be set per-connection.
var pragmas = []string{
	"PRAGMA journal_mode = WAL",
	"PRAGMA synchronous = NORMAL",
	"PRAGMA foreign_keys = ON",
	"PRAGMA busy_timeout = 5000",
	"PRAGMA cache_size = -8000",
	"PRAGMA mmap_size = 134217728",
	"PRAGMA temp_store = MEMORY",
	"PRAGMA wal_autocheckpoint = 1000",
}

// DB wraps a write connection (MaxOpenConns=1) and a read pool (MaxOpenConns=4)
// against the same SQLite WAL-mode database file.
type DB struct {
	write  *sql.DB
	read   *sql.DB
	path   string
	closed bool
	mu     sync.Mutex
}

// Open opens (or creates) a SQLite database at path, applies WAL pragmas,
// and returns a DB ready for use. Call Close when done.
func Open(path string) (*DB, error) {
	write, err := openConn(path, 1)
	if err != nil {
		return nil, fmt.Errorf("sqlitedb: open write connection: %w", err)
	}

	read, err := openConn(path, 4)
	if err != nil {
		write.Close()
		return nil, fmt.Errorf("sqlitedb: open read connection: %w", err)
	}

	return &DB{write: write, read: read, path: path}, nil
}

// openConn opens a *sql.DB with pragmas applied and the given max-open-conns.
func openConn(path string, maxConns int) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(maxConns)
	db.SetConnMaxLifetime(0)

	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}
	return db, nil
}

// Write returns the serialized write connection, or nil if the DB is closed.
// Use for INSERT, UPDATE, DELETE, CREATE, DROP.
func (d *DB) Write() *sql.DB {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	return d.write
}

// Read returns the read connection pool, or nil if the DB is closed.
// Use for SELECT queries.
func (d *DB) Read() *sql.DB {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	return d.read
}

// Path returns the filesystem path to the database file.
func (d *DB) Path() string { return d.path }

// Close closes both connections. Safe to call multiple times.
func (d *DB) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil
	}
	d.closed = true
	var errs []error
	if err := d.write.Close(); err != nil {
		errs = append(errs, fmt.Errorf("write: %w", err))
	}
	if err := d.read.Close(); err != nil {
		errs = append(errs, fmt.Errorf("read: %w", err))
	}
	if len(errs) > 0 {
		return fmt.Errorf("sqlitedb: close: %w", errors.Join(errs...))
	}
	return nil
}

// StartWALCheckpoint launches a background goroutine that runs
// "PRAGMA wal_checkpoint(TRUNCATE)" every 5 minutes on the write connection
// until ctx is cancelled. This prevents WAL files from growing unboundedly.
// Additionally, "PRAGMA optimize" is run every hour to keep query planner
// statistics fresh without a full ANALYZE scan.
// Safe to call multiple times — each call launches an independent goroutine.
func (d *DB) StartWALCheckpoint(ctx context.Context) {
	go func() {
		const checkpointInterval = 5 * time.Minute
		const optimizeInterval = time.Hour
		ticker := time.NewTicker(checkpointInterval)
		defer ticker.Stop()
		lastOptimize := time.Now()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := d.Write().ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
					slog.Warn("sqlitedb: WAL checkpoint failed", "err", err)
				}
				// Every hour, run PRAGMA optimize to refresh query statistics.
				if time.Since(lastOptimize) > optimizeInterval {
					if _, err := d.Write().ExecContext(ctx, "PRAGMA optimize"); err != nil {
						slog.Warn("sqlitedb: PRAGMA optimize failed", "err", err)
					}
					lastOptimize = time.Now()
				}
			}
		}
	}()
}

// RunOptimize runs "PRAGMA optimize" on the write connection.
// This is a lightweight alternative to ANALYZE (available in SQLite 3.18+)
// that updates query planner statistics only for tables that have changed
// significantly. Safe to call at startup to ensure fresh stats on new databases.
func (d *DB) RunOptimize() error {
	d.mu.Lock()
	closed := d.closed
	d.mu.Unlock()
	if closed {
		return fmt.Errorf("sqlitedb: RunOptimize: database is closed")
	}
	if _, err := d.write.Exec("PRAGMA optimize"); err != nil {
		return fmt.Errorf("sqlitedb: PRAGMA optimize: %w", err)
	}
	return nil
}
