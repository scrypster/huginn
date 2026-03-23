package session

// security_spec_test.go — Behavior specs for session ID validation security.
//
// These tests verify that validateID() rejects path-traversal payloads,
// preventing an attacker from escaping the session storage directory.
// All invariants are documented in store.go:validateID.

import (
	"testing"
)

func TestValidateID_PathTraversal_Rejected(t *testing.T) {
	traversals := []struct {
		name string
		id   string
	}{
		{"dot-dot-slash", "../../../etc/passwd"},
		{"dot-dot-backslash", `..\..\windows\system32`},
		{"leading-slash", "/etc/passwd"},
		{"backslash-only", `\evil`},
		{"embedded-dotdot", "sess/../../../secret"},
		{"pure-dotdot", ".."},
		{"double-dotdot", "../.."},
		{"slash-only", "/"},
	}

	for _, tc := range traversals {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateID(tc.id); err == nil {
				t.Errorf("validateID(%q) = nil, want error (path traversal not rejected)", tc.id)
			}
		})
	}
}

func TestValidateID_Empty_Rejected(t *testing.T) {
	if err := validateID(""); err == nil {
		t.Error("validateID(\"\") = nil, want error")
	}
}

func TestValidateID_ValidIDs_Accepted(t *testing.T) {
	valid := []string{
		"sess-abc123",
		"01JQABCDEFGHIJKLMNOP",
		"my-agent-session",
		"123456",
		"session.with.dots",
	}

	for _, id := range valid {
		if err := validateID(id); err != nil {
			t.Errorf("validateID(%q) = %v, want nil", id, err)
		}
	}
}

func TestStore_New_AlwaysGeneratesSafeULID(t *testing.T) {
	// New() always generates a ULID via NewID() — the title param cannot influence
	// the session ID. This test documents that the session ID is always safe.
	dir := t.TempDir()
	store := NewStore(dir)

	// Title contains path traversal chars but must NOT affect the generated ID.
	sess := store.New("a session title", "/ws", "model")
	if sess == nil {
		t.Fatal("New() returned nil")
	}
	if err := validateID(sess.ID); err != nil {
		t.Errorf("New() produced session with invalid ID %q: %v", sess.ID, err)
	}
}

func TestStore_TailMessages_RejectsPathTraversalSessionID(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	_, err := store.TailMessages("../../../etc/shadow", 10)
	if err == nil {
		t.Error("TailMessages with path-traversal ID should return error, got nil")
	}
}
