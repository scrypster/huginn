package agent

import (
	"context"
	"database/sql"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

// sqliteReplicationQueuer wraps *sqlitedb.DB to implement ReplicationQueuer.
type sqliteReplicationQueuer struct {
	db *sqlitedb.DB
}

// NewSQLiteReplicationQueuer wraps a *sqlitedb.DB for use with MemoryReplicator.
func NewSQLiteReplicationQueuer(db *sqlitedb.DB) ReplicationQueuer {
	return &sqliteReplicationQueuer{db: db}
}

func (q *sqliteReplicationQueuer) ReadQ() ReplicationDBReader  { return &sqliteDBReader{q.db.Read()} }
func (q *sqliteReplicationQueuer) WriteQ() ReplicationDBWriter { return &sqliteDBWriter{q.db.Write()} }

// sqliteDBReader adapts *sql.DB to ReplicationDBReader.
type sqliteDBReader struct{ db *sql.DB }

func (r *sqliteDBReader) QueryContext(ctx context.Context, query string, args ...any) (ReplicationRows, error) {
	return r.db.QueryContext(ctx, query, args...)
}

func (r *sqliteDBReader) QueryRowContext(ctx context.Context, query string, args ...any) ReplicationRow {
	return r.db.QueryRowContext(ctx, query, args...)
}

// sqliteDBWriter adapts *sql.DB to ReplicationDBWriter.
type sqliteDBWriter struct{ db *sql.DB }

func (w *sqliteDBWriter) ExecContext(ctx context.Context, query string, args ...any) (ReplicationResult, error) {
	return w.db.ExecContext(ctx, query, args...)
}
