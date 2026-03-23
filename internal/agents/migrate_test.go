package agents_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/sqlitedb"
	"github.com/scrypster/huginn/internal/storage"
)

func openMigrateTestPebble(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func openMigrateTestSQLiteDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// writePebbleSummary manually writes a summary to Pebble using the expected key format.
func writePebbleSummary(t *testing.T, s *storage.Store, sum agents.SessionSummary) {
	t.Helper()
	key := fmt.Sprintf("agent:summary:%s:%s:%s", sum.MachineID, sum.AgentName, sum.SessionID)
	val, err := json.Marshal(sum)
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	if err := s.DB().Set([]byte(key), val, nil); err != nil {
		t.Fatalf("pebble Set: %v", err)
	}
}

// writePebbleDelegation manually writes a delegation to Pebble using the expected key format.
func writePebbleDelegation(t *testing.T, s *storage.Store, machineID string, entry agents.DelegationEntry) {
	t.Helper()
	key := fmt.Sprintf("agent:delegation:%s:%s:%s:%020d", machineID, entry.From, entry.To, entry.Timestamp.UnixNano())
	val, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal delegation: %v", err)
	}
	if err := s.DB().Set([]byte(key), val, nil); err != nil {
		t.Fatalf("pebble Set: %v", err)
	}
}

func TestMigrateAgentMemory_SummariesMigrated(t *testing.T) {
	t.Parallel()
	pebble := openMigrateTestPebble(t)
	sqlDB := openMigrateTestSQLiteDB(t)

	machineID := "machine-1"
	summary := agents.SessionSummary{
		SessionID:     "sess-001",
		MachineID:     machineID,
		AgentName:     "Mark",
		Timestamp:     time.Now().UTC(),
		Summary:       "did some work",
		FilesTouched:  []string{"a.go", "b.go"},
		Decisions:     []string{"decision1"},
		OpenQuestions: []string{"question1"},
	}
	writePebbleSummary(t, pebble, summary)

	if err := agents.MigrateAgentMemoryFromPebble(context.Background(), pebble, sqlDB, machineID); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	store := agents.NewSQLiteMemoryStore(sqlDB.Write(), machineID)
	results, err := store.LoadRecentSummaries(context.Background(), "Mark", 10)
	if err != nil {
		t.Fatalf("LoadRecentSummaries: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 summary, got %d", len(results))
	}
	if results[0].SessionID != "sess-001" {
		t.Errorf("SessionID: want sess-001, got %s", results[0].SessionID)
	}
	if results[0].Summary != "did some work" {
		t.Errorf("Summary: want 'did some work', got %s", results[0].Summary)
	}
}

func TestMigrateAgentMemory_DelegationsMigrated(t *testing.T) {
	t.Parallel()
	pebble := openMigrateTestPebble(t)
	sqlDB := openMigrateTestSQLiteDB(t)

	machineID := "machine-1"
	ts := time.Now().UTC()
	entry := agents.DelegationEntry{
		From:      "Mark",
		To:        "Chris",
		Question:  "How do I write tests?",
		Answer:    "Use table-driven tests",
		Timestamp: ts,
	}
	writePebbleDelegation(t, pebble, machineID, entry)

	if err := agents.MigrateAgentMemoryFromPebble(context.Background(), pebble, sqlDB, machineID); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	store := agents.NewSQLiteMemoryStore(sqlDB.Write(), machineID)
	results, err := store.LoadRecentDelegations(context.Background(), "Mark", "Chris", 10)
	if err != nil {
		t.Fatalf("LoadRecentDelegations: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 delegation, got %d", len(results))
	}
	if results[0].Question != "How do I write tests?" {
		t.Errorf("Question: want 'How do I write tests?', got %s", results[0].Question)
	}
	if results[0].Answer != "Use table-driven tests" {
		t.Errorf("Answer: want 'Use table-driven tests', got %s", results[0].Answer)
	}
}

func TestMigrateAgentMemory_Idempotent(t *testing.T) {
	t.Parallel()
	pebble := openMigrateTestPebble(t)
	sqlDB := openMigrateTestSQLiteDB(t)

	machineID := "machine-1"
	summary := agents.SessionSummary{
		SessionID: "sess-idem",
		MachineID: machineID,
		AgentName: "Mark",
		Timestamp: time.Now().UTC(),
		Summary:   "idempotent test",
	}
	writePebbleSummary(t, pebble, summary)

	// First migration
	if err := agents.MigrateAgentMemoryFromPebble(context.Background(), pebble, sqlDB, machineID); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	// Second migration (should be no-op)
	if err := agents.MigrateAgentMemoryFromPebble(context.Background(), pebble, sqlDB, machineID); err != nil {
		t.Fatalf("second migrate (idempotent): %v", err)
	}

	store := agents.NewSQLiteMemoryStore(sqlDB.Write(), machineID)
	results, err := store.LoadRecentSummaries(context.Background(), "Mark", 10)
	if err != nil {
		t.Fatalf("LoadRecentSummaries: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("idempotent: want 1 summary, got %d", len(results))
	}
}

func TestMigrateAgentMemory_RecordsMigrations(t *testing.T) {
	t.Parallel()
	pebble := openMigrateTestPebble(t)
	sqlDB := openMigrateTestSQLiteDB(t)

	if err := agents.MigrateAgentMemoryFromPebble(context.Background(), pebble, sqlDB, "machine-1"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var count int
	err := sqlDB.Read().QueryRow(`SELECT COUNT(*) FROM _migrations WHERE name IN ('M2_agent_summaries', 'M2_agent_delegations')`).Scan(&count)
	if err != nil {
		t.Fatalf("query migrations: %v", err)
	}
	if count != 2 {
		t.Errorf("_migrations count = %d, want 2", count)
	}
}

func TestMigrateAgentMemory_DeletesPebbleKeys(t *testing.T) {
	t.Parallel()
	pebble := openMigrateTestPebble(t)
	sqlDB := openMigrateTestSQLiteDB(t)

	machineID := "machine-1"
	summary := agents.SessionSummary{
		SessionID: "sess-del",
		MachineID: machineID,
		AgentName: "Mark",
		Timestamp: time.Now().UTC(),
		Summary:   "test",
	}
	writePebbleSummary(t, pebble, summary)

	if err := agents.MigrateAgentMemoryFromPebble(context.Background(), pebble, sqlDB, machineID); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Verify Pebble key is gone
	key := fmt.Sprintf("agent:summary:%s:Mark:sess-del", machineID)
	_, closer, err := pebble.DB().Get([]byte(key))
	if err == nil {
		closer.Close()
		t.Error("Pebble key still exists after migration")
	}
}

func TestMigrateAgentMemory_MultipleSummariesAndDelegations(t *testing.T) {
	t.Parallel()
	pebble := openMigrateTestPebble(t)
	sqlDB := openMigrateTestSQLiteDB(t)

	machineID := "machine-1"
	baseTime := time.Now().UTC()

	// Write multiple summaries
	for i := 0; i < 3; i++ {
		summary := agents.SessionSummary{
			SessionID:    fmt.Sprintf("sess-%d", i),
			MachineID:    machineID,
			AgentName:    "Mark",
			Timestamp:    baseTime.Add(time.Duration(i) * time.Second),
			Summary:      fmt.Sprintf("work %d", i),
			FilesTouched: []string{fmt.Sprintf("file%d.go", i)},
		}
		writePebbleSummary(t, pebble, summary)
	}

	// Write multiple delegations
	for i := 0; i < 2; i++ {
		entry := agents.DelegationEntry{
			From:      "Mark",
			To:        "Chris",
			Question:  fmt.Sprintf("question %d", i),
			Answer:    fmt.Sprintf("answer %d", i),
			Timestamp: baseTime.Add(time.Duration(i) * time.Second),
		}
		writePebbleDelegation(t, pebble, machineID, entry)
	}

	if err := agents.MigrateAgentMemoryFromPebble(context.Background(), pebble, sqlDB, machineID); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	store := agents.NewSQLiteMemoryStore(sqlDB.Write(), machineID)

	summaries, err := store.LoadRecentSummaries(context.Background(), "Mark", 10)
	if err != nil {
		t.Fatalf("LoadRecentSummaries: %v", err)
	}
	if len(summaries) != 3 {
		t.Errorf("summaries: want 3, got %d", len(summaries))
	}

	delegations, err := store.LoadRecentDelegations(context.Background(), "Mark", "Chris", 10)
	if err != nil {
		t.Fatalf("LoadRecentDelegations: %v", err)
	}
	if len(delegations) != 2 {
		t.Errorf("delegations: want 2, got %d", len(delegations))
	}
}
