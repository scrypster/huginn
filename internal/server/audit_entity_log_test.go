package server

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestEntityAuditLogger_AppendAndList(t *testing.T) {
	dir := t.TempDir()
	al := newEntityAuditLogger(dir)

	if err := al.Append("agent_create", "user", "hired agent Winston", map[string]any{"agent": "Winston"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := al.Append("agent_delete", "user", "deleted agent Steve", map[string]any{"agent": "Steve"}); err != nil {
		t.Fatalf("append: %v", err)
	}

	entries, err := al.List(10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// Newest first.
	if entries[0].Action != "agent_delete" {
		t.Errorf("entries[0].Action = %q, want agent_delete", entries[0].Action)
	}
	if entries[0].Actor != "user" {
		t.Errorf("entries[0].Actor = %q, want user", entries[0].Actor)
	}
	if entries[0].TS.IsZero() {
		t.Error("entries[0].TS is zero")
	}
	if entries[1].Action != "agent_create" {
		t.Errorf("entries[1].Action = %q, want agent_create", entries[1].Action)
	}
}

func TestEntityAuditLogger_List_RespectsLimit(t *testing.T) {
	dir := t.TempDir()
	al := newEntityAuditLogger(dir)
	for i := 0; i < 5; i++ {
		if err := al.Append("member_seat", "user", "seated", nil); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	entries, err := al.List(2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (limit), got %d", len(entries))
	}
}

func TestEntityAuditLogger_List_EmptyWhenNoFile(t *testing.T) {
	dir := t.TempDir()
	al := newEntityAuditLogger(dir)
	entries, err := al.List(10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestEntityAuditLogger_RotatesPastSizeThreshold(t *testing.T) {
	dir := t.TempDir()
	al := newEntityAuditLogger(dir)

	// Each entry's detail blob is ~100KB; enough of them push the file past
	// auditMaxBytes (5MB) and trigger a rotation.
	blob := strings.Repeat("x", 100*1024)
	for i := 0; i < 60; i++ {
		if err := al.Append("member_seat", "user", "seated", map[string]any{"blob": blob}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	rotatedPath := filepath.Join(dir, "audit.jsonl.1")
	info, err := os.Stat(rotatedPath)
	if err != nil {
		t.Fatalf("expected rotated file %s to exist: %v", rotatedPath, err)
	}
	if info.Size() == 0 {
		t.Error("rotated predecessor file is empty")
	}

	currentInfo, err := os.Stat(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatalf("stat current audit.jsonl: %v", err)
	}
	if currentInfo.Size() >= auditMaxBytes {
		t.Errorf("current audit.jsonl size %d did not shrink after rotation (threshold %d)", currentInfo.Size(), auditMaxBytes)
	}

	// Only one predecessor is kept: a second rotation must replace, not
	// accumulate, audit.jsonl.2 etc.
	for i := 0; i < 60; i++ {
		if err := al.Append("member_seat", "user", "seated again", map[string]any{"blob": blob}); err != nil {
			t.Fatalf("append round 2 #%d: %v", i, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "audit.jsonl.2")); !os.IsNotExist(err) {
		t.Errorf("expected no audit.jsonl.2 to exist, got err=%v", err)
	}
}

func TestEntityAuditLogger_List_FallsBackToRotatedPredecessor(t *testing.T) {
	dir := t.TempDir()
	al := newEntityAuditLogger(dir)

	blob := strings.Repeat("x", 100*1024)
	// Force at least one rotation, then add a few more small entries to the
	// fresh current file.
	for i := 0; i < 60; i++ {
		if err := al.Append("member_seat", "user", "seated", map[string]any{"blob": blob}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "audit.jsonl.1")); err != nil {
		t.Fatalf("expected rotation to have happened: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := al.Append("agent_delete", "user", "deleted", nil); err != nil {
			t.Fatalf("append small %d: %v", i, err)
		}
	}

	// Asking for more entries than the current (post-rotation) file holds
	// must fall back to the retained predecessor for the remainder.
	entries, err := al.List(5)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries pulling from predecessor, got %d", len(entries))
	}
	for i := 0; i < 3; i++ {
		if entries[i].Action != "agent_delete" {
			t.Errorf("entries[%d].Action = %q, want agent_delete (newest-first)", i, entries[i].Action)
		}
	}
	for i := 3; i < 5; i++ {
		if entries[i].Action != "member_seat" {
			t.Errorf("entries[%d].Action = %q, want member_seat (from predecessor)", i, entries[i].Action)
		}
	}
}

func TestEntityAuditLogger_List_TailReadCorrectAcrossChunkBoundaries(t *testing.T) {
	dir := t.TempDir()
	al := newEntityAuditLogger(dir)

	// Enough small entries to span multiple auditTailChunkSize reads without
	// triggering rotation, so we're testing the chunked backward-read path
	// specifically (not the rotation fallback).
	const n = 4000
	for i := 0; i < n; i++ {
		if err := al.Append("member_seat", "user", "seated", map[string]any{"i": i}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	entries, err := al.List(7)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 7 {
		t.Fatalf("expected 7 entries, got %d", len(entries))
	}
	for i, e := range entries {
		wantI := n - 1 - i
		gotI, ok := e.Detail["i"].(float64)
		if !ok || int(gotI) != wantI {
			t.Errorf("entries[%d].Detail[i] = %v, want %d", i, e.Detail["i"], wantI)
		}
	}

	all, err := al.List(0)
	if err != nil {
		t.Fatalf("list(0): %v", err)
	}
	if len(all) != n {
		t.Fatalf("expected unlimited List to return all %d entries, got %d", n, len(all))
	}
}

// TestEntityAuditLogger_List_CorruptLineDoesNotSpliceStalePredecessor is the
// Opus-vet finding (2026-08-28): List used to fall back to the retained
// predecessor (audit.jsonl.1) whenever the current-file read came up short
// of `limit` valid entries — without checking whether the short read
// actually reached the start of the current file. A malformed line (or, as
// here, a wide run of them) inside the tail-read window made the old
// raw-newline stopping heuristic quit reading the current file early, so
// List spliced in much older predecessor rows even though the current file,
// read a bit further back, still had plenty of valid entries of its own.
func TestEntityAuditLogger_List_CorruptLineDoesNotSpliceStalePredecessor(t *testing.T) {
	dir := t.TempDir()
	al := newEntityAuditLogger(dir)

	// Predecessor file: distinctly marked so any splice from it is obvious.
	predLine := `{"ts":"2020-01-01T00:00:00Z","actor":"user","action":"OLD_PREDECESSOR","what":"stale","detail":{}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "audit.jsonl.1"), []byte(strings.Repeat(predLine, 5)), 0644); err != nil {
		t.Fatalf("write predecessor: %v", err)
	}

	// Current file, oldest to newest:
	//   20 valid entries ("older_valid", idx 0..19)
	//   2000 corrupted lines (a run far wider than one 64KB read chunk)
	//   3 valid entries ("newest_valid", idx 0..2)
	// The 5 most recent VALID entries are: the 3 newest_valid ones, plus the
	// 2 most recent older_valid ones (idx 19 and 18) — all still inside the
	// current file, well before any predecessor row should be consulted.
	var buf strings.Builder
	for i := 0; i < 20; i++ {
		buf.WriteString(`{"ts":"2026-01-01T00:00:00Z","actor":"user","action":"older_valid","what":"seated","detail":{"idx":`)
		buf.WriteString(strconv.Itoa(i))
		buf.WriteString("}}\n")
	}
	for i := 0; i < 2000; i++ {
		buf.WriteString("not valid json at all, just garbage padding text so this line has some length\n")
	}
	for i := 0; i < 3; i++ {
		buf.WriteString(`{"ts":"2026-01-02T00:00:00Z","actor":"user","action":"newest_valid","what":"seated","detail":{"idx":`)
		buf.WriteString(strconv.Itoa(i))
		buf.WriteString("}}\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "audit.jsonl"), []byte(buf.String()), 0644); err != nil {
		t.Fatalf("write current: %v", err)
	}

	entries, err := al.List(5)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d: %+v", len(entries), entries)
	}
	for _, e := range entries {
		if e.Action == "OLD_PREDECESSOR" {
			t.Fatalf("List spliced in a stale predecessor row while the current file still had valid entries: %+v", entries)
		}
	}
	// Newest-first: the 3 newest_valid entries (idx 2,1,0), then the 2 most
	// recent older_valid entries (idx 19, 18) — skipping straight past the
	// 2000-line corrupted run without ever touching audit.jsonl.1.
	wantAction := []string{"newest_valid", "newest_valid", "newest_valid", "older_valid", "older_valid"}
	wantIdx := []int{2, 1, 0, 19, 18}
	for i, e := range entries {
		if e.Action != wantAction[i] {
			t.Errorf("entries[%d].Action = %q, want %q", i, e.Action, wantAction[i])
		}
		gotIdx, ok := e.Detail["idx"].(float64)
		if !ok || int(gotIdx) != wantIdx[i] {
			t.Errorf("entries[%d].Detail[idx] = %v, want %d", i, e.Detail["idx"], wantIdx[i])
		}
	}
}