package server

import (
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
