package agents_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

func openAgentTestDB(t *testing.T) *sqlitedb.DB {
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

func TestSQLiteMemoryStore_SaveAndLoadSummary(t *testing.T) {
	t.Parallel()
	db := openAgentTestDB(t)
	s := agents.NewSQLiteMemoryStore(db.Write(), "machine-1")

	summary := agents.SessionSummary{
		SessionID: "sess-001",
		MachineID: "machine-1",
		AgentName: "Mark",
		Timestamp: time.Now().UTC(),
		Summary:   "Did some work",
		FilesTouched: []string{"a.go", "b.go"},
		Decisions:    []string{"chose X"},
	}
	if err := s.SaveSummary(context.Background(), summary); err != nil {
		t.Fatalf("SaveSummary: %v", err)
	}

	results, err := s.LoadRecentSummaries(context.Background(), "Mark", 10)
	if err != nil {
		t.Fatalf("LoadRecentSummaries: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 summary, got %d", len(results))
	}
	if results[0].SessionID != "sess-001" {
		t.Errorf("SessionID = %q", results[0].SessionID)
	}
	if len(results[0].FilesTouched) != 2 {
		t.Errorf("FilesTouched = %v", results[0].FilesTouched)
	}
}

func TestSQLiteMemoryStore_LoadLimit(t *testing.T) {
	t.Parallel()
	db := openAgentTestDB(t)
	s := agents.NewSQLiteMemoryStore(db.Write(), "machine-1")

	for i := 0; i < 5; i++ {
		s.SaveSummary(context.Background(), agents.SessionSummary{
			SessionID: fmt.Sprintf("sess-%d", i),
			MachineID: "machine-1",
			AgentName: "Mark",
			Timestamp: time.Now().UTC(),
			Summary:   "summary",
		})
	}

	results, err := s.LoadRecentSummaries(context.Background(), "Mark", 3)
	if err != nil {
		t.Fatalf("LoadRecentSummaries: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("want 3, got %d", len(results))
	}
}

func TestSQLiteMemoryStore_LoadDescendingOrder(t *testing.T) {
	t.Parallel()
	db := openAgentTestDB(t)
	s := agents.NewSQLiteMemoryStore(db.Write(), "machine-1")

	base := time.Now().UTC()
	summaries := []agents.SessionSummary{
		{SessionID: "old", MachineID: "machine-1", AgentName: "Mark", Timestamp: base.Add(-2 * time.Second), Summary: "old"},
		{SessionID: "new", MachineID: "machine-1", AgentName: "Mark", Timestamp: base, Summary: "new"},
	}
	for _, ss := range summaries {
		s.SaveSummary(context.Background(), ss)
	}

	results, _ := s.LoadRecentSummaries(context.Background(), "Mark", 10)
	if len(results) < 2 {
		t.Fatalf("want 2, got %d", len(results))
	}
	if results[0].SessionID != "new" {
		t.Errorf("want newest first, got %q", results[0].SessionID)
	}
}

func TestSQLiteMemoryStore_AgentIsolation(t *testing.T) {
	t.Parallel()
	db := openAgentTestDB(t)
	s := agents.NewSQLiteMemoryStore(db.Write(), "machine-1")

	s.SaveSummary(context.Background(), agents.SessionSummary{
		SessionID: "sess-mark", MachineID: "machine-1", AgentName: "Mark",
		Timestamp: time.Now().UTC(), Summary: "mark",
	})
	s.SaveSummary(context.Background(), agents.SessionSummary{
		SessionID: "sess-chris", MachineID: "machine-1", AgentName: "Chris",
		Timestamp: time.Now().UTC(), Summary: "chris",
	})

	markResults, _ := s.LoadRecentSummaries(context.Background(), "Mark", 10)
	if len(markResults) != 1 || markResults[0].AgentName != "Mark" {
		t.Errorf("agent isolation failed: %v", markResults)
	}
}

func TestSQLiteMemoryStore_DelegationRetention(t *testing.T) {
	t.Parallel()
	db := openAgentTestDB(t)
	s := agents.NewSQLiteMemoryStore(db.Write(), "machine-1")

	// Add 12 delegations (should trim to 10)
	base := time.Now().UTC()
	for i := 0; i < 12; i++ {
		s.AppendDelegation(context.Background(), agents.DelegationEntry{
			From:      "Mark",
			To:        "Chris",
			Question:  fmt.Sprintf("q%d", i),
			Answer:    "a",
			Timestamp: base.Add(time.Duration(i) * time.Second),
		})
	}

	results, err := s.LoadRecentDelegations(context.Background(), "Mark", "Chris", 20)
	if err != nil {
		t.Fatalf("LoadRecentDelegations: %v", err)
	}
	if len(results) != 10 {
		t.Errorf("want 10 (trimmed), got %d", len(results))
	}
	// Most recent should be first
	if results[0].Question != "q11" {
		t.Errorf("want q11 first, got %q", results[0].Question)
	}
}

func TestSQLiteMemoryStore_DelegationPairIsolation(t *testing.T) {
	t.Parallel()
	db := openAgentTestDB(t)
	s := agents.NewSQLiteMemoryStore(db.Write(), "machine-1")

	s.AppendDelegation(context.Background(), agents.DelegationEntry{
		From: "A", To: "B", Question: "AB", Answer: "a", Timestamp: time.Now().UTC(),
	})
	s.AppendDelegation(context.Background(), agents.DelegationEntry{
		From: "A", To: "C", Question: "AC", Answer: "a", Timestamp: time.Now().UTC(),
	})

	abResults, _ := s.LoadRecentDelegations(context.Background(), "A", "B", 10)
	if len(abResults) != 1 || abResults[0].Question != "AB" {
		t.Errorf("pair isolation failed: %v", abResults)
	}
}

// Compile-time interface check
var _ agents.MemoryStoreIface = (*agents.SQLiteMemoryStore)(nil)
