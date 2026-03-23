package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/scrypster/huginn/internal/session"
)

// SQLiteMemoryStore implements MemoryStoreIface backed by SQLite.
type SQLiteMemoryStore struct {
	db        *sql.DB // write connection
	machineID string
}

// NewSQLiteMemoryStore creates a new SQLiteMemoryStore.
func NewSQLiteMemoryStore(db *sql.DB, machineID string) *SQLiteMemoryStore {
	return &SQLiteMemoryStore{db: db, machineID: machineID}
}

// Compile-time assertion.
var _ MemoryStoreIface = (*SQLiteMemoryStore)(nil)

func (s *SQLiteMemoryStore) SaveSummary(_ context.Context, sum SessionSummary) error {
	filesJSON := marshalStringSlice(sum.FilesTouched)
	decisionsJSON := marshalStringSlice(sum.Decisions)
	questionsJSON := marshalStringSlice(sum.OpenQuestions)
	createdAt := sum.Timestamp.UTC().Format(time.RFC3339Nano)

	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO agent_summaries
			(id, machine_id, agent_name, session_id, summary, files_touched, decisions, open_questions, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.NewID(), s.machineID, sum.AgentName, sum.SessionID,
		sum.Summary, filesJSON, decisionsJSON, questionsJSON, createdAt,
	)
	return err
}

func (s *SQLiteMemoryStore) LoadRecentSummaries(_ context.Context, agentName string, limit int) ([]SessionSummary, error) {
	rows, err := s.db.Query(`
		SELECT agent_name, session_id, summary, files_touched, decisions, open_questions, created_at
		FROM agent_summaries
		WHERE machine_id = ? AND agent_name = ?
		ORDER BY created_at DESC
		LIMIT ?`,
		s.machineID, agentName, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SessionSummary
	for rows.Next() {
		var sum SessionSummary
		var filesJSON, decisionsJSON, questionsJSON, createdAtStr string
		if err := rows.Scan(&sum.AgentName, &sum.SessionID, &sum.Summary,
			&filesJSON, &decisionsJSON, &questionsJSON, &createdAtStr); err != nil {
			return nil, err
		}
		sum.MachineID = s.machineID
		sum.FilesTouched = unmarshalStringSlice(filesJSON)
		sum.Decisions = unmarshalStringSlice(decisionsJSON)
		sum.OpenQuestions = unmarshalStringSlice(questionsJSON)
		if t, err := time.Parse(time.RFC3339Nano, createdAtStr); err == nil {
			sum.Timestamp = t
		}
		results = append(results, sum)
	}
	return results, rows.Err()
}

func (s *SQLiteMemoryStore) AppendDelegation(_ context.Context, entry DelegationEntry) error {
	createdAt := entry.Timestamp.UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(`
		INSERT INTO agent_delegations
			(id, machine_id, from_agent, to_agent, question, answer, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		session.NewID(), s.machineID, entry.From, entry.To,
		entry.Question, entry.Answer, createdAt,
	)
	if err != nil {
		return err
	}
	return s.trimDelegations(entry.From, entry.To, 10)
}

func (s *SQLiteMemoryStore) LoadRecentDelegations(_ context.Context, from, to string, limit int) ([]DelegationEntry, error) {
	rows, err := s.db.Query(`
		SELECT from_agent, to_agent, question, answer, created_at
		FROM agent_delegations
		WHERE machine_id = ? AND from_agent = ? AND to_agent = ?
		ORDER BY created_at DESC
		LIMIT ?`,
		s.machineID, from, to, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []DelegationEntry
	for rows.Next() {
		var e DelegationEntry
		var createdAtStr string
		if err := rows.Scan(&e.From, &e.To, &e.Question, &e.Answer, &createdAtStr); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339Nano, createdAtStr); err == nil {
			e.Timestamp = t
		}
		results = append(results, e)
	}
	return results, rows.Err()
}

func (s *SQLiteMemoryStore) trimDelegations(from, to string, maxKeep int) error {
	_, err := s.db.Exec(`
		DELETE FROM agent_delegations
		WHERE machine_id = ? AND from_agent = ? AND to_agent = ?
		  AND id NOT IN (
			SELECT id FROM agent_delegations
			WHERE machine_id = ? AND from_agent = ? AND to_agent = ?
			ORDER BY created_at DESC
			LIMIT ?
		  )`,
		s.machineID, from, to,
		s.machineID, from, to, maxKeep,
	)
	return err
}

func marshalStringSlice(ss []string) string {
	if len(ss) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(ss)
	return string(b)
}

func unmarshalStringSlice(s string) []string {
	if s == "" || s == "[]" {
		return nil
	}
	var result []string
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		slog.Warn("memory_sqlite: unmarshal string slice", "err", err, "raw", s)
		return nil
	}
	return result
}
