package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/scrypster/huginn/internal/mcp"
	mem "github.com/scrypster/huginn/internal/memory"
	"github.com/scrypster/huginn/internal/tools"
	"github.com/scrypster/huginn/internal/workforce"
)

const (
	replicationPoolSize         = 3
	maxConcurrentReplications   = 8
	replicationRetryMaxAttempts = 5
	replicationDrainBatchSize   = 8
	replicationDrainIdleReset   = 30 * time.Second
	replicationDrainActiveReset = 2 * time.Second
	replicationStopTimeout      = 5 * time.Second
)

// ReplicationQueuer is the minimal interface needed for SQLite queue operations.
// Avoids a direct import of sqlitedb to keep the replicator testable in isolation.
type ReplicationQueuer interface {
	ReadQ() ReplicationDBReader
	WriteQ() ReplicationDBWriter
}

// ReplicationDBReader is a minimal reader interface for the replication queue.
type ReplicationDBReader interface {
	QueryContext(ctx context.Context, query string, args ...any) (ReplicationRows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) ReplicationRow
}

// ReplicationDBWriter is a minimal writer interface for the replication queue.
type ReplicationDBWriter interface {
	ExecContext(ctx context.Context, query string, args ...any) (ReplicationResult, error)
}

// ReplicationRows is a minimal rows interface for the replication queue scanner.
type ReplicationRows interface {
	Next() bool
	Scan(dest ...any) error
	Close() error
	Err() error
}

// ReplicationRow is a minimal single-row interface for the replication queue.
type ReplicationRow interface {
	Scan(dest ...any) error
}

// ReplicationResult is a minimal result interface for write operations.
type ReplicationResult interface {
	RowsAffected() (int64, error)
}

// MemoryReplicator replicates agent memories across channel vault members.
// It intercepts successful muninn tool calls and fans out the memory to every
// other channel member's personal MuninnDB vault, at zero LLM cost.
type MemoryReplicator struct {
	muninnCfgPath string
	db            ReplicationQueuer  // nil = no SQLite (no-op enqueue/drain)
	pool          chan *mcp.MCPClient // bounded pool, cap replicationPoolSize
	sem           chan struct{}       // bounds concurrent goroutines
	wg            sync.WaitGroup     // tracks in-flight goroutines for shutdown
	stopCh        chan struct{}       // closed by Stop()
	stopOnce      sync.Once          // guards close(stopCh) against double-close panic
}

// NewMemoryReplicator creates a MemoryReplicator. db may be nil for testing (no-op queue).
func NewMemoryReplicator(muninnCfgPath string, db ReplicationQueuer) *MemoryReplicator {
	return &MemoryReplicator{
		muninnCfgPath: muninnCfgPath,
		db:            db,
		pool:          make(chan *mcp.MCPClient, replicationPoolSize),
		sem:           make(chan struct{}, maxConcurrentReplications),
		stopCh:        make(chan struct{}),
	}
}

// Start runs the adaptive drain loop. Should be called in a goroutine.
// Fires immediately on startup to drain any rows queued before the last shutdown.
func (r *MemoryReplicator) Start(ctx context.Context) {
	timer := time.NewTimer(0) // drain immediately on startup to clear any backlog
	defer timer.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ctx.Done():
			return
		case <-timer.C:
			if r.db == nil {
				timer.Reset(replicationDrainIdleReset)
				continue
			}
			remaining := r.drainBatch(ctx, replicationDrainBatchSize)
			r.purgeDeadEntries(ctx)
			if remaining > 0 {
				timer.Reset(replicationDrainActiveReset)
			} else {
				timer.Reset(replicationDrainIdleReset)
			}
		}
	}
}

// Stop signals the drain loop to exit and waits for in-flight goroutines to finish.
// Safe to call multiple times.
func (r *MemoryReplicator) Stop() {
	r.stopOnce.Do(func() { close(r.stopCh) })

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(replicationStopTimeout):
		slog.Warn("memory_replicator: stop timeout — some goroutines may still be running")
	}

	// Drain and close pool clients.
	for {
		select {
		case c := <-r.pool:
			if c != nil {
				_ = c.Close()
			}
		default:
			return
		}
	}
}

// Intercept is called after a muninn tool call succeeds. It checks if the tool
// should be replicated and fans out to all non-producing channel members.
func (r *MemoryReplicator) Intercept(ctx context.Context, toolName string, args map[string]any, result tools.ToolResult, producerName string, replCtx *workforce.MemReplicationContext) {
	if result.IsError {
		return
	}
	if replCtx == nil || len(replCtx.Members) == 0 {
		return
	}
	if !ShouldReplicate(toolName, args) {
		return
	}

	concept := extractConcept(toolName, args)
	if concept == "" {
		return
	}
	content := extractContent(toolName, args)
	memType := extractMemType(toolName, args)
	origTags := extractTags(args)

	// Count non-producer members before doing any work.
	targetCount := 0
	for _, member := range replCtx.Members {
		if member.AgentName != producerName {
			targetCount++
		}
	}
	if targetCount == 0 {
		return // nothing to replicate (single-agent channel or producer is sole member)
	}

	// Enqueue all non-producer members synchronously for durability.
	for _, member := range replCtx.Members {
		if member.AgentName == producerName {
			continue
		}
		if err := r.enqueueRetry(member.VaultName, producerName, replCtx.SpaceID, concept, content, memType, origTags, replCtx.SpaceName); err != nil {
			slog.Warn("memory_replicator: enqueue failed", "vault", member.VaultName, "concept", concept, "err", err)
		}
	}

	// Async fan-out with a context that outlives the request.
	detached := context.WithoutCancel(ctx)
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() {
			if p := recover(); p != nil {
				slog.Error("memory_replicator: panic in fanOut", "err", p)
			}
		}()
		fanCtx, cancel := context.WithTimeout(detached, 30*time.Second)
		defer cancel()
		r.fanOut(fanCtx, concept, content, memType, origTags, producerName, replCtx)
	}()
}

// ReplicateFinishSummary replicates a thread completion summary to all channel members.
func (r *MemoryReplicator) ReplicateFinishSummary(ctx context.Context, summary, producerName, spaceID, spaceName string, members []workforce.ReplicationMember) {
	if summary == "" {
		return
	}

	concept := "thread completion: " + producerName
	memType := "event"
	tags := []string{"thread:completion"}

	targetCount := 0
	for _, member := range members {
		if member.AgentName != producerName {
			targetCount++
		}
	}
	if targetCount == 0 {
		return
	}

	for _, member := range members {
		if member.AgentName == producerName {
			continue
		}
		if err := r.enqueueRetry(member.VaultName, producerName, spaceID, concept, summary, memType, tags, spaceName); err != nil {
			slog.Warn("memory_replicator: enqueue finish summary failed", "vault", member.VaultName, "err", err)
		}
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() {
			if p := recover(); p != nil {
				slog.Error("memory_replicator: panic in ReplicateFinishSummary fanOut", "err", p)
			}
		}()
		// Detach from caller's context so thread-shutdown cancellation doesn't
		// abort the replication goroutine before it can deliver or enqueue.
		fanCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		replCtx := &workforce.MemReplicationContext{
			SpaceID:   spaceID,
			SpaceName: spaceName,
			Members:   members,
		}
		r.fanOut(fanCtx, concept, summary, memType, tags, producerName, replCtx)
	}()
}

// ShouldReplicate returns true if the tool call represents a memory worth replicating.
// Exported so external callers can gate replication decisions.
func ShouldReplicate(toolName string, args map[string]any) bool {
	// Anti-echo: never replicate already-replicated memories.
	if tags, ok := args["tags"]; ok {
		switch tt := tags.(type) {
		case []any:
			for _, t := range tt {
				if s, ok := t.(string); ok && s == "replicated:true" {
					return false
				}
			}
		case []string:
			for _, s := range tt {
				if s == "replicated:true" {
					return false
				}
			}
		}
	}

	switch toolName {
	case "muninn_decide":
		return true
	case "muninn_remember":
		memType, _ := args["type"].(string)
		switch memType {
		case "decision", "constraint", "fact", "procedure", "preference", "issue":
			return true
		}
		return false
	case "muninn_remember_batch":
		// Batch calls have no top-level "type" field — check if any memory in the
		// batch has a replicable type. Replicate the whole batch if any member qualifies.
		if mems, ok := args["memories"].([]any); ok {
			for _, m := range mems {
				if mm, ok := m.(map[string]any); ok {
					mt, _ := mm["type"].(string)
					switch mt {
					case "decision", "constraint", "fact", "procedure", "preference", "issue":
						return true
					}
				}
			}
		}
		return false
	case "muninn_remember_tree":
		return true
	case "muninn_evolve":
		return true
	}
	return false
}

// fanOut sends the memory to all non-producer members concurrently.
func (r *MemoryReplicator) fanOut(ctx context.Context, concept, content, memType string, baseTags []string, producerName string, replCtx *workforce.MemReplicationContext) {
	for _, member := range replCtx.Members {
		if member.AgentName == producerName {
			continue
		}
		m := member // capture for goroutine
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			defer func() {
				if p := recover(); p != nil {
					slog.Error("memory_replicator: panic in per-member fanOut", "vault", m.VaultName, "err", p)
				}
			}()
			// Acquire semaphore slot.
			select {
			case r.sem <- struct{}{}:
				defer func() { <-r.sem }()
				if err := r.replicateTo(ctx, m, concept, content, memType, baseTags, replCtx.SpaceID, replCtx.SpaceName, producerName); err != nil {
					slog.Warn("memory_replicator: replicateTo failed, queuing retry",
						"vault", m.VaultName, "concept", concept, "err", err)
					if qErr := r.enqueueRetry(m.VaultName, producerName, replCtx.SpaceID, concept, content, memType, baseTags, replCtx.SpaceName); qErr != nil {
						slog.Error("memory_replicator: enqueue retry failed", "vault", m.VaultName, "err", qErr)
					}
				}
			case <-ctx.Done():
				// Context expired: fall back to drain queue.
				if qErr := r.enqueueRetry(m.VaultName, producerName, replCtx.SpaceID, concept, content, memType, baseTags, replCtx.SpaceName); qErr != nil {
					slog.Error("memory_replicator: enqueue fallback failed", "vault", m.VaultName, "err", qErr)
				}
			}
		}()
	}
}

// replicateTo writes one memory entry to a target vault via MCP.
func (r *MemoryReplicator) replicateTo(ctx context.Context, member workforce.ReplicationMember, concept, content, memType string, baseTags []string, spaceID, spaceName, producerName string) error {
	client, err := r.acquireClient(ctx)
	if err != nil {
		return fmt.Errorf("acquireClient: %w", err)
	}
	defer r.releaseClient(client)

	conceptKey := normalizeConcept(concept)

	// Idempotency check: skip if already written.
	findResult, err := client.CallTool(ctx, "muninn_find_by_entity", map[string]any{
		"vault":  member.VaultName,
		"entity": "replicated_concept:" + conceptKey,
	})
	if err == nil && findResult != nil && len(findResult.Content) > 0 {
		// Found existing entry — dequeue and return success.
		r.dequeue(member.VaultName, conceptKey, spaceID)
		return nil
	}

	tags := buildReplicaTags(baseTags, producerName, spaceName, conceptKey)

	_, err = client.CallTool(ctx, "muninn_remember", map[string]any{
		"vault":   member.VaultName,
		"concept": concept,
		"content": content,
		"type":    memType,
		"tags":    tags,
	})
	if err != nil {
		return fmt.Errorf("muninn_remember: %w", err)
	}

	r.dequeue(member.VaultName, conceptKey, spaceID)
	return nil
}

// enqueueRetry inserts or updates a row in memory_replication_queue.
// INSERT ON CONFLICT preserves backoff; resurrects dead rows with new content.
func (r *MemoryReplicator) enqueueRetry(targetVault, producerName, spaceID, concept, content, memType string, tags []string, spaceName string) error {
	if r.db == nil {
		return nil // no-op when SQLite not configured
	}

	type payload struct {
		Concept     string   `json:"concept"`
		Content     string   `json:"content"`
		MemType     string   `json:"mem_type"`
		Tags        []string `json:"tags"`
		SpaceName   string   `json:"space_name"`
		ProducerName string  `json:"producer_name"`
	}
	p := payload{
		Concept:      concept,
		Content:      content,
		MemType:      memType,
		Tags:         tags,
		SpaceName:    spaceName,
		ProducerName: producerName,
	}
	data, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("enqueueRetry: marshal: %w", err)
	}

	conceptKey := normalizeConcept(concept)
	now := time.Now().Unix()
	nextRetry := now + int64(backoffDuration(0).Seconds())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = r.db.WriteQ().ExecContext(ctx, `
		INSERT INTO memory_replication_queue
		    (target_vault, source_agent, space_id, concept_key, payload, status, attempts, max_attempts, next_retry_at)
		VALUES (?, ?, ?, ?, ?, 'pending', 0, ?, ?)
		ON CONFLICT(target_vault, concept_key, space_id) DO UPDATE SET
		    payload       = excluded.payload,
		    max_attempts  = excluded.max_attempts,
		    -- Resurrect dead rows: reset attempts + status so the row gets a full
		    -- retry budget again. For non-dead rows preserve status/attempts/backoff.
		    status        = CASE WHEN memory_replication_queue.status = 'dead'
		                         THEN 'pending'
		                         ELSE memory_replication_queue.status END,
		    attempts      = CASE WHEN memory_replication_queue.status = 'dead'
		                         THEN 0
		                         ELSE memory_replication_queue.attempts END,
		    next_retry_at = CASE WHEN memory_replication_queue.status = 'dead'
		                         THEN excluded.next_retry_at
		                         ELSE memory_replication_queue.next_retry_at END
	`, targetVault, producerName, spaceID, conceptKey, string(data), replicationRetryMaxAttempts, nextRetry)
	return err
}

// dequeue removes a successfully delivered row from the queue.
func (r *MemoryReplicator) dequeue(targetVault, conceptKey, spaceID string) {
	if r.db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := r.db.WriteQ().ExecContext(ctx,
		`DELETE FROM memory_replication_queue WHERE target_vault=? AND concept_key=? AND space_id=?`,
		targetVault, conceptKey, spaceID,
	)
	if err != nil {
		slog.Warn("memory_replicator: dequeue failed", "vault", targetVault, "concept_key", conceptKey, "err", err)
	}
}

// drainBatch processes up to batchSize pending rows from the queue.
// Returns the count of remaining eligible rows after the batch.
func (r *MemoryReplicator) drainBatch(ctx context.Context, batchSize int) int {
	if r.db == nil {
		return 0
	}
	now := time.Now().Unix()

	type row struct {
		id           int64
		targetVault  string
		sourceAgent  string
		spaceID      string
		conceptKey   string
		payload      string
		attempts     int
	}

	qCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	rows, err := r.db.ReadQ().QueryContext(qCtx, `
		SELECT id, target_vault, source_agent, space_id, concept_key, payload, attempts
		FROM memory_replication_queue
		WHERE status = 'pending' AND next_retry_at <= ?
		ORDER BY next_retry_at ASC
		LIMIT ?
	`, now, batchSize)
	if err != nil {
		slog.Warn("memory_replicator: drainBatch query failed", "err", err)
		return 0
	}
	defer rows.Close()

	var entries []row
	for rows.Next() {
		var e row
		if err := rows.Scan(&e.id, &e.targetVault, &e.sourceAgent, &e.spaceID, &e.conceptKey, &e.payload, &e.attempts); err != nil {
			slog.Warn("memory_replicator: drainBatch scan", "err", err)
			continue
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("memory_replicator: drainBatch rows.Err", "err", err)
	}

	for _, e := range entries {
		var p struct {
			Concept      string   `json:"concept"`
			Content      string   `json:"content"`
			MemType      string   `json:"mem_type"`
			Tags         []string `json:"tags"`
			SpaceName    string   `json:"space_name"`
			ProducerName string   `json:"producer_name"`
		}
		if err := json.Unmarshal([]byte(e.payload), &p); err != nil {
			slog.Warn("memory_replicator: drainBatch unmarshal payload", "id", e.id, "err", err)
			continue
		}

		member := workforce.ReplicationMember{
			AgentName: "",
			VaultName: e.targetVault,
		}
		drainCtx, drainCancel := context.WithTimeout(ctx, 15*time.Second)
		err := r.replicateTo(drainCtx, member, p.Concept, p.Content, p.MemType, p.Tags, e.spaceID, p.SpaceName, p.ProducerName)
		drainCancel()

		if err != nil {
			newAttempts := e.attempts + 1
			var newStatus string
			if newAttempts >= replicationRetryMaxAttempts {
				newStatus = "dead"
			} else {
				newStatus = "pending"
			}
			nextRetry := time.Now().Add(backoffDuration(newAttempts)).Unix()
			wCtx, wCancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, uErr := r.db.WriteQ().ExecContext(wCtx, `
				UPDATE memory_replication_queue
				SET attempts=?, status=?, next_retry_at=?
				WHERE id=?
			`, newAttempts, newStatus, nextRetry, e.id)
			wCancel()
			if uErr != nil {
				slog.Warn("memory_replicator: drainBatch update failed", "id", e.id, "err", uErr)
			}
		}
	}

	// Return count of remaining eligible rows.
	var remaining int
	cCtx, cCancel := context.WithTimeout(ctx, 2*time.Second)
	defer cCancel()
	rErr := r.db.ReadQ().QueryRowContext(cCtx, `
		SELECT COUNT(*) FROM memory_replication_queue
		WHERE status='pending' AND next_retry_at <= ?
	`, time.Now().Unix()).Scan(&remaining)
	if rErr != nil {
		return 0
	}
	return remaining
}

// purgeDeadEntries removes dead rows older than 7 days.
func (r *MemoryReplicator) purgeDeadEntries(ctx context.Context) {
	if r.db == nil {
		return
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour).Unix()
	pCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := r.db.WriteQ().ExecContext(pCtx,
		`DELETE FROM memory_replication_queue WHERE status='dead' AND created_at < ?`,
		cutoff,
	)
	if err != nil {
		slog.Warn("memory_replicator: purgeDeadEntries failed", "err", err)
	}
}

// backoffDuration returns the retry wait for the given attempt number.
func backoffDuration(attempt int) time.Duration {
	switch attempt {
	case 0, 1:
		return 5 * time.Second
	case 2:
		return 30 * time.Second
	case 3:
		return 2 * time.Minute
	case 4:
		return 10 * time.Minute
	default:
		return time.Hour
	}
}

// acquireClient gets a client from the pool or creates a new one.
func (r *MemoryReplicator) acquireClient(ctx context.Context) (*mcp.MCPClient, error) {
	select {
	case c := <-r.pool:
		if c != nil {
			return c, nil
		}
	default:
	}
	return r.newClient(ctx)
}

// releaseClient returns a client to the pool, or closes it if the pool is full.
func (r *MemoryReplicator) releaseClient(c *mcp.MCPClient) {
	if c == nil {
		return
	}
	select {
	case r.pool <- c:
	default:
		_ = c.Close()
	}
}

// newClient creates and initializes a fresh MCP client connected to MuninnDB.
func (r *MemoryReplicator) newClient(ctx context.Context) (*mcp.MCPClient, error) {
	cfg, err := mem.LoadGlobalConfig(r.muninnCfgPath)
	if err != nil {
		return nil, fmt.Errorf("newClient: load config: %w", err)
	}

	// Use the username as the vault name for the replication token.
	// Falls back to the first configured vault token if the username vault is absent.
	vaultName := cfg.Username
	if vaultName == "" {
		vaultName = "default"
	}
	token, err := mem.VaultTokenFor(cfg, vaultName)
	if err != nil {
		// Try first available token as fallback.
		for _, tok := range cfg.VaultTokens {
			token = tok
			err = nil
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("newClient: get token: %w", err)
	}

	mcpURL, err := mem.MCPURLFromEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("newClient: build URL: %w", err)
	}

	transport := mcp.NewHTTPTransport(mcpURL, token)
	client := mcp.NewMCPClient(transport)

	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.Initialize(initCtx); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("newClient: initialize: %w", err)
	}
	return client, nil
}

// --- extraction helpers ---

// extractConcept gets the concept from tool args.
func extractConcept(toolName string, args map[string]any) string {
	switch toolName {
	case "muninn_remember", "muninn_remember_tree":
		if c, ok := args["concept"].(string); ok {
			return c
		}
	case "muninn_remember_batch":
		// Use first memory's concept as representative key.
		if mems, ok := args["memories"].([]any); ok && len(mems) > 0 {
			if m, ok := mems[0].(map[string]any); ok {
				if c, ok := m["concept"].(string); ok {
					return c
				}
			}
		}
		if c, ok := args["concept"].(string); ok {
			return c
		}
	case "muninn_decide":
		if q, ok := args["question"].(string); ok {
			return "decision: " + q
		}
		if c, ok := args["concept"].(string); ok {
			return c
		}
	case "muninn_evolve":
		if c, ok := args["concept"].(string); ok {
			return c
		}
	}
	return ""
}

// extractContent gets the content from tool args.
func extractContent(toolName string, args map[string]any) string {
	switch toolName {
	case "muninn_remember", "muninn_remember_tree", "muninn_evolve":
		if c, ok := args["content"].(string); ok {
			return c
		}
	case "muninn_remember_batch":
		if mems, ok := args["memories"].([]any); ok && len(mems) > 0 {
			if m, ok := mems[0].(map[string]any); ok {
				if c, ok := m["content"].(string); ok {
					return c
				}
			}
		}
		if c, ok := args["content"].(string); ok {
			return c
		}
	case "muninn_decide":
		if dec, ok := args["decision"].(string); ok {
			return dec
		}
		if rat, ok := args["rationale"].(string); ok {
			return rat
		}
	}
	return ""
}

// extractMemType gets the memory type from tool args.
func extractMemType(toolName string, args map[string]any) string {
	switch toolName {
	case "muninn_decide":
		return "decision"
	case "muninn_remember", "muninn_remember_batch", "muninn_remember_tree":
		if t, ok := args["type"].(string); ok {
			return t
		}
	case "muninn_evolve":
		if t, ok := args["type"].(string); ok {
			return t
		}
	}
	return "fact"
}

// extractTags gets tags from tool args, handling both []any and []string forms.
func extractTags(args map[string]any) []string {
	raw, ok := args["tags"]
	if !ok {
		return nil
	}
	switch t := raw.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, v := range t {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// --- provenance helpers ---

var nonAlphaRe = regexp.MustCompile(`[^a-z0-9]+`)

// normalizeConcept converts a concept to a canonical slug: lowercase, non-alphanumeric→"-", max 64 chars.
func normalizeConcept(concept string) string {
	s := strings.ToLower(concept)
	s = nonAlphaRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

// buildReplicaTags constructs the full tag list for a replicated memory,
// preserving original tags while adding provenance and dedup markers.
func buildReplicaTags(origTags []string, producerName, spaceName, conceptKey string) []string {
	result := []string{
		"replicated:true",
		"source:" + producerName,
		"channel:" + spaceName,
		"replicated_concept:" + conceptKey,
	}
	for _, t := range origTags {
		if t == "replicated:true" {
			continue
		}
		if strings.HasPrefix(t, "source:") {
			continue
		}
		if strings.HasPrefix(t, "replicated_concept:") {
			continue
		}
		result = append(result, t)
	}
	return result
}

// isMemoryToolName returns true if name is a muninn tool that may warrant replication.
// Used to stash args in the OnToolCall callback before execution.
func isMemoryToolName(name string) bool {
	switch name {
	case "muninn_remember", "muninn_remember_batch", "muninn_remember_tree",
		"muninn_decide", "muninn_evolve":
		return true
	}
	return false
}

// hashMessage returns a 16-hex-char hash of msg for use as a cache key.
func hashMessage(msg string) string {
	h := sha256.Sum256([]byte(msg))
	return fmt.Sprintf("%x", h[:8])
}
