package server

import (
	"os"
	"path/filepath"
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
