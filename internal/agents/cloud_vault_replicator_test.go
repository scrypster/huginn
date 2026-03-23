package agents_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// ---- mock CloudVaultClient ----

type mockVaultClient struct {
	mu    sync.Mutex
	calls [][]agents.VaultPushEntry
	err   error // returned on every PushEntries call when non-nil
}

func (m *mockVaultClient) PushEntries(_ context.Context, _ string, entries []agents.VaultPushEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.calls = append(m.calls, entries)
	return nil
}

func (m *mockVaultClient) FetchAll(_ context.Context, _, _, _ string) ([]agents.VaultFetchEntry, string, error) {
	return nil, "", nil
}

func (m *mockVaultClient) FetchSince(_ context.Context, _, _, _ string) ([]agents.VaultFetchEntry, string, error) {
	return nil, "", nil
}

func (m *mockVaultClient) totalCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *mockVaultClient) setErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

// ---- test DB helpers ----

func openReplicatorDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	// Use a file-based DB — ":memory:" opens separate in-memory databases per
	// connection (read pool ≠ write connection), so DDL on write is invisible to reads.
	path := fmt.Sprintf("%s/test-%d.db", t.TempDir(), time.Now().UnixNano())
	db, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	// Apply base schema (creates _migrations table etc.)
	if err := db.ApplySchema(); err != nil {
		t.Fatalf("ApplySchema: %v", err)
	}
	// Create cloud_vault_queue directly (avoids pulling session.Migrations import cycle).
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS cloud_vault_queue (
		    id             TEXT    NOT NULL PRIMARY KEY,
		    session_id     TEXT    NOT NULL,
		    agent_id       TEXT    NOT NULL,
		    vault_name     TEXT    NOT NULL,
		    operation      TEXT    NOT NULL
		                       CHECK (operation IN ('insert', 'update', 'delete')),
		    memory_id      TEXT    NOT NULL,
		    concept        TEXT    NOT NULL DEFAULT '',
		    memory_content TEXT    NOT NULL DEFAULT '',
		    status         TEXT    NOT NULL DEFAULT 'pending'
		                       CHECK (status IN ('pending', 'in_progress', 'completed', 'failed', 'dead')),
		    error_message  TEXT    NOT NULL DEFAULT '',
		    attempts       INTEGER NOT NULL DEFAULT 0,
		    max_attempts   INTEGER NOT NULL DEFAULT 5,
		    next_retry_at  INTEGER NOT NULL DEFAULT 0,
		    created_at     INTEGER NOT NULL DEFAULT (unixepoch()),
		    updated_at     INTEGER NOT NULL DEFAULT (unixepoch()),
		    UNIQUE(vault_name, memory_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cvq_drain
		    ON cloud_vault_queue(status, next_retry_at)
		    WHERE status IN ('pending', 'in_progress')`,
	}
	for _, s := range stmts {
		if _, err := db.Write().Exec(s); err != nil {
			t.Fatalf("create cloud_vault_queue schema: %v", err)
		}
	}
	return db
}

func newMR(t *testing.T, db *sqlitedb.DB, client agents.CloudVaultClient) *agents.MemoryReplicator {
	t.Helper()
	mr := agents.NewMemoryReplicator(db)
	if client != nil {
		mr.WithVaultClient(client, "test-machine-01")
	}
	return mr
}

func waitCond(t *testing.T, maxWait time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// ---- tests ----

func TestMemoryReplicator_NoOpWithoutVaultClient(t *testing.T) {
	db := openReplicatorDB(t)
	mr := newMR(t, db, nil) // local-only mode
	mr.Start()
	defer mr.Stop()

	ctx := context.Background()
	if err := mr.EnqueueMemoryOperation(ctx, "sess-1", "agent-A", "vault-A", "insert", "mem-1", "concept-1", "content-1"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	ok := waitCond(t, 15*time.Second, func() bool {
		count, err := mr.PendingCount(ctx)
		return err == nil && count == 0
	})
	if !ok {
		t.Fatal("pending count did not reach 0 in local-only mode")
	}
}

func TestMemoryReplicator_PushesSetOnInsert(t *testing.T) {
	db := openReplicatorDB(t)
	client := &mockVaultClient{}
	mr := newMR(t, db, client)
	mr.Start()
	defer mr.Stop()

	ctx := context.Background()
	if err := mr.EnqueueMemoryOperation(ctx, "sess-1", "agent-A", "vault-A", "insert", "mem-1", "arch-concept", "architecture content"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	ok := waitCond(t, 15*time.Second, func() bool {
		return client.totalCalls() >= 1
	})
	if !ok {
		t.Fatal("vault client was not called within timeout")
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.calls) == 0 || len(client.calls[0]) == 0 {
		t.Fatal("no push entries recorded")
	}
	entry := client.calls[0][0]
	if entry.Op != "set" {
		t.Errorf("op = %q, want \"set\"", entry.Op)
	}
	if entry.AgentName != "agent-A" {
		t.Errorf("agent_name = %q, want \"agent-A\"", entry.AgentName)
	}
	if entry.MemoryID != "mem-1" {
		t.Errorf("memory_id = %q, want \"mem-1\"", entry.MemoryID)
	}
	if entry.Concept != "arch-concept" {
		t.Errorf("concept = %q, want \"arch-concept\"", entry.Concept)
	}
	if entry.Content != "architecture content" {
		t.Errorf("content = %q, want \"architecture content\"", entry.Content)
	}
	if entry.Vault != "vault-A" {
		t.Errorf("vault = %q, want \"vault-A\"", entry.Vault)
	}
}

func TestMemoryReplicator_PushesDeleteOnDelete(t *testing.T) {
	db := openReplicatorDB(t)
	client := &mockVaultClient{}
	mr := newMR(t, db, client)
	mr.Start()
	defer mr.Stop()

	ctx := context.Background()
	if err := mr.EnqueueMemoryOperation(ctx, "sess-1", "agent-A", "vault-A", "delete", "mem-1", "concept", ""); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	ok := waitCond(t, 15*time.Second, func() bool {
		return client.totalCalls() >= 1
	})
	if !ok {
		t.Fatal("vault client not called")
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if client.calls[0][0].Op != "delete" {
		t.Errorf("op = %q, want \"delete\"", client.calls[0][0].Op)
	}
}

func TestMemoryReplicator_IdempotentUpsert(t *testing.T) {
	// Enqueue the same (vault_name, memory_id) pair twice — UNIQUE constraint should
	// collapse them into one row, and the vault client should see only one push.
	db := openReplicatorDB(t)
	client := &mockVaultClient{}
	mr := newMR(t, db, client)

	ctx := context.Background()
	_ = mr.EnqueueMemoryOperation(ctx, "sess-1", "agent-A", "vault-A", "insert", "mem-1", "concept", "v1")
	_ = mr.EnqueueMemoryOperation(ctx, "sess-1", "agent-A", "vault-A", "update", "mem-1", "concept", "v2")

	mr.Start()
	defer mr.Stop()

	ok := waitCond(t, 15*time.Second, func() bool {
		count, err := mr.PendingCount(ctx)
		return err == nil && count == 0
	})
	if !ok {
		t.Fatal("entries not drained within timeout")
	}

	// Exactly 1 call, with the latest content.
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.calls) != 1 {
		t.Errorf("expected 1 vault push call, got %d", len(client.calls))
	}
	if len(client.calls) > 0 && client.calls[0][0].Content != "v2" {
		t.Errorf("content = %q, want \"v2\"", client.calls[0][0].Content)
	}
}

func TestMemoryReplicator_ValidationRejectsMissingFields(t *testing.T) {
	db := openReplicatorDB(t)
	mr := newMR(t, db, nil)
	ctx := context.Background()

	cases := []struct {
		name                                                    string
		session, agent, vault, op, memID, concept, content string
	}{
		{"missing session", "", "agent", "vault", "insert", "mem", "concept", "body"},
		{"missing agent", "sess", "", "vault", "insert", "mem", "concept", "body"},
		{"missing vault", "sess", "agent", "", "insert", "mem", "concept", "body"},
		{"missing operation", "sess", "agent", "vault", "", "mem", "concept", "body"},
		{"missing memID", "sess", "agent", "vault", "insert", "", "concept", "body"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := mr.EnqueueMemoryOperation(ctx, tc.session, tc.agent, tc.vault, tc.op, tc.memID, tc.concept, tc.content)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestMemoryReplicator_RetryOnTransientError(t *testing.T) {
	db := openReplicatorDB(t)
	client := &mockVaultClient{}
	client.setErr(errors.New("transient network error"))

	mr := newMR(t, db, client)
	mr.Start()

	ctx := context.Background()
	_ = mr.EnqueueMemoryOperation(ctx, "sess-1", "agent-A", "vault-A", "insert", "mem-retry", "concept", "content")

	// Let it try a few times and fail.
	time.Sleep(200 * time.Millisecond)

	// Clear the error — next attempt should succeed.
	client.setErr(nil)

	defer mr.Stop()

	// The entry should eventually be retried and completed.
	ok := waitCond(t, 30*time.Second, func() bool {
		count, _ := mr.PendingCount(ctx)
		return count == 0
	})
	if !ok {
		t.Fatal("entry was not retried and completed")
	}
}

func TestMemoryReplicator_ConcurrentEnqueue(t *testing.T) {
	db := openReplicatorDB(t)
	client := &mockVaultClient{}
	mr := newMR(t, db, client)

	ctx := context.Background()
	const workers = 20
	var wg sync.WaitGroup
	var failed atomic.Int64
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			if err := mr.EnqueueMemoryOperation(ctx,
				"sess-conc", "agent-A", "vault-A",
				"insert", fmt.Sprintf("mem-%d", i),
				"concept", fmt.Sprintf("content-%d", i),
			); err != nil {
				failed.Add(1)
			}
		}(i)
	}
	wg.Wait()

	if n := failed.Load(); n > 0 {
		t.Errorf("%d concurrent enqueues failed", n)
	}

	count, err := mr.PendingCount(ctx)
	if err != nil {
		t.Fatalf("PendingCount: %v", err)
	}
	if count != workers {
		t.Errorf("pending count = %d, want %d", count, workers)
	}

	mr.Start()
	defer mr.Stop()

	ok := waitCond(t, 30*time.Second, func() bool {
		c, err := mr.PendingCount(ctx)
		return err == nil && c == 0
	})
	if !ok {
		t.Fatal("concurrent entries not drained")
	}
	if client.totalCalls() == 0 {
		t.Error("vault client was never called")
	}
}

func TestMemoryReplicator_StopIsClean(t *testing.T) {
	db := openReplicatorDB(t)
	mr := newMR(t, db, nil)
	mr.Start()

	done := make(chan struct{})
	go func() {
		defer close(done)
		mr.Stop()
	}()

	select {
	case <-done:
		// OK
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() did not return within 3 seconds")
	}
}
