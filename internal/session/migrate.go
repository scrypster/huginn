// internal/session/migrate.go
package session

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

const (
	sessMigrationName    = "M5_sessions"
	sessMigrationPrefix  = "M5_session:"
	sessMigrateBatchSize = 1000
)

// MigrateFromFilesystem migrates all sessions from the filesystem into SQLite.
// Idempotent: returns nil immediately if M5_sessions is already recorded.
// Per-session idempotency: each session tracked as M5_session:{id}.
func MigrateFromFilesystem(baseDir string, db *sqlitedb.DB) error {
	done, err := sessMigDone(db.Read(), sessMigrationName)
	if err != nil {
		return fmt.Errorf("sessions migrate: check: %w", err)
	}
	if done {
		return nil
	}

	if _, statErr := os.Stat(baseDir); errors.Is(statErr, os.ErrNotExist) {
		return recordSessMig(db.Write(), sessMigrationName, 0, baseDir)
	}

	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return fmt.Errorf("sessions migrate: readdir: %w", err)
	}

	var sessionDirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if validateID(e.Name()) != nil {
			continue
		}
		sessionDirs = append(sessionDirs, e.Name())
	}

	store := NewSQLiteSessionStore(db)
	total := len(sessionDirs)

	for i, sessID := range sessionDirs {
		key := sessMigrationPrefix + sessID
		done, err := sessMigDone(db.Read(), key)
		if err != nil {
			return fmt.Errorf("sessions migrate: check session %s: %w", sessID, err)
		}
		if done {
			if i%10 == 0 {
				slog.Info("[migration] sessions: skipping session", "count", fmt.Sprintf("%d/%d", i+1, total), "session_id", sessID)
			}
			continue
		}

		if i%10 == 0 {
			slog.Info("[migration] sessions: migrating session", "count", fmt.Sprintf("%d/%d", i+1, total), "session_id", sessID)
		}

		if err := migrateSingleSession(store, db, baseDir, sessID); err != nil {
			return fmt.Errorf("sessions migrate: session %s: %w", sessID, err)
		}

		if err := recordSessMig(db.Write(), key, 1, filepath.Join(baseDir, sessID)); err != nil {
			return fmt.Errorf("sessions migrate: record session %s: %w", sessID, err)
		}
	}

	if err := recordSessMig(db.Write(), sessMigrationName, total, baseDir); err != nil {
		return err
	}

	if err := os.Rename(baseDir, baseDir+".bak"); err != nil {
		slog.Error("sessions migrate: rename failed", "dir", baseDir, "err", err)
	}

	return nil
}

func migrateSingleSession(store *SQLiteSessionStore, db *sqlitedb.DB, baseDir, sessID string) error {
	sessDir := filepath.Join(baseDir, sessID)

	manifestPath := filepath.Join(sessDir, "manifest.json")
	mData, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}

	var ps PersistentSession
	if err := json.Unmarshal(mData, &ps); err != nil {
		return fmt.Errorf("unmarshal manifest: %w", err)
	}

	// PersistentSession.ID has json tag "session_id". Some older manifests
	// may have only "id" set. Fall back to the directory name.
	if ps.ID == "" {
		ps.ID = sessID
	}
	if ps.Status == "" {
		ps.Status = "active"
	}
	if ps.Version == 0 {
		ps.Version = 1
	}

	if err := store.Create(&ps); err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	msgsPath := filepath.Join(sessDir, "messages.jsonl")
	repairJSONL(msgsPath)
	if _, err := migrateMessagesJSONL(msgsPath, sessID, "session", db.Write()); err != nil {
		return fmt.Errorf("migrate messages: %w", err)
	}

	threadFiles, err := filepath.Glob(filepath.Join(sessDir, "thread-*.jsonl"))
	if err != nil {
		return fmt.Errorf("glob threads: %w", err)
	}

	for _, threadPath := range threadFiles {
		base := filepath.Base(threadPath)
		legacyTID := strings.TrimSuffix(strings.TrimPrefix(base, "thread-"), ".jsonl")
		newTID := NewID()

		if _, err := db.Write().Exec(`
			INSERT OR IGNORE INTO threads
				(id, parent_type, parent_id, status, created_at,
				 files_modified, key_decisions, artifacts)
			VALUES (?, 'session', ?, 'done', ?, '[]', '[]', '[]')`,
			newTID, sessID,
			time.Now().UTC().Format(time.RFC3339),
		); err != nil {
			return fmt.Errorf("insert thread %s→%s: %w", legacyTID, newTID, err)
		}

		repairJSONL(threadPath)
		if _, err := migrateMessagesJSONL(threadPath, newTID, "thread", db.Write()); err != nil {
			return fmt.Errorf("migrate thread %s: %w", legacyTID, err)
		}
	}

	return nil
}

func migrateMessagesJSONL(path, containerID, containerType string, writeDB *sql.DB) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	type rawMsg struct {
		ID         string  `json:"id"`
		Ts         string  `json:"ts"`
		Seq        int64   `json:"seq"`
		Role       string  `json:"role"`
		Content    string  `json:"content"`
		Agent      string  `json:"agent"`
		ToolName   string  `json:"tool_name"`
		ToolCallID string  `json:"tool_call_id"`
		Type       string  `json:"type"`
		PromptTok  int     `json:"prompt_tokens"`
		CompTok    int     `json:"completion_tokens"`
		CostUSD    float64 `json:"cost_usd"`
		Model      string  `json:"model"`
	}

	var batch []rawMsg
	total := 0

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		tx, err := writeDB.Begin()
		if err != nil {
			return fmt.Errorf("begin tx: %w", err)
		}
		stmt, err := tx.Prepare(`
			INSERT OR IGNORE INTO messages
				(id, container_type, container_id, seq, ts, role, content,
				 agent, tool_name, tool_call_id, type,
				 prompt_tokens, completion_tokens, cost_usd, model)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			tx.Rollback()
			return err
		}
		defer stmt.Close()

		for _, m := range batch {
			id := m.ID
			if id == "" {
				id = NewID()
			}
			ts := m.Ts
			if ts == "" {
				ts = time.Now().UTC().Format(time.RFC3339)
			}
			role := m.Role
			if role == "" {
				role = "user"
			}
			if _, err := stmt.Exec(
				id, containerType, containerID, m.Seq, ts, role, m.Content,
				m.Agent, m.ToolName, m.ToolCallID, m.Type,
				m.PromptTok, m.CompTok, m.CostUSD, m.Model,
			); err != nil {
				tx.Rollback()
				return fmt.Errorf("insert msg %s: %w", id, err)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}
		total += len(batch)
		batch = batch[:0]
		return nil
	}

	seq := int64(0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var m rawMsg
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		if m.Seq == 0 {
			seq++
			m.Seq = seq
		}
		batch = append(batch, m)
		if len(batch) >= sessMigrateBatchSize {
			if err := flush(); err != nil {
				return total, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return total, err
	}
	return total, flush()
}

func sessMigDone(readDB *sql.DB, name string) (bool, error) {
	var count int
	err := readDB.QueryRow(`SELECT COUNT(*) FROM _migrations WHERE name = ?`, name).Scan(&count)
	return count > 0, err
}

func recordSessMig(writeDB *sql.DB, name string, count int, sourcePath string) error {
	_, err := writeDB.Exec(
		`INSERT OR IGNORE INTO _migrations (name, record_count, source_path) VALUES (?, ?, ?)`,
		name, count, sourcePath,
	)
	return err
}
