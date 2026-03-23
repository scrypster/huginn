package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/scrypster/huginn/internal/storage"
)

// SessionSummary is the LLM-generated summary of a single agent session.
// Key: agent:summary:<machineID>:<agentName>:<sessionID>
type SessionSummary struct {
	SessionID     string    `json:"session_id"`
	MachineID     string    `json:"machine_id"`
	AgentName     string    `json:"agent"`
	Timestamp     time.Time `json:"timestamp"`
	Summary       string    `json:"summary"`
	FilesTouched  []string  `json:"files_touched,omitempty"`
	Decisions     []string  `json:"decisions,omitempty"`
	OpenQuestions []string  `json:"open_questions,omitempty"`
	// Channel context — set when session took place inside a Space/channel.
	SpaceID   string `json:"space_id,omitempty"`
	SpaceName string `json:"space_name,omitempty"`
}

// DelegationEntry records a single agent-to-agent consultation.
// Key: agent:delegation:<machineID>:<from>:<to>:<unix-nano-zero-padded>
type DelegationEntry struct {
	From      string    `json:"from"`
	To        string    `json:"to"`
	Question  string    `json:"question"`
	Answer    string    `json:"answer"`
	Timestamp time.Time `json:"timestamp"`
}

func SummaryKey(machineID, agentName, sessionID string) string {
	return fmt.Sprintf("agent:summary:%s:%s:%s", machineID, agentName, sessionID)
}

func SummaryPrefix(machineID, agentName string) string {
	return fmt.Sprintf("agent:summary:%s:%s:", machineID, agentName)
}

// Zero-padded to 20 digits so lexicographic sort matches chronological sort.
func DelegationKey(machineID, from, to string, ts time.Time) string {
	return fmt.Sprintf("agent:delegation:%s:%s:%s:%020d", machineID, from, to, ts.UnixNano())
}

func DelegationPrefix(machineID, from, to string) string {
	return fmt.Sprintf("agent:delegation:%s:%s:%s:", machineID, from, to)
}

// MemoryStoreIface is the contract for agent cross-session memory.
// Both *MemoryStore (Pebble-backed) and *SQLiteMemoryStore (SQLite-backed) implement it.
type MemoryStoreIface interface {
	SaveSummary(ctx context.Context, summary SessionSummary) error
	LoadRecentSummaries(ctx context.Context, agentName string, limit int) ([]SessionSummary, error)
	AppendDelegation(ctx context.Context, entry DelegationEntry) error
	LoadRecentDelegations(ctx context.Context, from, to string, limit int) ([]DelegationEntry, error)
}

// Compile-time assertion: *MemoryStore must satisfy MemoryStoreIface.
var _ MemoryStoreIface = (*MemoryStore)(nil)

// MemoryStore reads and writes agent memory to Pebble.
type MemoryStore struct {
	store     *storage.Store
	machineID string
}

func NewMemoryStore(s *storage.Store, machineID string) *MemoryStore {
	return &MemoryStore{store: s, machineID: machineID}
}

func (m *MemoryStore) SaveSummary(_ context.Context, summary SessionSummary) error {
	if m.store == nil {
		return nil
	}
	data, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("marshal summary: %w", err)
	}
	key := []byte(SummaryKey(m.machineID, summary.AgentName, summary.SessionID))
	return m.store.DB().Set(key, data, &pebble.WriteOptions{Sync: true})
}

func (m *MemoryStore) LoadRecentSummaries(_ context.Context, agentName string, limit int) ([]SessionSummary, error) {
	if m.store == nil {
		return nil, nil
	}
	prefix := []byte(SummaryPrefix(m.machineID, agentName))
	upper := incrementBytes(prefix)
	iter, err := m.store.DB().NewIter(&pebble.IterOptions{LowerBound: prefix, UpperBound: upper})
	if err != nil {
		return nil, fmt.Errorf("new iter: %w", err)
	}
	defer iter.Close()
	var summaries []SessionSummary
	for iter.First(); iter.Valid(); iter.Next() {
		var s SessionSummary
		if err := json.Unmarshal(iter.Value(), &s); err != nil {
			continue
		}
		summaries = append(summaries, s)
	}
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iter: %w", err)
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Timestamp.After(summaries[j].Timestamp) })
	if limit > 0 && len(summaries) > limit {
		summaries = summaries[:limit]
	}
	return summaries, nil
}

func (m *MemoryStore) AppendDelegation(_ context.Context, entry DelegationEntry) error {
	if m.store == nil {
		return nil
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal delegation: %w", err)
	}
	key := []byte(DelegationKey(m.machineID, entry.From, entry.To, entry.Timestamp))
	if err := m.store.DB().Set(key, data, &pebble.WriteOptions{Sync: true}); err != nil {
		return fmt.Errorf("set delegation: %w", err)
	}
	return m.trimDelegations(entry.From, entry.To, 10)
}

func (m *MemoryStore) trimDelegations(from, to string, max int) error {
	prefix := []byte(DelegationPrefix(m.machineID, from, to))
	upper := incrementBytes(prefix)
	iter, err := m.store.DB().NewIter(&pebble.IterOptions{LowerBound: prefix, UpperBound: upper})
	if err != nil {
		return fmt.Errorf("trim iter: %w", err)
	}
	var keys [][]byte
	for iter.First(); iter.Valid(); iter.Next() {
		k := make([]byte, len(iter.Key()))
		copy(k, iter.Key())
		keys = append(keys, k)
	}
	iterErr := iter.Error()
	iter.Close()
	if iterErr != nil {
		return iterErr
	}
	if len(keys) <= max {
		return nil
	}
	toDelete := keys[:len(keys)-max]
	batch := m.store.DB().NewBatch()
	defer batch.Close()
	for _, k := range toDelete {
		if err := batch.Delete(k, nil); err != nil {
			return fmt.Errorf("batch delete: %w", err)
		}
	}
	return batch.Commit(&pebble.WriteOptions{Sync: true})
}

func (m *MemoryStore) LoadRecentDelegations(_ context.Context, from, to string, limit int) ([]DelegationEntry, error) {
	if m.store == nil {
		return nil, nil
	}
	prefix := []byte(DelegationPrefix(m.machineID, from, to))
	upper := incrementBytes(prefix)
	iter, err := m.store.DB().NewIter(&pebble.IterOptions{LowerBound: prefix, UpperBound: upper})
	if err != nil {
		return nil, fmt.Errorf("new iter: %w", err)
	}
	defer iter.Close()
	var entries []DelegationEntry
	for iter.First(); iter.Valid(); iter.Next() {
		var e DelegationEntry
		if err := json.Unmarshal(iter.Value(), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iter: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Timestamp.After(entries[j].Timestamp) })
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

// incrementBytes returns the exclusive upper bound for a prefix scan.
func incrementBytes(b []byte) []byte {
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
