// internal/scheduler/delivery_dead_letter.go
package scheduler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// deliveryFailureMu serialises all writes to the daily JSONL dead-letter file.
// Multiple goroutines may call WriteDeliveryFailure concurrently; without this
// mutex their JSON lines could interleave and corrupt the JSONL structure.
// NOTE(localhost-app): This serialisation is within a single process; no
// cross-process file locking is needed because Huginn runs as a single process.
var deliveryFailureMu sync.Mutex

// DeliveryFailureRecord is a single dead-letter entry written to disk when all
// webhook delivery attempts are exhausted.
// RetriedAt is set (non-empty) when a manual retry was requested via the API.
// The JSONL file is append-only; retried records are never deleted, preserving
// a full audit trail. Consumers filter on RetriedAt to determine pending vs.
// already-retried failures.
type DeliveryFailureRecord struct {
	Ts         string `json:"ts"`
	WorkflowID string `json:"workflow_id"`
	RunID      string `json:"run_id"`
	URL        string `json:"url"`
	Attempts   int    `json:"attempts"`
	LastError  string `json:"last_error"`
	RetriedAt  string `json:"retried_at,omitempty"` // set when retried via API; file is append-only
}

// WriteDeliveryFailure appends a dead-letter record to
// <huginnDir>/delivery-failures/YYYY-MM-DD.jsonl in a fire-and-forget
// goroutine. Errors are logged but never propagated to the caller.
func WriteDeliveryFailure(huginnDir, workflowID, runID, url string, attempts int, lastError string) {
	rec := DeliveryFailureRecord{
		Ts:         time.Now().UTC().Format(time.RFC3339),
		WorkflowID: workflowID,
		RunID:      runID,
		URL:        url,
		Attempts:   attempts,
		LastError:  lastError,
	}
	go func() {
		if err := appendDeliveryFailure(huginnDir, rec); err != nil {
			slog.Error("scheduler: failed to write delivery dead-letter record",
				"workflow_id", workflowID, "run_id", runID, "err", err)
		}
	}()
}

func appendDeliveryFailure(huginnDir string, rec DeliveryFailureRecord) error {
	deliveryFailureMu.Lock()
	defer deliveryFailureMu.Unlock()
	dir := filepath.Join(huginnDir, "delivery-failures")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir delivery-failures: %w", err)
	}

	day := time.Now().UTC().Format("2006-01-02")
	path := filepath.Join(dir, day+".jsonl")

	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal delivery failure: %w", err)
	}
	line = append(line, '\n')

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open delivery failures file: %w", err)
	}
	defer f.Close()
	_, err = f.Write(line)
	return err
}

// ReadDeliveryFailures reads the last 7 days of dead-letter files from
// <huginnDir>/delivery-failures/ and returns up to limit records sorted
// newest first. It ignores files that cannot be read.
func ReadDeliveryFailures(huginnDir string, limit int) ([]DeliveryFailureRecord, error) {
	dir := filepath.Join(huginnDir, "delivery-failures")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []DeliveryFailureRecord{}, nil
		}
		return nil, fmt.Errorf("read delivery-failures dir: %w", err)
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -7).Format("2006-01-02")

	// Collect files within the 7-day window, newest first.
	var files []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		day := strings.TrimSuffix(name, ".jsonl")
		if day >= cutoff {
			files = append(files, filepath.Join(dir, name))
		}
	}
	// Sort descending (newest file first).
	sort.Sort(sort.Reverse(sort.StringSlice(files)))

	// Build a set of (workflow_id, run_id) pairs that have been retried so we
	// can suppress the original failure records that preceded them. Since the
	// file is append-only, a retried record always appears after the original.
	retriedPairs := make(map[string]struct{})
	for _, path := range files {
		recs, err := readFailureFile(path)
		if err != nil {
			continue
		}
		for _, r := range recs {
			if r.RetriedAt != "" {
				retriedPairs[r.WorkflowID+"|"+r.RunID] = struct{}{}
			}
		}
	}

	var out []DeliveryFailureRecord
	for _, path := range files {
		if limit > 0 && len(out) >= limit {
			break
		}
		recs, err := readFailureFile(path)
		if err != nil {
			slog.Warn("scheduler: could not read delivery failures file", "path", path, "err", err)
			continue
		}
		// Within each file, newest entries are at the bottom (append-only JSONL).
		// Reverse so we get newest-first ordering when prepending to out.
		for i := len(recs) - 1; i >= 0; i-- {
			if limit > 0 && len(out) >= limit {
				break
			}
			r := recs[i]
			// Skip: the retry marker record itself, and original failures for retried runs.
			if r.RetriedAt != "" {
				continue
			}
			if _, alreadyRetried := retriedPairs[r.WorkflowID+"|"+r.RunID]; alreadyRetried {
				continue
			}
			out = append(out, r)
		}
	}
	if out == nil {
		out = []DeliveryFailureRecord{}
	}
	return out, nil
}

// MarkDeliveryFailureRetried appends a retry marker record to the dead-letter
// file for today. The original failure record is preserved (append-only audit
// trail); ReadDeliveryFailures will filter it from future listings.
func MarkDeliveryFailureRetried(huginnDir, workflowID, runID, url string) {
	rec := DeliveryFailureRecord{
		Ts:         time.Now().UTC().Format(time.RFC3339),
		WorkflowID: workflowID,
		RunID:      runID,
		URL:        url,
		RetriedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	go func() {
		if err := appendDeliveryFailure(huginnDir, rec); err != nil {
			slog.Error("scheduler: failed to write retry marker", "workflow_id", workflowID, "run_id", runID, "err", err)
		}
	}()
}

func readFailureFile(path string) ([]DeliveryFailureRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var recs []DeliveryFailureRecord
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec DeliveryFailureRecord
		if json.Unmarshal([]byte(line), &rec) == nil {
			recs = append(recs, rec)
		}
	}
	return recs, nil
}
