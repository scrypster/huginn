package server

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// auditMaxBytes is the size threshold at which audit.jsonl is rotated to
// audit.jsonl.1 on the next Append. Keeping one predecessor bounds disk
// usage to roughly 2x this value while still giving List a useful amount of
// history to fall back to once the current file has just rotated.
const auditMaxBytes = 5 * 1024 * 1024 // 5MB

// auditTailChunkSize is the chunk size used when reading a bounded number of
// entries from the end of the audit log without parsing the whole file.
const auditTailChunkSize = 64 * 1024

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

	// Rotate before writing if the current file has grown past the size
	// threshold, so audit.jsonl never grows unbounded and List() never has
	// to scan an ever-larger file. Keep exactly one predecessor
	// (audit.jsonl.1); Rename atomically replaces any prior .1.
	if info, statErr := os.Stat(l.path); statErr == nil && info.Size() >= auditMaxBytes {
		if err := os.Rename(l.path, l.path+".1"); err != nil {
			return err
		}
	}

	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

// List returns up to limit entries, most recent first (limit <= 0 means
// unlimited). Returns an empty slice (not an error) when the log file does
// not exist yet.
//
// Reads from the tail of audit.jsonl backwards in chunks rather than
// parsing the whole file, so a request for a small limit stays cheap even
// once the file has grown close to its rotation threshold. If the current
// file doesn't have enough entries to satisfy limit, falls back to the one
// retained predecessor (audit.jsonl.1) for the remainder.
func (l *entityAuditLogger) List(limit int) ([]entityAuditEntry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	out, err := readAuditTail(l.path, limit)
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []entityAuditEntry{}
	}
	if limit > 0 && len(out) >= limit {
		return out, nil
	}

	remaining := -1 // unlimited
	if limit > 0 {
		remaining = limit - len(out)
	}
	older, err := readAuditTail(l.path+".1", remaining)
	if err != nil {
		return nil, err
	}
	out = append(out, older...)
	return out, nil
}

// countValidTrailingEntries counts JSON-parseable, non-empty lines in buf,
// skipping the first split element (it may be a truncated partial line —
// its start is only certain once the chunk read has reached byte 0 of the
// file). Stops scanning early once the count exceeds limit; the caller only
// needs to know whether there are "enough", not the exact total.
func countValidTrailingEntries(buf []byte, limit int) int {
	lines := bytes.Split(buf, []byte("\n"))
	n := 0
	for i := 1; i < len(lines); i++ {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		var e entityAuditEntry
		if err := json.Unmarshal(line, &e); err == nil {
			n++
			if n > limit {
				return n
			}
		}
	}
	return n
}

// readAuditTail reads the last `limit` JSONL entries from path, newest
// first, without parsing the whole file when limit > 0. limit <= 0 reads
// and parses the entire file. Returns (nil, nil) if path does not exist.
func readAuditTail(path string, limit int) ([]entityAuditEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if size == 0 {
		return nil, nil
	}

	var buf []byte
	if limit <= 0 {
		buf = make([]byte, size)
		if _, err := f.ReadAt(buf, 0); err != nil {
			return nil, err
		}
	} else {
		pos := size
		for pos > 0 {
			readSize := int64(auditTailChunkSize)
			if readSize > pos {
				readSize = pos
			}
			pos -= readSize
			chunk := make([]byte, readSize)
			if _, err := f.ReadAt(chunk, pos); err != nil {
				return nil, err
			}
			buf = append(chunk, buf...)
			if pos == 0 {
				// Reached the start of the file — stop regardless of how
				// many valid entries were found; there is nothing more to
				// read from this file.
				break
			}
			// Stop once the window holds more than `limit` VALID trailing
			// entries, not merely `limit` raw newlines. A corrupt/malformed
			// line mid-window used to be counted as a newline but silently
			// dropped during parsing below, so the raw-newline heuristic
			// could stop the read one or more chunks too early — returning
			// fewer than `limit` entries even though the rest of the
			// current file still held plenty. List() then treated that
			// short read as "current file exhausted" and spliced in rows
			// from the much older audit.jsonl.1 ahead of still-unread,
			// still-valid rows in the current file.
			if countValidTrailingEntries(buf, limit) > limit {
				break
			}
		}
	}

	lines := bytes.Split(buf, []byte("\n"))
	var all []entityAuditEntry
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var e entityAuditEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue // skip malformed lines rather than fail the whole read
		}
		all = append(all, e)
	}

	out := make([]entityAuditEntry, 0, len(all))
	for i := len(all) - 1; i >= 0; i-- {
		out = append(out, all[i])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}
