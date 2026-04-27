package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/sqlitedb"
	"github.com/scrypster/huginn/internal/storage"
	"gopkg.in/yaml.v3"
)

// MigrateAgentMemoryFromPebble migrates agent summaries and delegations
// from Pebble to SQLite. Idempotent: skips if already recorded in _migrations.
// After successful migration, deletes all "agent:" Pebble keys.
func MigrateAgentMemoryFromPebble(ctx context.Context, store *storage.Store, db *sqlitedb.DB, machineID string) error {
	summariesDone, err := agentMigrationDone(db.Read(), "M2_agent_summaries")
	if err != nil {
		return fmt.Errorf("agents migrate: check summaries: %w", err)
	}
	delegationsDone, err := agentMigrationDone(db.Read(), "M2_agent_delegations")
	if err != nil {
		return fmt.Errorf("agents migrate: check delegations: %w", err)
	}
	if summariesDone && delegationsDone {
		slog.Debug("agents migrate: already complete")
		return nil
	}

	pdb := store.DB()

	var summaryCount, delegationCount int

	if !summariesDone {
		summaryCount, err = migrateSummaries(ctx, pdb, db.Write(), machineID)
		if err != nil {
			return fmt.Errorf("agents migrate summaries: %w", err)
		}
		slog.Info("agents migrate: summaries complete", "count", summaryCount)
	}

	if !delegationsDone {
		delegationCount, err = migrateDelegations(ctx, pdb, db.Write(), machineID)
		if err != nil {
			return fmt.Errorf("agents migrate delegations: %w", err)
		}
		slog.Info("agents migrate: delegations complete", "count", delegationCount)
	}

	// Delete all agent: keys from Pebble.
	if err := deleteAgentPebbleKeys(pdb); err != nil {
		slog.Warn("agents migrate: failed to delete pebble keys", "err", err)
		// Non-fatal: SQLite is the source of truth now.
	}

	return nil
}

func migrateSummaries(ctx context.Context, pdb *pebble.DB, sqlDB *sql.DB, machineID string) (int, error) {
	prefix := []byte("agent:summary:")
	upper := migrateIncrementBytes(prefix)

	iter, err := pdb.NewIter(&pebble.IterOptions{LowerBound: prefix, UpperBound: upper})
	if err != nil {
		return 0, fmt.Errorf("open iter: %w", err)
	}
	defer iter.Close()

	tx, err := sqlDB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		var sum SessionSummary
		if err := json.Unmarshal(iter.Value(), &sum); err != nil {
			slog.Warn("agents migrate: skip malformed summary", "key", string(iter.Key()), "err", err)
			continue
		}

		createdAt := sum.Timestamp.UTC().Format(time.RFC3339Nano)
		_, err := tx.Exec(`
			INSERT OR IGNORE INTO agent_summaries
				(id, machine_id, agent_name, session_id, summary, files_touched, decisions, open_questions, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			session.NewID(), sum.MachineID, sum.AgentName, sum.SessionID,
			sum.Summary, marshalStringSlice(sum.FilesTouched),
			marshalStringSlice(sum.Decisions), marshalStringSlice(sum.OpenQuestions),
			createdAt,
		)
		if err != nil {
			return 0, fmt.Errorf("insert summary: %w", err)
		}
		count++
	}
	if err := iter.Error(); err != nil {
		return 0, err
	}

	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO _migrations (name, record_count, source_path) VALUES (?, ?, 'pebble')`,
		"M2_agent_summaries", count,
	); err != nil {
		return 0, err
	}

	return count, tx.Commit()
}

func migrateDelegations(ctx context.Context, pdb *pebble.DB, sqlDB *sql.DB, machineID string) (int, error) {
	prefix := []byte("agent:delegation:")
	upper := migrateIncrementBytes(prefix)

	iter, err := pdb.NewIter(&pebble.IterOptions{LowerBound: prefix, UpperBound: upper})
	if err != nil {
		return 0, fmt.Errorf("open iter: %w", err)
	}
	defer iter.Close()

	tx, err := sqlDB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		var entry DelegationEntry
		if err := json.Unmarshal(iter.Value(), &entry); err != nil {
			slog.Warn("agents migrate: skip malformed delegation", "key", string(iter.Key()), "err", err)
			continue
		}

		createdAt := entry.Timestamp.UTC().Format(time.RFC3339Nano)
		_, err := tx.Exec(`
			INSERT OR IGNORE INTO agent_delegations
				(id, machine_id, from_agent, to_agent, question, answer, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			session.NewID(), machineID, entry.From, entry.To,
			entry.Question, entry.Answer, createdAt,
		)
		if err != nil {
			return 0, fmt.Errorf("insert delegation: %w", err)
		}
		count++
	}
	if err := iter.Error(); err != nil {
		return 0, err
	}

	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO _migrations (name, record_count, source_path) VALUES (?, ?, 'pebble')`,
		"M2_agent_delegations", count,
	); err != nil {
		return 0, err
	}

	return count, tx.Commit()
}

func deleteAgentPebbleKeys(pdb *pebble.DB) error {
	prefix := []byte("agent:")
	upper := migrateIncrementBytes(prefix)
	return pdb.DeleteRange(prefix, upper, pebble.Sync)
}

func agentMigrationDone(db *sql.DB, name string) (bool, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM _migrations WHERE name = ?`, name).Scan(&count)
	return count > 0, err
}

// migrateIncrementBytes returns a byte slice that is the smallest value greater
// than b for prefix range iteration.
func migrateIncrementBytes(b []byte) []byte {
	end := make([]byte, len(b))
	copy(end, b)
	for i := len(end) - 1; i >= 0; i-- {
		end[i]++
		if end[i] != 0 {
			return end
		}
	}
	return append(b, 0x00)
}

// MigrateEmptyToolbeltToWildcard backfills a wildcard toolbelt entry on agents
// whose toolbelt is nil or empty. This preserves "all connections allowed"
// behavior for agents created before the default-deny change.
// Safe to call multiple times (idempotent: skips agents that already have entries).
func MigrateEmptyToolbeltToWildcard(baseDir string) error {
	agentsDir := filepath.Join(baseDir, "agents")
	jsonEntries, _ := filepath.Glob(filepath.Join(agentsDir, "*.json"))
	yamlEntries, _ := filepath.Glob(filepath.Join(agentsDir, "*.yaml"))
	entries := append(jsonEntries, yamlEntries...)

	for _, path := range entries {
		if filepath.Base(path) == ".draft.json" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var def AgentDef
		unmarshalErr := error(nil)
		switch filepath.Ext(path) {
		case ".json":
			unmarshalErr = json.Unmarshal(data, &def)
		case ".yaml", ".yml":
			unmarshalErr = yaml.Unmarshal(data, &def)
		default:
			continue
		}
		if unmarshalErr != nil {
			continue
		}
		// Only migrate agents with no toolbelt entries.
		if len(def.Toolbelt) > 0 {
			continue
		}
		def.Toolbelt = []ToolbeltEntry{{
			ConnectionID: "*",
			Provider:     "*",
		}}
		if err := SaveAgent(baseDir, def); err != nil {
			return fmt.Errorf("migrate agent %s: %w", def.Name, err)
		}
	}
	return nil
}
