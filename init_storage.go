package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	agentslib "github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/artifact"
	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/sqlitedb"
	"github.com/scrypster/huginn/internal/storage"
)

// storageResult holds all opened storage handles for use by subsequent init steps.
type storageResult struct {
	Store         *storage.Store             // Pebble key-value store (may be nil on error)
	SQLDB         *sqlitedb.DB               // SQLite structured database (may be nil on error)
	MemStore      agentslib.MemoryStoreIface  // agent memory store (may be nil)
	ArtifactStore *artifact.SQLiteStore      // artifact store (may be nil if SQLite unavailable)
}

// initStorage opens the Pebble store, SQLite database, runs schema migrations,
// and returns all storage handles. Failures are non-fatal and result in nil handles.
func initStorage(huginnHome, repoRoot string) storageResult {
	var res storageResult

	// --- Pebble store ---
	dir := storeDir(repoRoot)
	store, err := storage.Open(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "huginn: warning: store unavailable: %v\n", err)
	} else {
		res.Store = store
		slog.Info("storage: pebble store opened", "dir", dir)
	}

	// --- SQLite database ---
	dbPath := filepath.Join(huginnHome, "huginn.db")
	sqlDB, err := sqlitedb.Open(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "huginn: warning: sqlite unavailable: %v\n", err)
	} else {
		if err := sqlDB.ApplySchema(); err != nil {
			fmt.Fprintf(os.Stderr, "huginn: warning: sqlite schema failed: %v\n", err)
			sqlDB = nil
		}
		if sqlDB != nil {
			res.SQLDB = sqlDB
			slog.Info("storage: sqlite opened", "path", dbPath)
		}
	}

	// --- Artifact store ---
	if res.SQLDB != nil {
		artifactsDir := filepath.Join(huginnHome, "artifacts")
		if err := os.MkdirAll(artifactsDir, 0o700); err != nil {
			fmt.Fprintf(os.Stderr, "huginn: warning: cannot create artifacts dir: %v\n", err)
		} else {
			res.ArtifactStore = artifact.NewStore(res.SQLDB.Write(), artifactsDir)
			slog.Info("storage: artifact store opened", "dir", artifactsDir)
		}
	}

	// --- Agent memory store ---
	// Prefer SQLite; migrate from Pebble if both are available.
	if res.SQLDB != nil && res.Store != nil {
		if err := agentslib.MigrateAgentMemoryFromPebble(context.Background(), res.Store, res.SQLDB, relay.GetMachineID()); err != nil {
			slog.Info("agents: memory migration warning", "err", err)
		}
		res.MemStore = agentslib.NewSQLiteMemoryStore(res.SQLDB.Write(), relay.GetMachineID())
	} else if res.Store != nil {
		res.MemStore = agentslib.NewMemoryStore(res.Store, relay.GetMachineID())
	} else if res.SQLDB != nil {
		res.MemStore = agentslib.NewSQLiteMemoryStore(res.SQLDB.Write(), relay.GetMachineID())
	} else {
		slog.Warn("storage: both Pebble and SQLite unavailable — agent memory will not persist across sessions")
	}

	return res
}
