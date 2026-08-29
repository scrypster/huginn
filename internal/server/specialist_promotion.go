package server

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SpecialistPromotionWindow is the trailing window S14 counts specialist
// spawns within. Three spawns of the same capability label inside this
// window is the signal the CoS should recommend a permanent hire — it
// never hires automatically (see spawn_specialist's S13 speech contract).
const SpecialistPromotionWindow = 14 * 24 * time.Hour

// specialistSpawnRecord is one append-only record of a spawn_specialist
// success, written the moment the tool's Spawn callback returns without
// error. Mirrors entityAuditEntry's append-only JSONL pattern
// (audit_entity_log.go) but tracks a narrower, purpose-built shape for
// S14's promotion counter rather than reusing the generic
// actor/action/detail audit trail.
type specialistSpawnRecord struct {
	TS              time.Time `json:"ts"`
	Company         string    `json:"company,omitempty"`
	CapabilityLabel string    `json:"capability_label"`
	Model           string    `json:"model"`
	ThreadID        string    `json:"thread_id"`
}

// SpecialistPromotionTracker persists specialist spawn records to a JSONL
// file under the huginn data dir and answers "was this the Nth spawn of
// this capability within the trailing window" for the S14 promotion
// counter. Append-only, like entityAuditLogger — no rotation, since spawn
// volume is expected to be orders of magnitude lower than the audit trail.
type SpecialistPromotionTracker struct {
	mu   sync.Mutex
	path string
}

// NewSpecialistPromotionTracker returns a tracker writing to
// <huginnDir>/specialist_spawns.jsonl. Safe to construct even if huginnDir
// does not yet exist; RecordSpawn creates it.
func NewSpecialistPromotionTracker(huginnDir string) *SpecialistPromotionTracker {
	return &SpecialistPromotionTracker{path: filepath.Join(huginnDir, "specialist_spawns.jsonl")}
}

// NormalizeCapabilityLabel folds a specialist's capability label
// (spawn_specialist's specialistDomain, e.g. "Rust Audit") down to a
// comparison key: lowercased and whitespace-collapsed. Deliberately NOT
// fuzzy — "Rust Audit" and "Rust Security Review" stay distinct labels.
// The S14 synthesis decision explicitly accepts undercounting similar-but-
// differently-worded specialist requests over the false-clustering risk a
// fuzzy match would introduce.
func NormalizeCapabilityLabel(domain string) string {
	return strings.Join(strings.Fields(strings.ToLower(domain)), " ")
}

// RecordSpawn appends one spawn record. A write failure is returned to the
// caller to log — it must never block the specialist spawn itself, since
// the promotion counter is a nice-to-have, not load-bearing.
func (t *SpecialistPromotionTracker) RecordSpawn(company, capabilityLabel, model, threadID string) error {
	rec := specialistSpawnRecord{
		TS:              time.Now().UTC(),
		Company:         company,
		CapabilityLabel: NormalizeCapabilityLabel(capabilityLabel),
		Model:           model,
		ThreadID:        threadID,
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	t.mu.Lock()
	defer t.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(t.path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(t.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

// CountInWindow returns how many recorded spawns of capabilityLabel
// (normalized) fall within [now-window, now], inclusive.
func (t *SpecialistPromotionTracker) CountInWindow(capabilityLabel string, now time.Time, window time.Duration) (int, error) {
	label := NormalizeCapabilityLabel(capabilityLabel)
	since := now.Add(-window)

	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := os.ReadFile(t.path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	count := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var rec specialistSpawnRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue // skip malformed lines rather than fail the whole count
		}
		if rec.ThreadID == recommendedSentinel {
			continue // sentinel markers are not spawns
		}
		if rec.CapabilityLabel != label {
			continue
		}
		if rec.TS.Before(since) || rec.TS.After(now) {
			continue
		}
		count++
	}
	return count, nil
}

// ShouldRecommendHire reports whether NOW is the moment to surface the
// permanent-hire recommendation for capabilityLabel, and atomically marks it
// so it fires EXACTLY ONCE per label per window.
//
// The naive count==3 snapshot the Opus vet flagged (2026-08-29) was fragile
// two ways: three specialists completing in parallel each read count==3 and
// each recommended (up to 3x), while a 4th spawning before the first three
// completed pushed the count past 3 and it fired zero times. The fix is a
// check-and-set: fire when the count first reaches the threshold (>= 3) AND
// this label has not already been recommended within the current window,
// then record a "recommended" marker so subsequent completions stay silent.
func (t *SpecialistPromotionTracker) ShouldRecommendHire(capabilityLabel string) (bool, error) {
	label := NormalizeCapabilityLabel(capabilityLabel)
	now := time.Now()
	count, err := t.CountInWindow(label, now, SpecialistPromotionWindow)
	if err != nil {
		return false, err
	}
	if count < 3 {
		return false, nil
	}
	// check-and-set under the same mutex CountInWindow released — acceptable
	// for a single-process serve; a cross-process race would at worst
	// duplicate one recommendation, never lose the whole feature.
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.recommendedWithinLocked(label, now) {
		return false, nil
	}
	if err := t.markRecommendedLocked(label, now); err != nil {
		return false, err
	}
	return true, nil
}

// recommendedMarker is a sentinel record (ThreadID == recommendedSentinel)
// noting that a hire recommendation was already surfaced for a label.
const recommendedSentinel = "__recommended__"

func (t *SpecialistPromotionTracker) recommendedWithinLocked(label string, now time.Time) bool {
	since := now.Add(-SpecialistPromotionWindow)
	data, err := os.ReadFile(t.path)
	if err != nil {
		return false
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var rec specialistSpawnRecord
		if json.Unmarshal(line, &rec) != nil {
			continue
		}
		if rec.ThreadID == recommendedSentinel && rec.CapabilityLabel == label &&
			!rec.TS.Before(since) && !rec.TS.After(now) {
			return true
		}
	}
	return false
}

func (t *SpecialistPromotionTracker) markRecommendedLocked(label string, now time.Time) error {
	rec := specialistSpawnRecord{TS: now.UTC(), CapabilityLabel: label, ThreadID: recommendedSentinel}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(t.path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(t.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}
