package agents

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/scrypster/huginn/internal/sqlitedb"
)

// ReplicationQueueEntry represents a pending cloud vault memory operation.
type ReplicationQueueEntry struct {
	ID           string
	SessionID    string
	AgentID      string // agent name
	VaultName    string
	Operation    string // "insert", "update", "delete"
	MemoryID     string
	Concept      string
	MemoryContent string
	Status       string // "pending", "in_progress", "completed", "failed", "dead"
	ErrorMessage string
	Attempts     int
	MaxAttempts  int
	NextRetryAt  time.Time
	CreatedAt    time.Time
}

// MemoryReplicator manages replication of agent memories to the HuginnCloud vault.
// It drains a SQLite queue (cloud_vault_queue) and pushes entries via CloudVaultClient.
// When no vaultClient is wired (WithVaultClient not called), the replicator runs in
// no-op mode: entries are acknowledged as completed immediately (local-only mode).
type MemoryReplicator struct {
	db          *sqlitedb.DB
	machineID   string
	vaultClient CloudVaultClient // nil = local-only, no cloud push
	mu          sync.Mutex
	done        chan struct{}
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewMemoryReplicator creates a new MemoryReplicator backed by the given DB.
func NewMemoryReplicator(db *sqlitedb.DB) *MemoryReplicator {
	ctx, cancel := context.WithCancel(context.Background())
	return &MemoryReplicator{
		db:     db,
		done:   make(chan struct{}),
		ctx:    ctx,
		cancel: cancel,
	}
}

// WithVaultClient wires a CloudVaultClient for pushing memories to HuginnCloud.
// Must be called before Start(). Without this, the replicator runs in no-op mode.
func (mr *MemoryReplicator) WithVaultClient(client CloudVaultClient, machineID string) *MemoryReplicator {
	mr.vaultClient = client
	mr.machineID = machineID
	return mr
}

// Start begins processing the cloud vault replication queue in a background goroutine.
// Call Stop() to shut down cleanly.
func (mr *MemoryReplicator) Start() {
	go mr.processQueueLoop()
}

// Stop gracefully shuts down the replicator and waits for the background goroutine.
func (mr *MemoryReplicator) Stop() {
	mr.cancel()
	<-mr.done
}

// processQueueLoop polls for pending queue entries on a 5-second tick.
// 5 seconds is sufficient for async cloud sync; avoids unnecessary DB churn.
func (mr *MemoryReplicator) processQueueLoop() {
	defer close(mr.done)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-mr.ctx.Done():
			return
		case <-ticker.C:
			mr.processBatch(mr.ctx)
		}
	}
}

// processBatch fetches and processes a batch of pending entries, then purges stale rows.
func (mr *MemoryReplicator) processBatch(ctx context.Context) {
	mr.mu.Lock()
	defer mr.mu.Unlock()

	entries, err := mr.fetchPendingEntries(ctx, 10)
	if err != nil {
		slog.Error("cloud vault replicator: fetch pending entries", "err", err)
		return
	}

	for _, entry := range entries {
		mr.processEntry(ctx, entry)
	}

	mr.purgeDeadEntries(ctx)
}

// fetchPendingEntries retrieves up to limit due queue entries ordered by creation time.
func (mr *MemoryReplicator) fetchPendingEntries(ctx context.Context, limit int) ([]ReplicationQueueEntry, error) {
	now := time.Now().Unix()
	rows, err := mr.db.Read().QueryContext(ctx, `
		SELECT id, session_id, agent_id, vault_name, operation, memory_id, concept,
		       memory_content, status, error_message, attempts, max_attempts,
		       next_retry_at, created_at
		FROM cloud_vault_queue
		WHERE status IN ('pending', 'in_progress') AND next_retry_at <= ?
		ORDER BY created_at ASC
		LIMIT ?
	`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []ReplicationQueueEntry
	for rows.Next() {
		var entry ReplicationQueueEntry
		var nextRetryAt, createdAt int64
		if err := rows.Scan(
			&entry.ID, &entry.SessionID, &entry.AgentID, &entry.VaultName,
			&entry.Operation, &entry.MemoryID, &entry.Concept, &entry.MemoryContent,
			&entry.Status, &entry.ErrorMessage, &entry.Attempts, &entry.MaxAttempts,
			&nextRetryAt, &createdAt,
		); err != nil {
			slog.Error("cloud vault replicator: scan entry", "err", err)
			continue
		}
		entry.NextRetryAt = time.Unix(nextRetryAt, 0)
		entry.CreatedAt = time.Unix(createdAt, 0)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// processEntry attempts to push a single entry to the cloud vault.
// On transient failure the entry is rescheduled with exponential backoff.
// After MaxAttempts failures the entry is marked "dead" (preserved for diagnostics,
// purged after 7 days by purgeDeadEntries).
func (mr *MemoryReplicator) processEntry(ctx context.Context, entry ReplicationQueueEntry) {
	if err := mr.updateEntryStatus(ctx, entry.ID, "in_progress", ""); err != nil {
		slog.Error("cloud vault replicator: mark in_progress", "entry_id", entry.ID, "err", err)
		return
	}

	// Local-only mode: no vault client configured — acknowledge and move on.
	if mr.vaultClient == nil || mr.machineID == "" {
		_ = mr.updateEntryStatus(ctx, entry.ID, "completed", "")
		return
	}

	op := "set"
	if entry.Operation == "delete" {
		op = "delete"
	}

	pushEntries := []VaultPushEntry{{
		Op:        op,
		AgentName: entry.AgentID,
		MemoryID:  entry.MemoryID,
		Vault:     entry.VaultName,
		Concept:   entry.Concept,
		Content:   entry.MemoryContent,
		CreatedAt: entry.CreatedAt.UnixMilli(),
	}}

	// withRetry provides 3 in-process attempts (100/200/400ms) before returning error.
	err := withRetry(ctx, func() error {
		return mr.vaultClient.PushEntries(ctx, mr.machineID, pushEntries)
	})

	if err != nil {
		newAttempts := entry.Attempts + 1
		status := "pending"
		if entry.MaxAttempts > 0 && newAttempts >= entry.MaxAttempts {
			status = "dead"
		}
		nextRetry := time.Now().Add(cloudVaultBackoff(newAttempts)).Unix()
		if uErr := mr.updateEntryBackoff(ctx, entry.ID, status, err.Error(), newAttempts, nextRetry); uErr != nil {
			slog.Error("cloud vault replicator: update backoff", "entry_id", entry.ID, "err", uErr)
		}
		slog.Warn("cloud vault replicator: push failed",
			"entry_id", entry.ID, "attempts", newAttempts, "status", status, "err", err)
		return
	}

	if err := mr.updateEntryStatus(ctx, entry.ID, "completed", ""); err != nil {
		slog.Error("cloud vault replicator: mark completed", "entry_id", entry.ID, "err", err)
	}
}

// cloudVaultBackoff returns exponential backoff for queue-level retries.
// 30s → 1m → 2m → 4m → 8m, capped at 1h, plus up to 5s jitter.
func cloudVaultBackoff(attempts int) time.Duration {
	const base = 30 * time.Second
	const cap = time.Hour
	d := base * (1 << uint(attempts))
	if d > cap || d <= 0 { // guard overflow
		d = cap
	}
	return d + time.Duration(rand.Int63n(int64(5*time.Second)))
}

// updateEntryStatus updates a queue entry's status and clears the error message.
func (mr *MemoryReplicator) updateEntryStatus(ctx context.Context, id, status, errorMsg string) error {
	_, err := mr.db.Write().ExecContext(ctx, `
		UPDATE cloud_vault_queue
		SET status = ?, error_message = ?, updated_at = unixepoch()
		WHERE id = ?
	`, status, errorMsg, id)
	return err
}

// updateEntryBackoff updates attempts, status, error, and next_retry_at for a failed entry.
func (mr *MemoryReplicator) updateEntryBackoff(ctx context.Context, id, status, errorMsg string, attempts int, nextRetryAt int64) error {
	_, err := mr.db.Write().ExecContext(ctx, `
		UPDATE cloud_vault_queue
		SET status = ?, error_message = ?, attempts = ?, next_retry_at = ?, updated_at = unixepoch()
		WHERE id = ?
	`, status, errorMsg, attempts, nextRetryAt, id)
	return err
}

// EnqueueMemoryOperation adds a memory operation to the cloud vault replication queue.
// Callers: muninn tool interceptor, memory write paths.
// Uses INSERT OR REPLACE semantics (UNIQUE on vault_name+memory_id) so a more
// recent write to the same memory overwrites a queued-but-unsent earlier write.
func (mr *MemoryReplicator) EnqueueMemoryOperation(ctx context.Context, sessionID, agentID, vaultName, operation, memoryID, concept, memoryContent string) error {
	if sessionID == "" || agentID == "" || vaultName == "" || operation == "" || memoryID == "" {
		return errors.New("missing required parameters")
	}

	id := newULID()
	_, err := mr.db.Write().ExecContext(ctx, `
		INSERT INTO cloud_vault_queue (
			id, session_id, agent_id, vault_name, operation, memory_id,
			concept, memory_content, status, next_retry_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', unixepoch())
		ON CONFLICT(vault_name, memory_id) DO UPDATE SET
			operation      = excluded.operation,
			concept        = excluded.concept,
			memory_content = excluded.memory_content,
			status         = 'pending',
			next_retry_at  = unixepoch(),
			updated_at     = unixepoch()
	`, id, sessionID, agentID, vaultName, operation, memoryID, concept, memoryContent)
	return err
}

// PendingCount returns the number of pending cloud vault replication entries.
func (mr *MemoryReplicator) PendingCount(ctx context.Context) (int, error) {
	var count int
	err := mr.db.Read().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cloud_vault_queue WHERE status = 'pending'
	`).Scan(&count)
	return count, err
}

// purgeDeadEntries removes completed and dead rows older than 7 days.
func (mr *MemoryReplicator) purgeDeadEntries(ctx context.Context) {
	cutoff := time.Now().Add(-7 * 24 * time.Hour).Unix()
	_, err := mr.db.Write().ExecContext(ctx, `
		DELETE FROM cloud_vault_queue
		WHERE status IN ('completed', 'dead') AND created_at < ?
	`, cutoff)
	if err != nil {
		slog.Warn("cloud vault replicator: purge dead entries", "err", err)
	}
}

// newULID generates a monotonically-increasing ULID using the default entropy source.
func newULID() string {
	return ulid.Make().String()
}
