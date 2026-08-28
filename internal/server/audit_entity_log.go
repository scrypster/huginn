package server

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// entityAuditEntry is one append-only record of a lifecycle action on a
// named entity — an agent hire/delete, a company seat/unseat, a memory
// forget. Unlike audit_log (SQLite; permission-gate allow/deny decisions,
// pruned to 30 days, no actor field), this trail is meant to answer "who did
// what, and when" for entities the operator can create and destroy, and is
// kept as a separate, actor-carrying record rather than overloading a table
// documented for a different purpose.
type entityAuditEntry struct {
	TS     time.Time      `json:"ts"`
	Actor  string         `json:"actor"`
	Action string         `json:"action"`
	What   string         `json:"what"`
	Detail map[string]any `json:"detail,omitempty"`
}

// entityAuditLogger appends entityAuditEntry records to a JSONL file under
// the huginn data dir. Append-only by construction: entries are never
// rewritten or deleted, only appended and later read back (newest first).
type entityAuditLogger struct {
	mu   sync.Mutex
	path string
}

// newEntityAuditLogger returns a logger writing to <huginnDir>/audit.jsonl.
// Safe to construct even if huginnDir does not yet exist; Append creates it.
func newEntityAuditLogger(huginnDir string) *entityAuditLogger {
	return &entityAuditLogger{path: filepath.Join(huginnDir, "audit.jsonl")}
}

// Append writes one audit entry. Best-effort on directory creation; returns
// an error only if the write itself fails, so callers can log-and-continue
// without blocking the action being audited.
func (l *entityAuditLogger) Append(action, actor, what string, detail map[string]any) error {
	entry := entityAuditEntry{
		TS:     time.Now().UTC(),
		Actor:  actor,
		Action: action,
		What:   what,
		Detail: detail,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(l.path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

// List returns up to limit entries, most recent first. Returns an empty
// slice (not an error) when the log file does not exist yet.
func (l *entityAuditLogger) List(limit int) ([]entityAuditEntry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	f, err := os.Open(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []entityAuditEntry{}, nil
		}
		return nil, err
	}
	defer f.Close()

	var all []entityAuditEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var e entityAuditEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue // skip malformed lines rather than fail the whole read
		}
		all = append(all, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Reverse into newest-first order, then cap to limit.
	out := make([]entityAuditEntry, 0, len(all))
	for i := len(all) - 1; i >= 0; i-- {
		out = append(out, all[i])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}
