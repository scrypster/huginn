package relay

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sync/atomic"

	"github.com/cockroachdb/pebble/v2"
	"github.com/scrypster/huginn/internal/storage"
)

const (
	outboxPrefix          = "relay:outbox:"
	outboxMigrationSentinel = "relay:outbox:migration:v2"
	outboxMaxItems        = 10_000
	outboxWarnThreshold   = 5_000 // 50% — log a warning, still enqueue
	outboxRejectThreshold = 9_000 // 90% — return ErrOutboxNearFull, do not enqueue
	OutboxMaxDepth        = 1000
)

// outboxKeyV1Re matches old-format keys: relay:outbox:<16 hex chars>
// These have no colon after the prefix, just 16 hex characters.
var outboxKeyV1Re = regexp.MustCompile(`^relay:outbox:[0-9a-f]{16}$`)

// Outbox is a Pebble-backed durable queue for outbound relay messages.
type Outbox struct {
	db  *storage.Store
	hub Hub
	seq atomic.Uint64
}

// NewOutbox creates an Outbox backed by the given Store and optional Hub.
// If hub is nil, Flush will always return an error.
// NewOutbox runs a one-time crash-safe migration from the v1 key format
// (no priority byte) to the v2 format (priority byte prefix).
func NewOutbox(db *storage.Store, hub Hub) *Outbox {
	o := &Outbox{db: db, hub: hub}
	o.migrateV1Keys()
	o.initSeq()
	return o
}

// migrateV1Keys rewrites legacy outbox keys (relay:outbox:<hex16>) to the v2
// format (relay:outbox:<priority_encoded>:<hex16>). The migration is
// idempotent: old-format keys are distinguished by the absence of a colon after
// the prefix. A sentinel key marks completion so re-runs are free.
func (o *Outbox) migrateV1Keys() {
	pdb := o.db.DB()
	if pdb == nil {
		return
	}

	// Check sentinel — skip if already migrated.
	sentinelKey := []byte(outboxMigrationSentinel)
	if val, closer, err := pdb.Get(sentinelKey); err == nil {
		closer.Close()
		_ = val
		return
	}

	prefix := []byte(outboxPrefix)
	iter, err := pdb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: incrementLastByte(prefix),
	})
	if err != nil {
		slog.Warn("relay: outbox: v1 migration iter failed", "err", err)
		return
	}
	defer iter.Close()

	const batchSize = 1000
	batch := pdb.NewBatch()

	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		key := string(iter.Key())
		if !outboxKeyV1Re.MatchString(key) {
			continue // already v2 or sentinel — skip
		}
		// Extract hex suffix (the sequence number).
		hexSuffix := key[len(outboxPrefix):]
		// Re-encode as v2 with PriorityNormal (all legacy messages are normal priority).
		newKey := outboxKeyV2(PriorityNormal, 0) // placeholder — we need the actual seq
		// Parse the hex seq to reconstruct the key properly.
		var seq uint64
		if _, err := fmt.Sscanf(hexSuffix, "%x", &seq); err != nil {
			slog.Warn("relay: outbox: v1 migration: could not parse seq, skipping", "key", key)
			continue
		}
		newKey = outboxKeyV2(PriorityNormal, seq)
		val := make([]byte, len(iter.Value()))
		copy(val, iter.Value())
		if err := batch.Set([]byte(newKey), val, nil); err != nil {
			slog.Warn("relay: outbox: v1 migration: set failed", "key", key, "err", err)
			continue
		}
		if err := batch.Delete(iter.Key(), nil); err != nil {
			slog.Warn("relay: outbox: v1 migration: delete failed", "key", key, "err", err)
			continue
		}
		count++
		if count%batchSize == 0 {
			if err := batch.Commit(&pebble.WriteOptions{Sync: true}); err != nil {
				slog.Warn("relay: outbox: v1 migration: batch commit failed", "err", err)
				batch.Close()
				batch = pdb.NewBatch()
			}
		}
	}

	// Commit remaining and write sentinel.
	if err := batch.Set(sentinelKey, []byte("1"), nil); err == nil {
		if err := batch.Commit(&pebble.WriteOptions{Sync: true}); err != nil {
			slog.Warn("relay: outbox: v1 migration: final commit failed", "err", err)
		} else if count > 0 {
			slog.Info("relay: outbox: v1→v2 key migration complete", "keys_migrated", count)
		}
	}
	batch.Close()
}

// Enqueue appends msg to the outbox.
func (o *Outbox) Enqueue(msg Message) error {
	if o.db.DB() == nil {
		return fmt.Errorf("relay: outbox: storage is closed")
	}
	n, err := o.Len()
	if err != nil {
		return fmt.Errorf("relay: outbox len: %w", err)
	}
	if n >= outboxMaxItems {
		return ErrOutboxFull
	}
	if n >= outboxRejectThreshold {
		return ErrOutboxNearFull
	}
	if n >= outboxWarnThreshold {
		slog.Warn("relay: outbox pressure high", "depth", n, "cap", outboxMaxItems)
	}

	// Check if at max depth — if so, drop oldest (FIFO eviction)
	if n >= OutboxMaxDepth {
		if err := o.dropOldest(); err != nil {
			return fmt.Errorf("relay: outbox enqueue drop oldest: %w", err)
		}
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("relay: outbox marshal: %w", err)
	}
	seq := o.seq.Add(1)
	key := outboxKeyV2(msg.Priority, seq)
	return o.db.DB().Set([]byte(key), data, &pebble.WriteOptions{Sync: true})
}

// Drain reads up to n messages in priority+FIFO order and deletes them.
func (o *Outbox) Drain(n int) ([]Message, error) {
	if o.db.DB() == nil {
		return nil, fmt.Errorf("relay: outbox: storage is closed")
	}
	var messages []Message
	prefix := []byte(outboxPrefix)

	iter, err := o.db.DB().NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: incrementLastByte(prefix),
	})
	if err != nil {
		return nil, fmt.Errorf("relay: outbox drain iter: %w", err)
	}
	defer iter.Close()

	batch := o.db.DB().NewBatch()
	defer batch.Close()

	count := 0
	for iter.First(); iter.Valid() && count < n; iter.Next() {
		key := string(iter.Key())
		// Skip the migration sentinel.
		if key == outboxMigrationSentinel {
			continue
		}
		var msg Message
		if err := json.Unmarshal(iter.Value(), &msg); err != nil {
			// Corrupt record: delete it so it doesn't permanently block delivery.
			slog.Warn("relay: outbox skipping and deleting corrupt record",
				"key", key, "err", err)
			if delErr := batch.Delete(iter.Key(), nil); delErr != nil {
				return nil, fmt.Errorf("relay: outbox delete corrupt: %w", delErr)
			}
			count++ // count corrupt entries toward the limit to bound iteration
			continue
		}
		messages = append(messages, msg)
		if err := batch.Delete(iter.Key(), nil); err != nil {
			return nil, fmt.Errorf("relay: outbox delete: %w", err)
		}
		count++
	}
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("relay: outbox iter error: %w", err)
	}

	if len(messages) > 0 {
		if err := batch.Commit(&pebble.WriteOptions{Sync: true}); err != nil {
			return nil, fmt.Errorf("relay: outbox batch commit: %w", err)
		}
	}

	return messages, nil
}

// Flush sends all queued messages to the hub and removes successfully sent ones.
// Messages are sent in priority+FIFO order (high-priority first, then by seq).
// Send failures are logged and skipped so the remaining messages are still
// attempted; the failed messages stay in the outbox for the next Flush call.
// Corrupt (unmarshal-failing) entries are deleted so they never block delivery.
//
// Deletions are batched: instead of one fsync per message (O(n) fsyncs), keys
// are collected during the iteration and committed in a single batch with one
// sync at the end. This reduces fsync cost from O(n) to O(1) under load.
//
// Returns a non-nil error if any messages failed to send or if iteration fails.
func (o *Outbox) Flush(ctx context.Context) error {
	if o.hub == nil {
		return fmt.Errorf("relay: outbox flush: hub is nil")
	}

	pdb := o.db.DB()
	if pdb == nil {
		return fmt.Errorf("relay: outbox: storage is closed")
	}
	prefix := []byte(outboxPrefix)

	iter, err := pdb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: incrementLastByte(prefix),
	})
	if err != nil {
		return fmt.Errorf("relay: outbox flush iter: %w", err)
	}
	defer iter.Close()

	// Collect keys to delete in two buckets:
	//   sentKeys   — successfully sent; committed with Sync: true at the end.
	//   corruptKeys — corrupt records; included in the same batch.
	var sentKeys [][]byte
	var corruptKeys [][]byte

	var failedCount int
	for iter.First(); iter.Valid(); iter.Next() {
		if err := ctx.Err(); err != nil {
			// Partial flush: only delete the messages that were sent so far.
			if commitErr := o.batchDelete(pdb, sentKeys, corruptKeys); commitErr != nil {
				slog.Warn("relay: outbox flush: batch delete on cancel failed", "err", commitErr)
			}
			return fmt.Errorf("relay: outbox flush cancelled: %w", err)
		}

		key := string(iter.Key())
		// Skip the migration sentinel.
		if key == outboxMigrationSentinel {
			continue
		}

		// Copy the key bytes — iter key slice is invalidated on Next().
		keyCopy := make([]byte, len(iter.Key()))
		copy(keyCopy, iter.Key())

		var msg Message
		if err := json.Unmarshal(iter.Value(), &msg); err != nil {
			// Corrupt record: schedule for deletion so it never blocks delivery.
			slog.Warn("relay: outbox deleting corrupt message", "key", key, "err", err)
			corruptKeys = append(corruptKeys, keyCopy)
			continue
		}

		// Skip failing sends rather than aborting — remaining messages are still delivered.
		if err := o.hub.Send(msg.MachineID, msg); err != nil {
			slog.Warn("relay: outbox flush send failed, message kept for retry",
				"machine_id", msg.MachineID, "err", err)
			failedCount++
			continue
		}

		// Message sent successfully — queue for batch delete.
		sentKeys = append(sentKeys, keyCopy)
	}

	if err := iter.Error(); err != nil {
		// Still attempt to delete successfully sent messages before returning error.
		if commitErr := o.batchDelete(pdb, sentKeys, corruptKeys); commitErr != nil {
			slog.Warn("relay: outbox flush: batch delete on iter error failed", "err", commitErr)
		}
		return fmt.Errorf("relay: outbox flush iter error: %w", err)
	}

	// Single batch commit with one fsync for all collected keys.
	if err := o.batchDelete(pdb, sentKeys, corruptKeys); err != nil {
		return fmt.Errorf("relay: outbox flush delete: %w", err)
	}

	if failedCount > 0 {
		return fmt.Errorf("relay: outbox flush: %d message(s) failed to send", failedCount)
	}
	return nil
}

// batchDelete removes sentKeys and corruptKeys from pdb in a single Pebble
// batch with one Sync: true commit. If no keys are provided it is a no-op.
func (o *Outbox) batchDelete(pdb *pebble.DB, sentKeys, corruptKeys [][]byte) error {
	if len(sentKeys) == 0 && len(corruptKeys) == 0 {
		return nil
	}
	batch := pdb.NewBatch()
	defer batch.Close()
	for _, k := range sentKeys {
		if err := batch.Delete(k, nil); err != nil {
			return fmt.Errorf("relay: outbox batch delete sent: %w", err)
		}
	}
	for _, k := range corruptKeys {
		if err := batch.Delete(k, nil); err != nil {
			return fmt.Errorf("relay: outbox batch delete corrupt: %w", err)
		}
	}
	return batch.Commit(&pebble.WriteOptions{Sync: true})
}

// Len returns the number of messages currently in the outbox (excluding sentinel).
func (o *Outbox) Len() (int, error) {
	prefix := []byte(outboxPrefix)

	pdb := o.db.DB()
	if pdb == nil {
		return 0, fmt.Errorf("outbox: store is closed")
	}
	iter, err := pdb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: incrementLastByte(prefix),
	})
	if err != nil {
		return 0, fmt.Errorf("outbox: iterate: %w", err)
	}
	defer iter.Close()

	count := 0
	for iter.First(); iter.Valid(); iter.Next() {
		if string(iter.Key()) == outboxMigrationSentinel {
			continue // don't count the sentinel
		}
		count++
	}
	return count, nil
}

// dropOldest deletes the oldest message (smallest key) from the outbox.
func (o *Outbox) dropOldest() error {
	prefix := []byte(outboxPrefix)

	iter, err := o.db.DB().NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: incrementLastByte(prefix),
	})
	if err != nil {
		return fmt.Errorf("relay: outbox drop oldest iter: %w", err)
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		if string(iter.Key()) == outboxMigrationSentinel {
			continue // skip sentinel
		}
		key := make([]byte, len(iter.Key()))
		copy(key, iter.Key())
		slog.Warn("relay: outbox at max depth, dropping oldest message", "max", OutboxMaxDepth)
		if err := o.db.DB().Delete(key, &pebble.WriteOptions{Sync: true}); err != nil {
			return fmt.Errorf("relay: outbox drop oldest delete: %w", err)
		}
		return nil
	}
	return nil // empty, nothing to drop
}

// outboxKeyV2 encodes a priority+sequence number as a lexicographically sortable key.
// The priority byte is encoded as (255 - priority) so that higher Priority values
// sort earlier (PriorityHigh=255 → prefix \x00, PriorityNormal=0 → prefix \xff).
// Sequence number uses big-endian uint64 hex encoding to preserve insertion order
// within the same priority tier.
func outboxKeyV2(priority uint8, seq uint64) string {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], seq)
	// Invert priority byte so higher priority values sort first lexicographically.
	encoded := 255 - priority
	return fmt.Sprintf("%s%02x:%x", outboxPrefix, encoded, b[:])
}

// outboxKey encodes a sequence number as a lexicographically sortable key.
// Kept for backward compatibility; new code should use outboxKeyV2.
func outboxKey(seq uint64) string {
	return outboxKeyV2(PriorityNormal, seq)
}

// incrementLastByte returns a byte slice that is the exclusive upper bound
// for a Pebble prefix scan. It increments the last byte, carrying over on 0xFF.
func incrementLastByte(b []byte) []byte {
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

// initSeq recovers the sequence number from existing keys after restart.
// It scans all outbox keys and sets seq to the highest one found.
func (o *Outbox) initSeq() {
	prefix := []byte(outboxPrefix)

	pdb := o.db.DB()
	if pdb == nil {
		return
	}
	iter, err := pdb.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: incrementLastByte(prefix),
	})
	if err != nil {
		return
	}
	defer iter.Close()

	var maxSeq uint64
	for iter.Last(); iter.Valid(); iter.Prev() {
		key := string(iter.Key())
		if key == outboxMigrationSentinel {
			continue
		}
		// Try v2 format: relay:outbox:<2hex>:<16hex>
		if len(key) > len(outboxPrefix)+3 {
			rest := key[len(outboxPrefix):]
			// Find the colon separator
			if colonIdx := len(rest) - 17; colonIdx >= 2 && rest[colonIdx] == ':' {
				hexStr := rest[colonIdx+1:]
				var seq uint64
				if _, err := fmt.Sscanf(hexStr, "%x", &seq); err == nil {
					if seq > maxSeq {
						maxSeq = seq
					}
					break
				}
			}
		}
		// Try v1 format: relay:outbox:<16hex>
		if len(key) > len(outboxPrefix) {
			hexStr := key[len(outboxPrefix):]
			var seq uint64
			if _, err := fmt.Sscanf(hexStr, "%x", &seq); err == nil {
				if seq > maxSeq {
					maxSeq = seq
				}
				break
			}
		}
	}
	o.seq.Store(maxSeq)
}
